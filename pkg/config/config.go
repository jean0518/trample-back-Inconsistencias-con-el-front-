package config

import (
	"fmt"
	"os"
	"strconv"
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
	TelegramBotToken       string
	TelegramChatID         string

	// Bold — pagos en línea. Opcionales: sin llave de identidad el checkout en
	// línea queda desactivado y la tienda sigue funcionando con efectivo y
	// transferencia.
	BoldIdentityKey        string
	BoldSecretKey          string
	BoldEnv                string
	BoldAPIBaseURL         string
	BoldPaymentHoldMinutes int
	// Correo transaccional (Resend). Sin API key los correos no se envían, pero
	// la tienda funciona igual.
	ResendAPIKey string
	EmailFrom    string
	// Respuestas de clientes (el remitente es no-reply) y datos del pie de los
	// correos. EmailLogoURL debe ser un PNG público.
	EmailReplyTo     string
	EmailLogoURL     string
	StoreContactMail string
	StoreLocation    string
	StoreSocial      string
	// StoreWhatsApp es el número de WhatsApp de la tienda con indicativo. Los
	// correos invitan a escribir ahí para cambios o cancelaciones.
	StoreWhatsApp string
	// URL del frontend a la que Bold redirige tras el pago (…/carrito).
	FrontendURL string
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

	holdMinutes, err := strconv.Atoi(envOr("BOLD_PAYMENT_HOLD_MINUTES", "30"))
	if err != nil || holdMinutes <= 0 {
		holdMinutes = 30
	}

	rawOrigins := envOr("ALLOWED_ORIGINS", "http://localhost:5173")
	origins := strings.Split(rawOrigins, ",")
	for i, o := range origins {
		origins[i] = strings.TrimSpace(o)
	}

	frontendURL := envOr("FRONTEND_URL", "http://localhost:5173")
	contactMail := envOr("STORE_CONTACT_EMAIL", "hola@trample.co")

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
		// Opcionales: sin ellas el webhook actualiza precios igual, solo que
		// no avisa por Telegram.
		TelegramBotToken: envOr("TELEGRAM_BOT_TOKEN", ""),
		TelegramChatID:   envOr("TELEGRAM_CHAT_ID", ""),
		// Bold — pagos en línea. En el ambiente de pruebas la llave secreta de
		// webhook va vacía, así que no se exige. `BoldEnv` distingue test de
		// producción para las validaciones del webhook.
		BoldIdentityKey:        envOr("BOLD_IDENTITY_KEY", ""),
		BoldSecretKey:          envOr("BOLD_SECRET_KEY", ""),
		BoldEnv:                envOr("BOLD_ENV", "test"),
		BoldAPIBaseURL:         envOr("BOLD_API_BASE_URL", "https://payments.api.bold.co"),
		BoldPaymentHoldMinutes: holdMinutes,
		ResendAPIKey:           envOr("RESEND_API_KEY", ""),
		EmailFrom:              envOr("EMAIL_FROM", "Trample Store <no-reply@tramplestore.com>"),
		EmailReplyTo:           envOr("EMAIL_REPLY_TO", contactMail),
		EmailLogoURL:           envOr("EMAIL_LOGO_URL", strings.TrimRight(frontendURL, "/")+"/logos/email-logo.png"),
		StoreContactMail:       contactMail,
		StoreLocation:          envOr("STORE_LOCATION", "Barranquilla, Colombia"),
		StoreSocial:            envOr("STORE_SOCIAL", "@tramplestore"),
		// Mismo número que el frontend (src/constants/site.ts).
		StoreWhatsApp: envOr("STORE_WHATSAPP", "573150655368"),
		FrontendURL:   frontendURL,
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
