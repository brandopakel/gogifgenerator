package config

import (
	"os"
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
	OpenAIImageModel       string
	OpenAIImageQuality     string
	GiphyAPIKey            string
	MemKVAddress           string
	BlobDirectory          string
	ComfyUIURL             string
	ComfyUICheckpoint      string
	ComfyUIInputDir        string
	ComfyUIModelURL        string
	ComfyUIAPIKey          string
	ComfyUIModelRecipe     string
	ImageGenerator         string
	ModelGenerator         string
	BlenderExecutable      string
	FFmpegExecutable       string
	QualityPipelineEnabled bool
	UnityExecutable        string
	UnityProject           string
	UnityMethod            string
	UnrealExecutable       string
	UnrealProject          string
	UnrealScript           string
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
		OpenAIImageModel:       envOr("GOGIF_OPENAI_IMAGE_MODEL", "gpt-image-2"),
		OpenAIImageQuality:     strings.ToLower(strings.TrimSpace(envOr("GOGIF_OPENAI_IMAGE_QUALITY", "high"))),
		GiphyAPIKey:            os.Getenv("GIPHY_API_KEY"),
		MemKVAddress:           os.Getenv("GOGIF_MEMKV_ADDR"),
		BlobDirectory:          envOr("GOGIF_BLOB_DIR", ".data/blobs"),
		ComfyUIURL:             comfyUIURL,
		ComfyUICheckpoint:      os.Getenv("GOGIF_COMFYUI_CHECKPOINT"),
		ComfyUIInputDir:        os.Getenv("GOGIF_COMFYUI_INPUT_DIR"),
		ComfyUIModelURL:        envOr("GOGIF_COMFYUI_MODEL_URL", comfyUIURL),
		ComfyUIAPIKey:          os.Getenv("COMFY_CLOUD_API_KEY"),
		ComfyUIModelRecipe:     strings.ToLower(strings.TrimSpace(envOr("GOGIF_COMFYUI_MODEL_RECIPE", "tripo-3.1"))),
		ImageGenerator:         strings.ToLower(strings.TrimSpace(os.Getenv("GOGIF_IMAGE_GENERATOR"))),
		ModelGenerator:         strings.ToLower(strings.TrimSpace(os.Getenv("GOGIF_MODEL_GENERATOR"))),
		BlenderExecutable:      envOr("GOGIF_BLENDER_EXECUTABLE", "blender"),
		FFmpegExecutable:       envOr("GOGIF_FFMPEG_EXECUTABLE", "ffmpeg"),
		QualityPipelineEnabled: envBool("GOGIF_ENABLE_QUALITY_PIPELINE"),
		UnityExecutable:        envOr("GOGIF_UNITY_EXECUTABLE", "Unity"),
		UnityProject:           os.Getenv("GOGIF_UNITY_PROJECT"),
		UnityMethod:            envOr("GOGIF_UNITY_METHOD", "GoGIF.Editor.BatchRenderer.Render"),
		UnrealExecutable:       envOr("GOGIF_UNREAL_EXECUTABLE", "UnrealEditor-Cmd"),
		UnrealProject:          os.Getenv("GOGIF_UNREAL_PROJECT"),
		UnrealScript:           os.Getenv("GOGIF_UNREAL_SCRIPT"),
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
