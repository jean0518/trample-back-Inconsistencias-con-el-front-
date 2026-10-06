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
	"strings"
	"syscall"
	"time"

	httpadapter "trample-back/internal/adapters/in/http"
	"trample-back/internal/adapters/out/bold"
	"trample-back/internal/adapters/out/email"
	"trample-back/internal/adapters/out/postgres"
	"trample-back/internal/adapters/out/scrydex"
	"trample-back/internal/adapters/out/supabase"
	"trample-back/internal/adapters/out/telegram"
	"trample-back/internal/adapters/out/trm"
	appAdmin "trample-back/internal/application/admin"
	appAuth "trample-back/internal/application/auth"
	appCatalog "trample-back/internal/application/catalog"
	appDashboard "trample-back/internal/application/dashboard"
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
	// Pasarela de pagos. Sin BOLD_IDENTITY_KEY el checkout en línea queda
	// deshabilitado y la tienda sigue funcionando con transferencia/efectivo.
	boldClient := bold.NewClient(cfg.BoldIdentityKey, cfg.BoldSecretKey, cfg.BoldAPIBaseURL)
	if boldClient.Configured() && !strings.HasPrefix(cfg.FrontendURL, "https://") {
		// Bold exige https en la URL de redirección: con http el botón falla en el
		// navegador con BTN-001 y el cliente no puede pagar.
		log.Warn("FRONTEND_URL no usa https: el botón de Bold fallará con BTN-001",
			slog.String("FRONTEND_URL", cfg.FrontendURL))
	}
	if boldClient.Configured() && cfg.BoldEnv != "production" {
		// En este modo el webhook se firma con llave vacía (cualquiera puede
		// generar la firma). Es correcto para el ambiente de pruebas de Bold, pero
		// si ves esto en producción falta BOLD_ENV=production.
		log.Warn("Bold en modo prueba: las notificaciones no se verifican con la llave secreta",
			slog.String("BOLD_ENV", cfg.BoldEnv))
	}

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
	dashboardRepo := postgres.NewDashboardRepository(pool)

	// Casos de uso
	searchUC := appCatalog.NewSearchScrydex(scrydexClient, trmClient)
	priceByLanguageUC := appCatalog.NewPriceByLanguageUseCase(searchUC)
	syncExpansionsUC := appCatalog.NewSyncExpansionsUseCase(scrydexClient, expansionRepo)
	importCardUC := appCatalog.NewImportCardUseCase(searchUC, cardRepo)
	importListingUC := appCatalog.NewImportListingUseCase(searchUC, cardRepo, listingRepo, ownerRepo, trmClient)
	// Alta de inventario desde el panel: el alta manual de cartas y el botón "+"
	// que crea una expansión sin pasar por Scrydex.
	manualCardRepo := postgres.NewManualCardRepositoryPG(pool)
	// Las imágenes van a Supabase Storage si hay bucket configurado; si falta
	// alguna variable, storage queda nil y los frontales se guardan en la base
	// como hasta ahora, para que el arranque no dependa del bucket.
	imageStorage := supabase.NewStorage(cfg.SupabaseURL, cfg.SupabaseServiceRoleKey, cfg.SupabaseStorageBucket)
	cardImageRepo := postgres.NewCardImageRepositoryPG(pool, imageStorage)
	manualListingUC := appCatalog.NewManualListingUseCase(manualCardRepo, listingRepo, ownerRepo, cardImageRepo, trmClient)
	newExpansionUC := appCatalog.NewNewExpansionUseCase(expansionRepo)
	priceRefresherUC := appCatalog.NewPriceRefresher(searchUC, cardRepo, log)
	// Aviso por Telegram de los precios del inventario que cambian con el
	// webhook. Si faltan las variables queda desactivado.
	priceNotifier := telegram.NewNotifier(cfg.TelegramBotToken, cfg.TelegramChatID)
	processWebhookUC := appCatalog.NewProcessWebhookUseCase(searchUC, cardRepo, priceNotifier, log)
	listCardsUC := appCatalog.NewListCardsUseCase(cardRepo)
	gamesUC := appCatalog.NewGamesUseCase(gameRepo)
	registerUC := appAuth.NewRegisterUseCase(userRepo)
	loginUC := appAuth.NewLoginUseCase(userRepo, cfg.JWTSecret)
	createListingUC := appListing.NewCreateListingUseCase(listingRepo, ownerRepo, trmClient)
	listListingsUC := appListing.NewListListingsUseCase(listingRepo)
	updateStockUC := appListing.NewUpdateStockUseCase(listingRepo)
	editListingUC := appListing.NewEditListingUseCase(listingRepo, trmClient)
	deleteListingUC := appListing.NewDeleteListingUseCase(listingRepo)
	createOwnerUC := appOwner.NewCreateOwnerUseCase(ownerRepo)
	updateOwnerUC := appOwner.NewUpdateOwnerUseCase(ownerRepo)
	listOwnersUC := appOwner.NewListOwnersUseCase(ownerRepo)
	deleteOwnerUC := appOwner.NewDeleteOwnerUseCase(ownerRepo)
	createAdminUC := appAdmin.NewCreateAdminUseCase(userRepo, adminUserRepo, panelRepo)
	listAdminsUC := appAdmin.NewListAdminsUseCase(adminUserRepo)
	updateAdminUserUC := appAdmin.NewUpdateAdminUserUseCase(userRepo, adminUserRepo, panelRepo)
	panelsListUC := appAdmin.NewPanelsListUseCase(panelRepo)
	deleteAdminUC := appAdmin.NewDeleteAdminUseCase(adminUserRepo)
	cartUC := appReservation.NewCartUseCase(reservationRepo)
	// El pago en línea no registra el pedido: deja listo el checkout con la
	// referencia de la pasarela. El pedido se crea al aprobarse el pago.
	createCheckoutUC := appSale.NewCreateCheckoutUseCase(reservationRepo, saleRepo, boldClient, cfg.BoldPaymentHoldMinutes, cfg.FrontendURL)
	listSalesUC := appSale.NewListSalesUseCase(saleRepo)
	statsSalesUC := appSale.NewSaleStatsUseCase(saleRepo)
	// Correos de compra (Resend). Sin RESEND_API_KEY se omiten con un log.
	mailer := email.NewNoopMailer()
	if cfg.ResendAPIKey != "" {
		mailer = email.NewMailer(email.NewResendSender(cfg.ResendAPIKey, cfg.EmailFrom, cfg.EmailReplyTo), email.Brand{
			SiteURL:      cfg.FrontendURL,
			LogoURL:      cfg.EmailLogoURL,
			ContactEmail: cfg.StoreContactMail,
			Location:     cfg.StoreLocation,
			Social:       cfg.StoreSocial,
		})
	} else {
		log.Warn("RESEND_API_KEY sin configurar: no se enviarán correos a los clientes")
	}
	purchaseNotifier := appSale.NewPurchaseNotifier(userRepo, mailer)
	confirmSaleUC := appSale.NewConfirmSaleUseCase(reservationRepo, saleRepo).WithNotifier(purchaseNotifier)
	pickupOrdersUC := appSale.NewPickupOrdersUseCase(saleRepo, purchaseNotifier)
	checkPaymentUC := appSale.NewCheckPaymentStatusUseCase(saleRepo, boldClient).WithNotifier(purchaseNotifier)
	processPaymentEventUC := appSale.NewProcessPaymentEventUseCase(saleRepo, boldClient).WithNotifier(purchaseNotifier)
	expireCheckoutsUC := appSale.NewExpireCheckoutsUseCase(saleRepo)
	reconcileCheckoutsUC := appSale.NewReconcileCheckoutsUseCase(saleRepo, checkPaymentUC)
	dashboardSummaryUC := appDashboard.NewSummaryUseCase(dashboardRepo)

	// Router
	secureCookie := cfg.Env == "production"
	authMiddleware := httpadapter.NewAuthMiddleware([]byte(cfg.JWTSecret))
	router := httpadapter.NewRouter(httpadapter.Handlers{
		Auth:           httpadapter.NewAuthHandler(registerUC, loginUC, secureCookie),
		Games:          httpadapter.NewGamesHandler(gamesUC, syncExpansionsUC),
		Catalog:        httpadapter.NewCatalogHandler(listCardsUC),
		Pokemon:        httpadapter.NewPokemonHandler(searchUC, priceByLanguageUC, syncExpansionsUC, importCardUC, importListingUC),
		Magic:          httpadapter.NewMagicHandler(searchUC, priceByLanguageUC, syncExpansionsUC, importCardUC, importListingUC),
		Riftbound:      httpadapter.NewRiftboundHandler(searchUC, syncExpansionsUC),
		Webhook:        httpadapter.NewWebhookHandler(processWebhookUC, cfg.ScrydexWebhookSecret),
		Payments:       httpadapter.NewBoldWebhookHandler(processPaymentEventUC, cfg.BoldSecretKey, cfg.BoldEnv != "production"),
		Listings:       httpadapter.NewListingHandler(createListingUC, listListingsUC, updateStockUC, editListingUC, deleteListingUC),
		Owners:         httpadapter.NewOwnerHandler(createOwnerUC, updateOwnerUC, listOwnersUC, deleteOwnerUC),
		AdminUsers:     httpadapter.NewAdminUserHandler(createAdminUC, listAdminsUC, updateAdminUserUC, deleteAdminUC, panelsListUC),
		Cart:           httpadapter.NewCartHandler(cartUC),
		Sales:          httpadapter.NewSaleHandler(confirmSaleUC, createCheckoutUC, listSalesUC, statsSalesUC, checkPaymentUC, pickupOrdersUC),
		Dashboard:      httpadapter.NewDashboardHandler(dashboardSummaryUC),
		Inventory:      httpadapter.NewInventoryHandler(manualListingUC, newExpansionUC),
		CardImages:     httpadapter.NewCardImageHandler(cardImageRepo),
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

	// Refresco periódico de precios: actualiza en lotes las cartas cuyo precio
	// en Scrydex lleva demasiado tiempo sin tocarse. El umbral lo fija
	// appCatalog.PriceStaleAfter, que es de 30 días.
	bgCtx, cancelBg := context.WithCancel(context.Background())
	go runPriceRefreshJob(bgCtx, priceRefresherUC, log)

	// Libera periódicamente las reservas (stock temporal de 5 min) vencidas.
	go releaseExpiredLoop(log, reservationRepo)

	// Cierra los pagos en línea que el cliente nunca llegó a completar dentro de
	// la ventana de retención, devolviendo el stock reservado.
	go expireCheckoutsLoop(log, expireCheckoutsUC)

	// Cancela los pedidos de recogida en tienda que no se recogieron dentro de
	// los 2 días hábiles y devuelve su stock al inventario.
	go expirePickupsLoop(bgCtx, log, pickupOrdersUC)

	// Pregunta a la pasarela por los pagos que pudieron cobrarse sin que el
	// backend se enterara (webhook perdido o servidor reiniciado a mitad de
	// proceso). Corre más seguido que el margen de gracia del stock.
	go reconcileCheckoutsLoop(bgCtx, log, reconcileCheckoutsUC)

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

// runPriceRefreshJob refresca precios periódicamente: cada tick busca las
// cartas más desactualizadas y las actualiza en lotes hasta agotar el backlog.
func runPriceRefreshJob(ctx context.Context, refresher *appCatalog.PriceRefresher, log *slog.Logger) {
	const (
		tick      = 24 * time.Hour
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

// reconcileCheckoutsLoop recupera los pagos aprobados en la pasarela que no
// llegaron por webhook: los pedidos se crean aunque el cliente nunca vuelva al
// carrito.
func reconcileCheckoutsLoop(ctx context.Context, log *slog.Logger, reconcile *appSale.ReconcileCheckoutsUseCase) {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		runCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		n, err := reconcile.Execute(runCtx)
		cancel()
		if err != nil {
			log.Error("reconciliar pagos con la pasarela", slog.Any("error", err))
			continue
		}
		if n > 0 {
			log.Warn("pagos recuperados por reconciliación", slog.Int("count", n))
		}
	}
}

// expireCheckoutsLoop cierra los checkouts (pagos en línea) cuya ventana de
// retención venció sin que el cliente pagara. Si el pago llega después, el
// webhook lo registra y crea el pedido marcado para revisión manual en vez de
// perder el dinero.
func expireCheckoutsLoop(log *slog.Logger, expire *appSale.ExpireCheckoutsUseCase) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		n, err := expire.Execute(ctx)
		cancel()
		if err != nil {
			log.Error("cerrar checkouts vencidos", slog.Any("error", err))
			continue
		}
		if n > 0 {
			log.Info("checkouts vencidos cerrados", slog.Int64("count", n))
		}
	}
}

// expirePickupsLoop cancela periódicamente los pedidos de recogida vencidos. El
// plazo vence a las 23:59:59, así que revisar cada 10 minutos basta.
func expirePickupsLoop(ctx context.Context, log *slog.Logger, pickups *appSale.PickupOrdersUseCase) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		runCtx, cancel := context.WithTimeout(ctx, time.Minute)
		n, err := pickups.ExpireOverdue(runCtx)
		cancel()
		if err != nil {
			log.Error("cancelar pedidos de recogida vencidos", slog.Any("error", err))
		}
		if n > 0 {
			log.Info("pedidos de recogida vencidos cancelados", slog.Int("count", n))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
