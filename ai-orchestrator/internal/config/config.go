package config

import (
	"os"
	"strconv"
)

type Config struct {
	Server   ServerConfig
	LLM      LLMConfig
	Database DatabaseConfig
}

type ServerConfig struct {
	Port int
}

type LLMConfig struct {
	DefaultProvider string
	Providers       []ProviderConfig
}

type ProviderConfig struct {
	Name     string
	Type     string // "anthropic", "openai", "ollama"
	APIKey   string
	Endpoint string
	Model    string
	MaxTokens int
	Priority  int // Lower = higher priority
}

type DatabaseConfig struct {
	URL          string
	MaxOpenConns int
}

func Load() (*Config, error) {
	port, _ := strconv.Atoi(getEnv("ORCHESTRATOR_PORT", "8081"))

	cfg := &Config{
		Server: ServerConfig{
			Port: port,
		},
		LLM: LLMConfig{
			DefaultProvider: getEnv("LLM_DEFAULT_PROVIDER", "anthropic"),
			Providers: []ProviderConfig{
				{
					Name:      "anthropic",
					Type:      "anthropic",
					APIKey:    getEnv("ANTHROPIC_API_KEY", ""),
					Endpoint:  "https://api.anthropic.com",
					Model:     getEnv("ANTHROPIC_MODEL", "claude-sonnet-4-20250514"),
					MaxTokens: 4096,
					Priority:  1,
				},
				{
					Name:      "openai",
					Type:      "openai",
					APIKey:    getEnv("OPENAI_API_KEY", ""),
					Endpoint:  "https://api.openai.com",
					Model:     getEnv("OPENAI_MODEL", "gpt-4o"),
					MaxTokens: 4096,
					Priority:  2,
				},
			},
		},
		Database: DatabaseConfig{
			URL:          getEnv("DATABASE_URL", "postgres://nexusgate:nexusgate@localhost:5432/nexusgate?sslmode=disable"),
			MaxOpenConns: 25,
		},
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
