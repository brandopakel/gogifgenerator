package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Address                string
	OpenAIAPIKey           string
	OpenAIModel            string
	OpenAIBaseURL          string
	PaidAIEnabled          bool
	PaidImageEnabled       bool
	PaidModelEnabled       bool
	PaidMotionEnabled      bool
	SemanticMaxAttempts    int
	OpenAIImageModel       string
	OpenAIImageQuality     string
	GiphyAPIKey            string
	MemKVAddress           string
	BlobDirectory          string
	ComfyUIURL             string
	ComfyUICheckpoint      string
	ComfyUIInputDir        string
	ComfyUIImageURL        string
	ComfyUIImageRecipe     string
	ComfyUIModelURL        string
	ComfyUIAPIKey          string
	ComfyUIModelRecipe     string
	ComfyUIRecipe          string
	ComfyUIGGUFUNet        string
	ComfyUIGGUFClipL       string
	ComfyUIGGUFClipT5      string
	ComfyUIGGUFVAE         string
	ComfyUIGGUFGuidance    float64
	ComfyUIPrivateEndpoint bool
	ComfyUIAuthToken       string
	ComfyUISteps           int
	HuggingFaceAPIKey      string
	HuggingFaceModel       string
	HuggingFaceBaseURL     string
	Planner                string
	EmbeddingProvider      string
	EmbeddingModel         string
	EmbeddingURL           string
	EmbeddingWeight        float64
	ImageGenerator         string
	ModelGenerator         string
	BlenderExecutable      string
	FFmpegExecutable       string
	QualityPipelineEnabled bool
	SceneJobsEnabled       bool
	SceneWorkerToken       string
	SceneTargets           []string
	UnityExecutable        string
	UnityProject           string
	UnityMethod            string
	UnrealExecutable       string
	UnrealProject          string
	UnrealScript           string
	PublicURL              string
	AuthMode               string
	SessionSecret          string
	LocalOwnerEmail        string
	OIDCIssuer             string
	OIDCClientID           string
	OIDCClientSecret       string
	OIDCRedirectURL        string
	BillingEnabled         bool
	StripeSecretKey        string
	StripeWebhookSecret    string
	StripeCreatorPriceID   string
	StripeProPriceID       string
	CreatorPriceCents      int
	ProPriceCents          int
}

