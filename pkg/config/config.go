package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Port          string
	DatabaseURL   string
	ScrydexAPIKey string
	ScrydexTeamID string
	JWTSecret     string
}

func Load() (*Config, error) {
	_ = godotenv.Load()
	return &Config{
		Port:          envOr("PORT", "8080"),
		DatabaseURL:   require("DATABASE_URL"),
		ScrydexAPIKey: require("SCRYDEX_API_KEY"),
		ScrydexTeamID: require("SCRYDEX_TEAM_ID"),
		JWTSecret:     require("JWT_SECRET"),
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
