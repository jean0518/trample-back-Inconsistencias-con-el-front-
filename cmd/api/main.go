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

	httpadapter "trample-back/internal/adapters/in/http"
	"trample-back/internal/adapters/out/postgres"
	"trample-back/internal/adapters/out/scrydex"
	"trample-back/internal/adapters/out/trm"
	appAdmin "trample-back/internal/application/admin"
	appAuth "trample-back/internal/application/auth"
	appCatalog "trample-back/internal/application/catalog"
	appListing "trample-back/internal/application/listing"
	appOwner "trample-back/internal/application/owner"
	appReservation "trample-back/internal/application/reservation"
	appSale "trample-back/internal/application/sale"
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
	adminUserRepo := postgres.NewAdminUserRepository(pool)
	panelRepo := postgres.NewPanelRepository(pool)
	gameRepo := postgres.NewGameRepository(pool)
	listingRepo := postgres.NewListingRepository(pool)
	ownerRepo := postgres.NewOwnerRepository(pool)
	reservationRepo := postgres.NewReservationRepository(pool)
	saleRepo := postgres.NewSaleRepository(pool)

	// Casos de uso
	searchUC := appCatalog.NewSearchScrydex(scrydexClient, trmClient)
	syncExpansionsUC := appCatalog.NewSyncExpansionsUseCase(scrydexClient, expansionRepo)
	importCardUC := appCatalog.NewImportCardUseCase(searchUC, cardRepo)
	importListingUC := appCatalog.NewImportListingUseCase(searchUC, cardRepo, listingRepo, ownerRepo, trmClient)
	priceRefresherUC := appCatalog.NewPriceRefresher(searchUC, cardRepo, log)
	listCardsUC := appCatalog.NewListCardsUseCase(cardRepo, priceRefresherUC)
	gamesUC := appCatalog.NewGamesUseCase(gameRepo)
	registerUC := appAuth.NewRegisterUseCase(userRepo)
	loginUC := appAuth.NewLoginUseCase(userRepo, cfg.JWTSecret)
	createListingUC := appListing.NewCreateListingUseCase(listingRepo, ownerRepo, trmClient)
	listListingsUC := appListing.NewListListingsUseCase(listingRepo)
	updateStockUC := appListing.NewUpdateStockUseCase(listingRepo)
	deleteListingUC := appListing.NewDeleteListingUseCase(listingRepo)
	createOwnerUC := appOwner.NewCreateOwnerUseCase(ownerRepo)
	updateOwnerUC := appOwner.NewUpdateOwnerUseCase(ownerRepo)
	listOwnersUC := appOwner.NewListOwnersUseCase(ownerRepo)
	deleteOwnerUC := appOwner.NewDeleteOwnerUseCase(ownerRepo)
	createAdminUC := appAdmin.NewCreateAdminUseCase(userRepo, adminUserRepo, panelRepo)
	listAdminsUC := appAdmin.NewListAdminsUseCase(adminUserRepo)
	updateRoleUC := appAdmin.NewUpdateRoleUseCase(adminUserRepo)
	updatePanelsUC := appAdmin.NewUpdatePanelsUseCase(adminUserRepo, panelRepo)
	updateAdminUserUC := appAdmin.NewUpdateAdminUserUseCase(userRepo, adminUserRepo, panelRepo)
	panelsListUC := appAdmin.NewPanelsListUseCase(panelRepo)
	deleteAdminUC := appAdmin.NewDeleteAdminUseCase(adminUserRepo)
	cartUC := appReservation.NewCartUseCase(reservationRepo)
	confirmSaleUC := appSale.NewConfirmSaleUseCase(reservationRepo, saleRepo)
	listSalesUC := appSale.NewListSalesUseCase(saleRepo)
	statsSalesUC := appSale.NewSaleStatsUseCase(saleRepo)

	// Router
	secureCookie := cfg.Env == "production"
	authMiddleware := httpadapter.NewAuthMiddleware([]byte(cfg.JWTSecret))
	router := httpadapter.NewRouter(httpadapter.Handlers{
		Auth:           httpadapter.NewAuthHandler(registerUC, loginUC, secureCookie),
		Games:          httpadapter.NewGamesHandler(gamesUC, syncExpansionsUC),
		Catalog:        httpadapter.NewCatalogHandler(listCardsUC),
		Pokemon:        httpadapter.NewPokemonHandler(searchUC, syncExpansionsUC, importCardUC, importListingUC),
		Magic:          httpadapter.NewMagicHandler(searchUC, syncExpansionsUC, importCardUC, importListingUC),
		Riftbound:      httpadapter.NewRiftboundHandler(searchUC, syncExpansionsUC),
		Listings:       httpadapter.NewListingHandler(createListingUC, listListingsUC, updateStockUC, deleteListingUC),
		Owners:         httpadapter.NewOwnerHandler(createOwnerUC, updateOwnerUC, listOwnersUC, deleteOwnerUC),
		AdminUsers:     httpadapter.NewAdminUserHandler(createAdminUC, listAdminsUC, updateRoleUC, updatePanelsUC, updateAdminUserUC, deleteAdminUC, panelsListUC),
		Cart:           httpadapter.NewCartHandler(cartUC),
		Sales:          httpadapter.NewSaleHandler(confirmSaleUC, listSalesUC, statsSalesUC),
		AuthMiddleware: authMiddleware,
		AllowedOrigins: cfg.AllowedOrigins,
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

	// Job de respaldo: refresca en lotes las cartas cuyo precio en Scrydex
	// tiene más de una semana, para cubrir también las que nadie consulta
	// (el refresco perezoso en ListCardsUseCase solo cubre cartas que se
	// listan).
	bgCtx, cancelBg := context.WithCancel(context.Background())
	go runPriceRefreshJob(bgCtx, priceRefresherUC, log)

	// Libera periódicamente las reservas (stock temporal de 5 min) vencidas.
	go releaseExpiredLoop(log, reservationRepo)

	go func() {
		log.Info("server starting", slog.String("port", cfg.Port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("server error", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	<-quit
	log.Info("shutting down...")
	cancelBg()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

// runPriceRefreshJob es el respaldo periódico del refresco perezoso de
// precios: cada tick busca cartas con más de una semana sin consultarse en
// Scrydex y las actualiza en lotes hasta agotar el backlog.
func runPriceRefreshJob(ctx context.Context, refresher *appCatalog.PriceRefresher, log *slog.Logger) {
	const (
		tick      = 6 * time.Hour
		batchSize = 50
	)

	refreshUntilCaughtUp := func() {
		for {
			n, err := refresher.RefreshStaleBatch(ctx, appCatalog.PriceStaleAfter, batchSize)
			if err != nil {
				log.Error("job de refresco de precios falló", slog.Any("error", err))
				return
			}
			if n < batchSize {
				return
			}
		}
	}

	refreshUntilCaughtUp()

	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refreshUntilCaughtUp()
		}
	}
}

// releaseExpiredLoop libera en un ciclo las reservas de carrito vencidas
// (5 minutos) restaurando el stock al inventario.
func releaseExpiredLoop(log *slog.Logger, repo *postgres.ReservationRepository) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		n, err := repo.ReleaseExpired(ctx)
		cancel()
		if err != nil {
			log.Error("liberar reservas expiradas", slog.Any("error", err))
			continue
		}
		if n > 0 {
			log.Info("reservas expiradas liberadas", slog.Int64("count", n))
		}
	}
}
