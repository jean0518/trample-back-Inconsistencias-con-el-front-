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

	dbURL, err := require("DATABASE_URL")
	if err != nil {
		return nil, err
	}
	scrydexKey, err := require("SCRYDEX_API_KEY")
	if err != nil {
		return nil, err
	}
	scrydexTeam, err := require("SCRYDEX_TEAM_ID")
	if err != nil {
		return nil, err
	}
	jwtSecret, err := require("JWT_SECRET")
	if err != nil {
		return nil, err
	}

	return &Config{
		Port:          envOr("PORT", "8080"),
		DatabaseURL:   dbURL,
		ScrydexAPIKey: scrydexKey,
		ScrydexTeamID: scrydexTeam,
		JWTSecret:     jwtSecret,
	}, nil
}

func require(key string) (string, error) {
	v := os.Getenv(key)
	if v == "" {
		return "", fmt.Errorf("env var %s is required", key)
	}
	return v, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
