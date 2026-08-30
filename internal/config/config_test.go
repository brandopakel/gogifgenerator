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
