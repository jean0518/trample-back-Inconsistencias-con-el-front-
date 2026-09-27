package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Port                   string
	DatabaseURL            string
	ScrydexAPIKey          string
	ScrydexTeamID          string
	ScrydexWebhookSecret   string
	JWTSecret              string
	AllowedOrigins         []string
	Env                    string
	SupabaseURL            string
	SupabaseServiceRoleKey string
	SupabaseStorageBucket  string
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

	rawOrigins := envOr("ALLOWED_ORIGINS", "http://localhost:5173")
	origins := strings.Split(rawOrigins, ",")
	for i, o := range origins {
		origins[i] = strings.TrimSpace(o)
	}

	return &Config{
		Port:                 envOr("PORT", "8080"),
		DatabaseURL:          dbURL,
		ScrydexAPIKey:        scrydexKey,
		ScrydexTeamID:        scrydexTeam,
		ScrydexWebhookSecret: envOr("SCRYDEX_WEBHOOK_SECRET", ""),
		JWTSecret:            jwtSecret,
		AllowedOrigins:       origins,
		Env:                  envOr("ENV", "development"),
		// Opcionales a propósito: sin ellas el catálogo sigue funcionando y
		// los frontales se guardan en la base en vez de en el bucket.
		SupabaseURL:            envOr("SUPABASE_URL", ""),
		SupabaseServiceRoleKey: envOr("SUPABASE_SERVICE_ROLE_KEY", ""),
		SupabaseStorageBucket:  envOr("SUPABASE_STORAGE_BUCKET", ""),
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
