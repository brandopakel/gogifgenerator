package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/account"
	"github.com/brandopakel/gogifgenerator/internal/auth"
	"github.com/brandopakel/gogifgenerator/internal/billing"
	"github.com/brandopakel/gogifgenerator/internal/cinematic"
	filmblender "github.com/brandopakel/gogifgenerator/internal/cinematic/blender"
	filmffmpeg "github.com/brandopakel/gogifgenerator/internal/cinematic/ffmpeg"
	"github.com/brandopakel/gogifgenerator/internal/cinematic/unity"
	"github.com/brandopakel/gogifgenerator/internal/cinematic/unreal"
	"github.com/brandopakel/gogifgenerator/internal/config"
	"github.com/brandopakel/gogifgenerator/internal/httpapi"
	"github.com/brandopakel/gogifgenerator/internal/imagegen"
	"github.com/brandopakel/gogifgenerator/internal/imagegen/blender"
	"github.com/brandopakel/gogifgenerator/internal/imagegen/comfypartner"
	"github.com/brandopakel/gogifgenerator/internal/imagegen/comfyui"
	openaiimage "github.com/brandopakel/gogifgenerator/internal/imagegen/openai"
	"github.com/brandopakel/gogifgenerator/internal/media"
	"github.com/brandopakel/gogifgenerator/internal/modelgen"
	modelcomfy "github.com/brandopakel/gogifgenerator/internal/modelgen/comfyui"
	"github.com/brandopakel/gogifgenerator/internal/planner"
	"github.com/brandopakel/gogifgenerator/internal/provider"
	"github.com/brandopakel/gogifgenerator/internal/provider/gifcities"
	"github.com/brandopakel/gogifgenerator/internal/provider/nasa"
	"github.com/brandopakel/gogifgenerator/internal/provider/prelinger"
	"github.com/brandopakel/gogifgenerator/internal/provider/wikimedia"
	"github.com/brandopakel/gogifgenerator/internal/provider/yarn"
	"github.com/brandopakel/gogifgenerator/internal/reference"
	"github.com/brandopakel/gogifgenerator/internal/scene"
	"github.com/brandopakel/gogifgenerator/internal/store"
	"github.com/brandopakel/gogifgenerator/internal/video"
	"github.com/brandopakel/gogifgenerator/internal/video/ffmpeg"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	settings := config.Load()
	if settings.AuthMode == auth.ModeOIDC && settings.MemKVAddress == "" {
		logger.Error("configure accounts", "error", "GOGIF_MEMKV_ADDR is required for durable multi-user accounts")
		os.Exit(1)
	}
	localPlanner := planner.Local{}
	var animationPlanner planner.Planner = localPlanner
	paidAIEnabled := settings.PaidAIEnabled && settings.OpenAIAPIKey != ""
	if paidAIEnabled {
		animationPlanner = planner.WithFallback{
			Primary: planner.OpenAI{
				APIKey:  settings.OpenAIAPIKey,
				Model:   settings.OpenAIModel,
				BaseURL: settings.OpenAIBaseURL,
			},
			Fallback: localPlanner,
			OnError: func(err error) {
				logger.Warn("AI planner unavailable; using local planner", "error", err)
			},
		}
	}

	var catalog store.KV = store.NewMemoryKV()
	catalogBackend := "memory"
	if settings.MemKVAddress != "" {
		memkv, err := store.NewMemKV(settings.MemKVAddress)
		if err != nil {
			logger.Error("configure MemKV", "error", err)
			os.Exit(1)
		}
		defer memkv.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err = memkv.Ping(ctx)
		cancel()
		if err != nil {
			logger.Error("connect to MemKV", "address", settings.MemKVAddress, "error", err)
			os.Exit(1)
		}
		catalog = memkv
		catalogBackend = "memkv"
	}
	blobs, err := store.NewFileBlobStore(settings.BlobDirectory)
	if err != nil {
		logger.Error("configure blob storage", "error", err)
		os.Exit(1)
	}
	mediaRepository := media.NewRepository(catalog)
	library := media.NewLibrary(mediaRepository, blobs)
	var generatedSaver media.GeneratedSaver = library
	var generatedReader media.GeneratedReader = library
	var modelSaver media.ModelSaver = library
	var modelReader media.ModelReader = library
	plans := account.NewCatalog(account.CatalogOptions{
		CreatorPriceID: settings.StripeCreatorPriceID, ProPriceID: settings.StripeProPriceID,
		CreatorCents: settings.CreatorPriceCents, ProCents: settings.ProPriceCents,
	})
	accounts := account.NewRepository(catalog)
	usage := account.NewLedger(catalog)
	var sceneRepository *scene.Repository
	if settings.SceneJobsEnabled {
		if settings.AuthMode == auth.ModeDisabled {
			logger.Error("configure Scene jobs", "error", "accounts must be enabled before Scene jobs")
			os.Exit(1)
		}
		if len(settings.SceneWorkerToken) < 32 {
			logger.Error("configure Scene jobs", "error", "GOGIF_SCENE_WORKER_TOKEN must contain at least 32 characters")
			os.Exit(1)
		}
		targets := make([]scene.EngineTarget, 0, len(settings.SceneTargets))
		for _, target := range settings.SceneTargets {
			targets = append(targets, scene.EngineTarget(target))
		}
		sceneRepository, err = scene.NewRepository(catalog, scene.Options{AllowedTargets: targets})
		if err != nil {
			logger.Error("configure Scene jobs", "error", err)
			os.Exit(1)
		}
		if catalogBackend == "memory" {
			logger.Warn("Scene jobs use ephemeral memory; configure GOGIF_MEMKV_ADDR before production testing")
		}
	}
	var identityProvider auth.Provider
	if settings.AuthMode == auth.ModeOIDC {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		provider, providerErr := auth.NewOIDCProvider(ctx, auth.OIDCOptions{
			Issuer: settings.OIDCIssuer, ClientID: settings.OIDCClientID, ClientSecret: settings.OIDCClientSecret, RedirectURL: settings.OIDCRedirectURL,
		})
		cancel()
		if providerErr != nil {
			logger.Error("configure OIDC", "error", providerErr)
			os.Exit(1)
		}
		identityProvider = provider
	}
	authManager, err := auth.New(auth.Options{
		Mode: settings.AuthMode, SessionSecret: settings.SessionSecret, PublicURL: settings.PublicURL,
		Repository: accounts, Provider: identityProvider, LocalEmail: settings.LocalOwnerEmail,
	})
	if err != nil {
		logger.Error("configure accounts", "error", err)
		os.Exit(1)
	}
	var stripeBilling *billing.Stripe
	if settings.BillingEnabled {
		if settings.AuthMode != auth.ModeOIDC {
			logger.Error("configure billing", "error", "Stripe billing requires GOGIF_AUTH_MODE=oidc")
			os.Exit(1)
		}
		stripeBilling, err = billing.NewStripe(billing.Options{
			SecretKey: settings.StripeSecretKey, WebhookSecret: settings.StripeWebhookSecret, PublicURL: settings.PublicURL,
			Catalog: plans, Accounts: accounts, KV: catalog,
		})
		if err != nil {
			logger.Error("configure billing", "error", err)
			os.Exit(1)
		}
	}
	commons, err := wikimedia.New(wikimedia.Options{})
	if err != nil {
		logger.Error("configure Wikimedia Commons", "error", err)
		os.Exit(1)
	}
	cities, err := gifcities.New(gifcities.Options{})
	if err != nil {
		logger.Error("configure GifCities", "error", err)
		os.Exit(1)
	}
	archive, err := prelinger.New(prelinger.Options{})
	if err != nil {
		logger.Error("configure Prelinger Archive", "error", err)
		os.Exit(1)
	}
	nasaLibrary, err := nasa.New(nasa.Options{})
	if err != nil {
		logger.Error("configure NASA Image and Video Library", "error", err)
		os.Exit(1)
	}
	yarnClips, err := yarn.New(yarn.Options{})
	if err != nil {
		logger.Error("configure Yarn clip search", "error", err)
		os.Exit(1)
	}
	mediaProviders := []provider.Provider{
		provider.Cached{Next: commons, KV: catalog, TTL: 15 * time.Minute},
		provider.Cached{Next: cities, KV: catalog, TTL: 15 * time.Minute},
		provider.Cached{Next: archive, KV: catalog, TTL: 15 * time.Minute},
		provider.Cached{Next: nasaLibrary, KV: catalog, TTL: 15 * time.Minute},
		provider.Cached{Next: yarnClips, KV: catalog, TTL: 15 * time.Minute},
	}
	referenceFetcher, err := reference.New(reference.Options{})
	if err != nil {
		logger.Error("configure temporary reference fetcher", "error", err)
		os.Exit(1)
	}
	var videoDecoder video.Decoder
	configuredVideoDecoder, err := ffmpeg.New(ffmpeg.Options{Executable: settings.FFmpegExecutable})
	if err != nil {
		logger.Info("short-video editor disabled; install FFmpeg or set GOGIF_FFMPEG_EXECUTABLE", "error", err)
	} else {
		videoDecoder = configuredVideoDecoder
	}
	var stillGenerator imagegen.Generator
	imageGeneratorName := settings.ImageGenerator
	if imageGeneratorName == "" {
		if settings.PaidImageEnabled && settings.ComfyUIAPIKey != "" {
			imageGeneratorName = "comfyui-cloud"
		} else if settings.PaidImageEnabled && settings.OpenAIAPIKey != "" {
			imageGeneratorName = "openai"
		} else if settings.ComfyUICheckpoint != "" {
			imageGeneratorName = "comfyui"
		}
	}
	switch imageGeneratorName {
	case "":
	case "blender":
		generator, err := blender.New(blender.Options{Executable: settings.BlenderExecutable})
		if err != nil {
			logger.Error("configure local Blender", "error", err)
			os.Exit(1)
		}
		stillGenerator = generator
	case "comfyui":
		if settings.ComfyUICheckpoint == "" {
			logger.Error("configure local ComfyUI", "error", "GOGIF_COMFYUI_CHECKPOINT is required")
			os.Exit(1)
		}
		generator, err := comfyui.New(comfyui.Options{
			Endpoint: settings.ComfyUIURL, Checkpoint: settings.ComfyUICheckpoint, InputDirectory: settings.ComfyUIInputDir,
		})
		if err != nil {
			logger.Error("configure local ComfyUI", "error", err)
			os.Exit(1)
		}
		stillGenerator = generator
	case "comfyui-cloud", "comfyui-partner":
		if !settings.PaidImageEnabled || settings.ComfyUIAPIKey == "" {
			logger.Error("configure hosted Comfy image generation", "error", "GOGIF_ENABLE_PAID_IMAGE_GENERATION=true and COMFY_CLOUD_API_KEY are required")
			os.Exit(1)
		}
		generator, err := comfypartner.New(comfypartner.Options{
			Endpoint: settings.ComfyUIImageURL, APIKey: settings.ComfyUIAPIKey, Recipe: settings.ComfyUIImageRecipe,
		})
		if err != nil {
			logger.Error("configure hosted Comfy image generation", "error", err)
			os.Exit(1)
		}
		stillGenerator = generator
	case "openai":
		if !settings.PaidImageEnabled || settings.OpenAIAPIKey == "" {
			logger.Error("configure OpenAI image generation", "error", "GOGIF_ENABLE_PAID_IMAGE_GENERATION=true and OPENAI_API_KEY are required")
			os.Exit(1)
		}
		generator, err := openaiimage.New(openaiimage.Options{
			APIKey: settings.OpenAIAPIKey, Model: settings.OpenAIImageModel,
			Quality: settings.OpenAIImageQuality, BaseURL: settings.OpenAIBaseURL,
		})
		if err != nil {
			logger.Error("configure OpenAI image generation", "error", err)
			os.Exit(1)
		}
		stillGenerator = generator
	default:
		logger.Error("configure image generator", "error", "GOGIF_IMAGE_GENERATOR must be blender, comfyui, comfyui-cloud, or openai")
		os.Exit(1)
	}

	var modelGenerator modelgen.Generator
	if settings.ModelGenerator != "" {
		switch settings.ModelGenerator {
		case "comfyui":
			if !settings.PaidModelEnabled || settings.ComfyUIAPIKey == "" {
				logger.Error("configure ComfyUI 3D generation", "error", "GOGIF_ENABLE_PAID_MODEL_GENERATION=true and COMFY_CLOUD_API_KEY are required")
				os.Exit(1)
			}
			generator, err := modelcomfy.New(modelcomfy.Options{
				Endpoint: settings.ComfyUIModelURL, APIKey: settings.ComfyUIAPIKey, DefaultRecipe: settings.ComfyUIModelRecipe,
			})
			if err != nil {
				logger.Error("configure ComfyUI 3D generation", "error", err)
				os.Exit(1)
			}
			modelGenerator = generator
		default:
			logger.Error("configure model generator", "error", "GOGIF_MODEL_GENERATOR must be comfyui")
			os.Exit(1)
		}
	}

	pipelineStatus := cinematic.DisabledDescriptor([]cinematic.StageDescriptor{
		cinematic.ProbeExecutable("blender", "Blender", "assets and geometry", settings.BlenderExecutable),
		cinematic.ProbeExecutable("unity-6.3", "Unity 6.3", "portable motion and transparent VFX", settings.UnityExecutable),
		cinematic.ProbeExecutable("unreal-5", "Unreal Engine 5", "cinematic beauty rendering", settings.UnrealExecutable),
		cinematic.ProbeExecutable("ffmpeg", "FFmpeg", "palette and GIF encoding", settings.FFmpegExecutable),
	})
	var cinematicRenderer cinematic.Renderer
	if settings.QualityPipelineEnabled {
		blenderStage, err := filmblender.New(filmblender.Options{Executable: settings.BlenderExecutable})
		if err != nil {
			logger.Error("configure cinematic Blender stage", "error", err)
			os.Exit(1)
		}
		unityStage, err := unity.New(unity.Options{
			Executable: settings.UnityExecutable, Project: settings.UnityProject, Method: settings.UnityMethod,
		})
		if err != nil {
			logger.Error("configure cinematic Unity stage", "error", err)
			os.Exit(1)
		}
		unrealStage, err := unreal.New(unreal.Options{
			Executable: settings.UnrealExecutable, Project: settings.UnrealProject, Script: settings.UnrealScript,
		})
		if err != nil {
			logger.Error("configure cinematic Unreal stage", "error", err)
			os.Exit(1)
		}
		encoder, err := filmffmpeg.New(filmffmpeg.Options{Executable: settings.FFmpegExecutable})
		if err != nil {
			logger.Error("configure cinematic FFmpeg encoder", "error", err)
			os.Exit(1)
		}
		var referenceGenerator imagegen.Generator
		if stillGenerator != nil && stillGenerator.Descriptor().ID != "blender-local" {
			referenceGenerator = stillGenerator
		}
		pipeline, err := cinematic.New(cinematic.PipelineOptions{
			ReferenceGenerator: referenceGenerator,
			Stages:             []cinematic.Stage{blenderStage, unityStage, unrealStage},
			Encoder:            encoder,
		})
		if err != nil {
			logger.Error("configure cinematic quality pipeline", "error", err)
			os.Exit(1)
		}
		cinematicRenderer = pipeline
		pipelineStatus = pipeline.Descriptor()
	}

	handler := httpapi.New(httpapi.Options{
		Planner:           animationPlanner,
		Logger:            logger,
		AIEnabled:         paidAIEnabled,
		AIModel:           settings.OpenAIModel,
		GiphyAPIKey:       settings.GiphyAPIKey,
		Catalog:           catalog,
		CatalogBackend:    catalogBackend,
		GeneratedSaver:    generatedSaver,
		GeneratedReader:   generatedReader,
		ModelSaver:        modelSaver,
		ModelReader:       modelReader,
		Providers:         mediaProviders,
		ImageGenerator:    stillGenerator,
		ModelGenerator:    modelGenerator,
		CinematicRenderer: cinematicRenderer,
		CinematicStatus:   pipelineStatus,
		ReferenceFetcher:  referenceFetcher,
		VideoDecoder:      videoDecoder,
		Auth:              authManager,
		Accounts:          accounts,
		Plans:             plans,
		Usage:             usage,
		LibraryCatalog:    mediaRepository,
		Billing:           stripeBilling,
		Scenes:            sceneRepository,
		SceneWorkerToken:  settings.SceneWorkerToken,
		SceneLease:        2 * time.Minute,
	})
	writeTimeout := 30 * time.Second
	if videoDecoder != nil {
		writeTimeout = 90 * time.Second
	}
	if stillGenerator != nil {
		writeTimeout = 5 * time.Minute
	}
	if cinematicRenderer != nil {
		writeTimeout = 30 * time.Minute
	}
	if modelGenerator != nil {
		writeTimeout = 30 * time.Minute
	}
	server := &http.Server{
		Addr:              settings.Address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       60 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			logger.Error("graceful shutdown", "error", err)
		}
	}()

	imageGeneratorID := "disabled"
	if stillGenerator != nil {
		imageGeneratorID = stillGenerator.Descriptor().ID
	}
	logger.Info("GoGIF is ready", "url", httpapi.AddressURL(settings.Address), "ai", paidAIEnabled, "image_generator", imageGeneratorID, "quality_pipeline", pipelineStatus.Enabled, "scene_jobs", sceneRepository != nil, "catalog", catalogBackend)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