func Load() Config {
	comfyUIURL := envOr("GOGIF_COMFYUI_URL", "http://127.0.0.1:8188")
	return Config{
		Address:                envOr("GOGIF_ADDR", ":8080"),
		OpenAIAPIKey:           os.Getenv("OPENAI_API_KEY"),
		OpenAIModel:            envOr("OPENAI_MODEL", "gpt-5-mini"),
		OpenAIBaseURL:          envOr("OPENAI_BASE_URL", "https://api.openai.com/v1"),
		PaidAIEnabled:          envBool("GOGIF_ENABLE_PAID_AI"),
		PaidImageEnabled:       envBool("GOGIF_ENABLE_PAID_IMAGE_GENERATION"),
		PaidModelEnabled:       envBool("GOGIF_ENABLE_PAID_MODEL_GENERATION"),
		PaidMotionEnabled:      envBool("GOGIF_ENABLE_PAID_MOTION_GENERATION"),
		SemanticMaxAttempts:    envInt("GOGIF_SEMANTIC_MAX_ATTEMPTS", 2),
		OpenAIImageModel:       envOr("GOGIF_OPENAI_IMAGE_MODEL", "gpt-image-2"),
		OpenAIImageQuality:     strings.ToLower(strings.TrimSpace(envOr("GOGIF_OPENAI_IMAGE_QUALITY", "high"))),
		GiphyAPIKey:            os.Getenv("GIPHY_API_KEY"),
		MemKVAddress:           os.Getenv("GOGIF_MEMKV_ADDR"),
		BlobDirectory:          envOr("GOGIF_BLOB_DIR", ".data/blobs"),
		ComfyUIURL:             comfyUIURL,
		ComfyUICheckpoint:      os.Getenv("GOGIF_COMFYUI_CHECKPOINT"),
		ComfyUIInputDir:        os.Getenv("GOGIF_COMFYUI_INPUT_DIR"),
		ComfyUIImageURL:        envOr("GOGIF_COMFYUI_IMAGE_URL", "https://cloud.comfy.org/api"),
		ComfyUIImageRecipe:     strings.ToLower(strings.TrimSpace(envOr("GOGIF_COMFYUI_IMAGE_RECIPE", "flux-ultra"))),
		ComfyUIModelURL:        envOr("GOGIF_COMFYUI_MODEL_URL", comfyUIURL),
		ComfyUIAPIKey:          os.Getenv("COMFY_CLOUD_API_KEY"),
		ComfyUIModelRecipe:     strings.ToLower(strings.TrimSpace(envOr("GOGIF_COMFYUI_MODEL_RECIPE", "tripo-3.1"))),
		ComfyUIRecipe:          strings.ToLower(strings.TrimSpace(envOr("GOGIF_COMFYUI_RECIPE", "sd-checkpoint"))),
		ComfyUIGGUFUNet:        os.Getenv("GOGIF_COMFYUI_GGUF_UNET"),
		ComfyUIGGUFClipL:       os.Getenv("GOGIF_COMFYUI_GGUF_CLIP_L"),
		ComfyUIGGUFClipT5:      os.Getenv("GOGIF_COMFYUI_GGUF_CLIP_T5"),
		ComfyUIGGUFVAE:         os.Getenv("GOGIF_COMFYUI_GGUF_VAE"),
		ComfyUIGGUFGuidance:    envFloat("GOGIF_COMFYUI_GGUF_GUIDANCE", 3.5),
		ComfyUIPrivateEndpoint: envBool("GOGIF_COMFYUI_PRIVATE_ENDPOINT"),
		ComfyUIAuthToken:       os.Getenv("GOGIF_COMFYUI_AUTH_TOKEN"),
		ComfyUISteps:           envInt("GOGIF_COMFYUI_STEPS", 0),
		HuggingFaceAPIKey:      os.Getenv("HUGGINGFACE_API_KEY"),
		HuggingFaceModel:       envOr("GOGIF_HUGGINGFACE_MODEL", "openai/gpt-oss-120b:cheapest"),
		HuggingFaceBaseURL:     envOr("GOGIF_HUGGINGFACE_BASE_URL", "https://router.huggingface.co/v1"),
		Planner:                strings.ToLower(strings.TrimSpace(os.Getenv("GOGIF_PLANNER"))),
		EmbeddingProvider:      strings.ToLower(strings.TrimSpace(envOr("GOGIF_EMBEDDING_PROVIDER", "lexical"))),
		EmbeddingModel:         envOr("GOGIF_EMBEDDING_MODEL", "BAAI/bge-small-en-v1.5"),
		EmbeddingURL:           envOr("GOGIF_EMBEDDING_URL", "https://router.huggingface.co"),
		EmbeddingWeight:        envFloat("GOGIF_EMBEDDING_WEIGHT", 0.6),
		ImageGenerator:         strings.ToLower(strings.TrimSpace(os.Getenv("GOGIF_IMAGE_GENERATOR"))),
		ModelGenerator:         strings.ToLower(strings.TrimSpace(os.Getenv("GOGIF_MODEL_GENERATOR"))),
		BlenderExecutable:      envOr("GOGIF_BLENDER_EXECUTABLE", "blender"),
		FFmpegExecutable:       envOr("GOGIF_FFMPEG_EXECUTABLE", "ffmpeg"),
		QualityPipelineEnabled: envBool("GOGIF_ENABLE_QUALITY_PIPELINE"),
		SceneJobsEnabled:       envBool("GOGIF_ENABLE_SCENE_JOBS"),
		SceneWorkerToken:       os.Getenv("GOGIF_SCENE_WORKER_TOKEN"),
		SceneTargets:           envList("GOGIF_SCENE_TARGETS", []string{"unreal"}),
		UnityExecutable:        envOr("GOGIF_UNITY_EXECUTABLE", "Unity"),
		UnityProject:           os.Getenv("GOGIF_UNITY_PROJECT"),
		UnityMethod:            envOr("GOGIF_UNITY_METHOD", "GoGIF.Editor.BatchRenderer.Render"),
		UnrealExecutable:       envOr("GOGIF_UNREAL_EXECUTABLE", "UnrealEditor-Cmd"),
		UnrealProject:          os.Getenv("GOGIF_UNREAL_PROJECT"),
		UnrealScript:           os.Getenv("GOGIF_UNREAL_SCRIPT"),
		PublicURL:              envOr("GOGIF_PUBLIC_URL", "http://localhost:8080"),
		AuthMode:               strings.ToLower(strings.TrimSpace(envOr("GOGIF_AUTH_MODE", "disabled"))),
		SessionSecret:          os.Getenv("GOGIF_SESSION_SECRET"),
		LocalOwnerEmail:        os.Getenv("GOGIF_LOCAL_OWNER_EMAIL"),
		OIDCIssuer:             os.Getenv("GOGIF_OIDC_ISSUER"),
		OIDCClientID:           os.Getenv("GOGIF_OIDC_CLIENT_ID"),
		OIDCClientSecret:       os.Getenv("GOGIF_OIDC_CLIENT_SECRET"),
		OIDCRedirectURL:        os.Getenv("GOGIF_OIDC_REDIRECT_URL"),
		BillingEnabled:         envBool("GOGIF_ENABLE_BILLING"),
		StripeSecretKey:        os.Getenv("STRIPE_SECRET_KEY"),
		StripeWebhookSecret:    os.Getenv("STRIPE_WEBHOOK_SECRET"),
		StripeCreatorPriceID:   os.Getenv("STRIPE_CREATOR_PRICE_ID"),
		StripeProPriceID:       os.Getenv("STRIPE_PRO_PRICE_ID"),
		CreatorPriceCents:      envInt("GOGIF_CREATOR_PRICE_CENTS", 1500),
		ProPriceCents:          envInt("GOGIF_PRO_PRICE_CENTS", 3900),
	}
}

func envBool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envInt(name string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func envList(name string, fallback []string) []string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return append([]string(nil), fallback...)
	}
	result := make([]string, 0)
	seen := make(map[string]bool)
	for _, item := range strings.Split(value, ",") {
		item = strings.ToLower(strings.TrimSpace(item))
		if item != "" && !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}

func envFloat(key string, fallback float64) float64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(os.Getenv(key)), 64)
	if err != nil {
		return fallback
	}
	return value
}
