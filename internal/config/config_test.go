package config

import "testing"

func TestPaidAIRequiresExplicitOptIn(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "present-but-not-authorized")
	t.Setenv("GOGIF_ENABLE_PAID_AI", "")
	if settings := Load(); settings.PaidAIEnabled {
		t.Fatal("PaidAIEnabled = true without explicit opt-in")
	}

	t.Setenv("GOGIF_ENABLE_PAID_AI", "true")
	if settings := Load(); !settings.PaidAIEnabled {
		t.Fatal("PaidAIEnabled = false with explicit opt-in")
	}
}

func TestPaidImageGenerationRequiresSeparateExplicitOptIn(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "present-but-not-authorized")
	t.Setenv("GOGIF_ENABLE_PAID_AI", "true")
	t.Setenv("GOGIF_ENABLE_PAID_IMAGE_GENERATION", "")
	if settings := Load(); settings.PaidImageEnabled {
		t.Fatal("PaidImageEnabled = true without image opt-in")
	}

	t.Setenv("GOGIF_ENABLE_PAID_IMAGE_GENERATION", "true")
	t.Setenv("GOGIF_OPENAI_IMAGE_MODEL", "gpt-image-2")
	t.Setenv("GOGIF_OPENAI_IMAGE_QUALITY", "medium")
	settings := Load()
	if !settings.PaidImageEnabled || settings.OpenAIImageModel != "gpt-image-2" || settings.OpenAIImageQuality != "medium" {
		t.Fatalf("Load() = %#v", settings)
	}
}

func TestPaidModelGenerationRequiresSeparateExplicitOptIn(t *testing.T) {
	t.Setenv("COMFY_CLOUD_API_KEY", "present-but-not-authorized")
	t.Setenv("GOGIF_ENABLE_PAID_MODEL_GENERATION", "")
	if settings := Load(); settings.PaidModelEnabled {
		t.Fatal("PaidModelEnabled = true without explicit opt-in")
	}
	t.Setenv("GOGIF_ENABLE_PAID_MODEL_GENERATION", "true")
	t.Setenv("GOGIF_MODEL_GENERATOR", "comfyui")
	t.Setenv("GOGIF_COMFYUI_MODEL_URL", "https://cloud.comfy.org/api")
	t.Setenv("GOGIF_COMFYUI_MODEL_RECIPE", "hunyuan-3.1")
	settings := Load()
	if !settings.PaidModelEnabled || settings.ModelGenerator != "comfyui" || settings.ComfyUIModelRecipe != "hunyuan-3.1" || settings.ComfyUIAPIKey == "" {
		t.Fatalf("Load() = %#v", settings)
	}
}

func TestQualityPipelineRequiresExplicitOptIn(t *testing.T) {
	t.Setenv("GOGIF_ENABLE_QUALITY_PIPELINE", "")
	if settings := Load(); settings.QualityPipelineEnabled {
		t.Fatal("QualityPipelineEnabled = true without explicit opt-in")
	}
	t.Setenv("GOGIF_ENABLE_QUALITY_PIPELINE", "true")
	t.Setenv("GOGIF_UNITY_PROJECT", "/tmp/unity")
	t.Setenv("GOGIF_UNREAL_PROJECT", "/tmp/unreal.uproject")
	settings := Load()
	if !settings.QualityPipelineEnabled || settings.UnityProject != "/tmp/unity" || settings.UnrealProject != "/tmp/unreal.uproject" {
		t.Fatalf("Load() = %#v", settings)
	}
}
