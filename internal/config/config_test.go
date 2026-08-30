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
