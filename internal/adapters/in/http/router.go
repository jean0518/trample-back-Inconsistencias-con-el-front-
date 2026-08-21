package http

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httprate"
	httpSwagger "github.com/swaggo/http-swagger/v2"
	"trample-back/internal/domain/auth"
)

type Handlers struct {
	Auth           *AuthHandler
	Games          *GamesHandler
	Catalog        *CatalogHandler
	Pokemon        *PokemonHandler
	Magic          *MagicHandler
	Riftbound      *RiftboundHandler
	Listings       *ListingHandler
	AuthMiddleware *AuthMiddleware
}

func NewRouter(h Handlers) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Get("/games", h.Games.List)
	r.Get("/expansions", h.Games.ListExpansions)

	r.Get("/swagger/*", httpSwagger.WrapHandler)

	r.Route("/auth", func(r chi.Router) {
		r.Use(httprate.LimitByIP(10, time.Minute))
		r.Post("/register", h.Auth.Register)
		r.Post("/login", h.Auth.Login)
	})

	// Scrydex — consultas en vivo a la API externa (solo admin)
	r.Route("/scrydex", func(r chi.Router) {
		r.Use(h.AuthMiddleware.RequireAuth)
		r.Use(h.AuthMiddleware.RequireRole(auth.RoleAdmin))
		r.Route("/pokemon", func(r chi.Router) {
			r.Post("/cards", h.Pokemon.Search)
			r.Post("/cards/{id}", h.Pokemon.FetchOne)
		})
		r.Route("/magic", func(r chi.Router) {
			r.Post("/cards", h.Magic.Search)
			r.Post("/cards/{id}", h.Magic.FetchOne)
		})

		r.Route("/riftbound", func(r chi.Router) {
			r.Post("/cards", h.Riftbound.Search)
			r.Post("/cards/{id}", h.Riftbound.FetchOne)
		})
	})

	// Catalog — consultas a la DB local
	r.Route("/catalog", func(r chi.Router) {
		r.Get("/cards", h.Catalog.ListCards)
		r.Route("/pokemon", func(r chi.Router) {
			r.Get("/expansions", h.Pokemon.ListExpansions)
		})
		r.Route("/magic", func(r chi.Router) {
			r.Get("/expansions", h.Magic.ListExpansions)
		})
	})

	// Listings — inventario del vendedor autenticado
	r.Route("/listings", func(r chi.Router) {
		r.Use(h.AuthMiddleware.RequireAuth)
		r.Get("/", h.Listings.List)
		r.Post("/", h.Listings.Create)
		r.Delete("/{id}", h.Listings.Delete)
	})

	// Admin — sincronización e importación (solo usuarios con rol "admin")
	r.Route("/admin", func(r chi.Router) {
		r.Use(h.AuthMiddleware.RequireAuth)
		r.Use(h.AuthMiddleware.RequireRole(auth.RoleAdmin))
		r.Post("/cards/import", h.Pokemon.ImportCards)
		r.Route("/pokemon", func(r chi.Router) {
			r.Post("/expansions/sync", h.Pokemon.SyncExpansions)
			r.Put("/cards/{id}", h.Pokemon.RefreshCard)
			r.Delete("/cards/{id}", h.Pokemon.DeleteCard)
		})
		r.Route("/magic", func(r chi.Router) {
			r.Post("/expansions/sync", h.Magic.SyncExpansions)
			r.Put("/cards/{id}", h.Magic.RefreshCard)
			r.Delete("/cards/{id}", h.Magic.DeleteCard)
		})
	})

	return r
}
