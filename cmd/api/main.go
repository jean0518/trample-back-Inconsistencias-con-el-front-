// @title           Trample API
// @version         1.0
// @description     API para el catálogo de cartas coleccionables.
// @host            localhost:8080
// @BasePath        /
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Ingresá el token con el prefijo Bearer. Ej: "Bearer eyJ..."
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "trample-back/docs"
	httpadapter "trample-back/internal/adapters/in/http"
	"trample-back/internal/adapters/out/postgres"
	"trample-back/internal/adapters/out/scrydex"
	"trample-back/internal/adapters/out/trm"
	appAuth "trample-back/internal/application/auth"
	appCatalog "trample-back/internal/application/catalog"
	"trample-back/pkg/config"
	"trample-back/pkg/db"
	"trample-back/pkg/logger"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("configuración inválida", slog.Any("error", err))
		os.Exit(1)
	}

	log := logger.New()

	pool, err := db.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Error("no se pudo conectar a la base de datos", slog.Any("error", err))
		os.Exit(1)
	}
	defer pool.Close()

	// Clientes externos
	scrydexClient := scrydex.NewClient(cfg.ScrydexAPIKey, cfg.ScrydexTeamID)
	trmClient := trm.NewClient()

	// Repositorios
	expansionRepo := postgres.NewExpansionRepository(pool)
	cardRepo := postgres.NewCardRepository(pool)
	userRepo := postgres.NewUserRepository(pool)
	gameRepo := postgres.NewGameRepository(pool)

	// Casos de uso
	searchUC := appCatalog.NewSearchScrydex(scrydexClient, trmClient)
	syncExpansionsUC := appCatalog.NewSyncExpansionsUseCase(scrydexClient, expansionRepo)
	importCardUC := appCatalog.NewImportCardUseCase(searchUC, cardRepo, expansionRepo)
	listCardsUC := appCatalog.NewListCardsUseCase(cardRepo)
	gamesUC := appCatalog.NewGamesUseCase(gameRepo)
	registerUC := appAuth.NewRegisterUseCase(userRepo)
	loginUC := appAuth.NewLoginUseCase(userRepo, cfg.JWTSecret)

	// Router
	secureCookie := cfg.Env == "production"
	authMiddleware := httpadapter.NewAuthMiddleware([]byte(cfg.JWTSecret))
	router := httpadapter.NewRouter(httpadapter.Handlers{
		Auth:           httpadapter.NewAuthHandler(registerUC, loginUC, secureCookie),
		Games:          httpadapter.NewGamesHandler(gamesUC, syncExpansionsUC),
		Catalog:        httpadapter.NewCatalogHandler(listCardsUC),
		Pokemon:        httpadapter.NewPokemonHandler(searchUC, syncExpansionsUC, importCardUC),
		Magic:          httpadapter.NewMagicHandler(searchUC, syncExpansionsUC, importCardUC),
		Riftbound:      httpadapter.NewRiftboundHandler(searchUC),
		AuthMiddleware: authMiddleware,
		FrontendURL:    cfg.FrontendURL,
	})

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Info("server starting", slog.String("port", cfg.Port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("server error", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	<-quit
	log.Info("shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
