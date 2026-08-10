package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Port          string
	ScrydexAPIKey string
	ScrydexTeamID string
}

func Load() (*Config, error) {
	_ = godotenv.Load()
	return &Config{
		Port:          envOr("PORT", "8080"),
		ScrydexAPIKey: require("SCRYDEX_API_KEY"),
		ScrydexTeamID: require("SCRYDEX_TEAM_ID"),
	}, nil
}

func require(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic(fmt.Sprintf("env var %s is required", key))
	}
	return v
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
