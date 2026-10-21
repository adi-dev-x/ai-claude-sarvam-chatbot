// Package config loads settings from the environment, with a .env file as fallback.
package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	// BackendURL is the rental app (go-clean-code-eccomerce); BackendAPIKey must match its CHATBOT_API_KEY.
	BackendURL    string
	BackendAPIKey string

	Host          string
	Port          string
	AllowedOrigin string

	SarvamAPIKey    string
	AnthropicAPIKey string
}

// Load reads envFile (if present) without overriding variables already set in the
// environment, then builds the Config.
func Load(envFile string) (Config, error) {
	loadEnv(envFile)

	cfg := Config{
		BackendURL:    getenv("BACKEND_URL", "http://localhost:5080"),
		BackendAPIKey: os.Getenv("BACKEND_API_KEY"),

		Host:          getenv("HOST", "0.0.0.0"),
		Port:          getenv("SERVER_PORT", "5090"),
		AllowedOrigin: getenv("ALLOWED_ORIGIN", "*"),

		SarvamAPIKey:    os.Getenv("SARVAM_API_KEY"),
		AnthropicAPIKey: os.Getenv("ANTHROPIC_API_KEY"),
	}
	log.println("backenddd---urllll---", cfg.BackendURL)

	var missing []string
	for name, value := range map[string]string{
		"BACKEND_API_KEY":   cfg.BackendAPIKey,
		"SARVAM_API_KEY":    cfg.SarvamAPIKey,
		"ANTHROPIC_API_KEY": cfg.AnthropicAPIKey,
	} {
		if value == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return cfg, fmt.Errorf("missing required settings: %s", strings.Join(missing, ", "))
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// loadEnv reads KEY=VALUE lines from path without overriding variables
// that are already set in the environment.
func loadEnv(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}

		key = strings.TrimSpace(strings.TrimPrefix(key, "export "))
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, value)
		}
	}
}
