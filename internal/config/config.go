package config

import (
	"os"
	"strings"
)

type Config struct {
	Address       string
	OpenAIAPIKey  string
	OpenAIModel   string
	OpenAIBaseURL string
	PaidAIEnabled bool
	GiphyAPIKey   string
	MemKVAddress  string
	BlobDirectory string
}

func Load() Config {
	return Config{
		Address:       envOr("GOGIF_ADDR", ":8080"),
		OpenAIAPIKey:  os.Getenv("OPENAI_API_KEY"),
		OpenAIModel:   envOr("OPENAI_MODEL", "gpt-5-mini"),
		OpenAIBaseURL: envOr("OPENAI_BASE_URL", "https://api.openai.com/v1"),
		PaidAIEnabled: envBool("GOGIF_ENABLE_PAID_AI"),
		GiphyAPIKey:   os.Getenv("GIPHY_API_KEY"),
		MemKVAddress:  os.Getenv("GOGIF_MEMKV_ADDR"),
		BlobDirectory: envOr("GOGIF_BLOB_DIR", ".data/blobs"),
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
