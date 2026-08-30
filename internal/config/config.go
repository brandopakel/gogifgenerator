package config

import "os"

type Config struct {
	Address       string
	OpenAIAPIKey  string
	OpenAIModel   string
	OpenAIBaseURL string
	GiphyAPIKey   string
}

func Load() Config {
	return Config{
		Address:       envOr("GOGIF_ADDR", ":8080"),
		OpenAIAPIKey:  os.Getenv("OPENAI_API_KEY"),
		OpenAIModel:   envOr("OPENAI_MODEL", "gpt-5-mini"),
		OpenAIBaseURL: envOr("OPENAI_BASE_URL", "https://api.openai.com/v1"),
		GiphyAPIKey:   os.Getenv("GIPHY_API_KEY"),
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
