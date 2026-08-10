package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	httpadapter "trample-back/internal/adapters/in/http"
	"trample-back/internal/adapters/out/scrydex"
	"trample-back/internal/adapters/out/trm"
	appCatalog "trample-back/internal/application/catalog"
	"trample-back/pkg/config"
	"trample-back/pkg/logger"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		os.Exit(1)
	}

	log := logger.New()

	scrydexClient := scrydex.NewClient(cfg.ScrydexAPIKey, cfg.ScrydexTeamID)
	trmClient := trm.NewClient()

	catalogUC := appCatalog.NewSearchScrydex(scrydexClient, trmClient)
	cardHandler := httpadapter.NewCardHandler(catalogUC)
	router := httpadapter.NewRouter(cardHandler)

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
