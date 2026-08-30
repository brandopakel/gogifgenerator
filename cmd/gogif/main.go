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

	"github.com/brandopakel/gogifgenerator/internal/cinematic"
	filmblender "github.com/brandopakel/gogifgenerator/internal/cinematic/blender"
	filmffmpeg "github.com/brandopakel/gogifgenerator/internal/cinematic/ffmpeg"
	"github.com/brandopakel/gogifgenerator/internal/cinematic/unity"
	"github.com/brandopakel/gogifgenerator/internal/cinematic/unreal"
	"github.com/brandopakel/gogifgenerator/internal/config"
	"github.com/brandopakel/gogifgenerator/internal/httpapi"
	"github.com/brandopakel/gogifgenerator/internal/imagegen"
	"github.com/brandopakel/gogifgenerator/internal/imagegen/blender"
	"github.com/brandopakel/gogifgenerator/internal/imagegen/comfyui"
	"github.com/brandopakel/gogifgenerator/internal/media"
	"github.com/brandopakel/gogifgenerator/internal/planner"
	"github.com/brandopakel/gogifgenerator/internal/provider"
	"github.com/brandopakel/gogifgenerator/internal/provider/gifcities"
	"github.com/brandopakel/gogifgenerator/internal/provider/nasa"
	"github.com/brandopakel/gogifgenerator/internal/provider/prelinger"
	"github.com/brandopakel/gogifgenerator/internal/provider/wikimedia"
	"github.com/brandopakel/gogifgenerator/internal/reference"
	"github.com/brandopakel/gogifgenerator/internal/store"
	"github.com/brandopakel/gogifgenerator/internal/video"
	"github.com/brandopakel/gogifgenerator/internal/video/ffmpeg"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	settings := config.Load()
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
	library := media.NewLibrary(media.NewRepository(catalog), blobs)
	var generatedSaver media.GeneratedSaver = library
	var generatedReader media.GeneratedReader = library
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
	mediaProviders := []provider.Provider{
		provider.Cached{Next: commons, KV: catalog, TTL: 15 * time.Minute},
		provider.Cached{Next: cities, KV: catalog, TTL: 15 * time.Minute},
		provider.Cached{Next: archive, KV: catalog, TTL: 15 * time.Minute},
		provider.Cached{Next: nasaLibrary, KV: catalog, TTL: 15 * time.Minute},
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
	if imageGeneratorName == "" && settings.ComfyUICheckpoint != "" {
		imageGeneratorName = "comfyui"
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
	default:
		logger.Error("configure local image generator", "error", "GOGIF_IMAGE_GENERATOR must be blender or comfyui")
		os.Exit(1)
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
		Providers:         mediaProviders,
		ImageGenerator:    stillGenerator,
		CinematicRenderer: cinematicRenderer,
		CinematicStatus:   pipelineStatus,
		ReferenceFetcher:  referenceFetcher,
		VideoDecoder:      videoDecoder,
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
	logger.Info("GoGIF is ready", "url", httpapi.AddressURL(settings.Address), "ai", paidAIEnabled, "image_generator", imageGeneratorID, "quality_pipeline", pipelineStatus.Enabled, "catalog", catalogBackend)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
