package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	httpSwagger "github.com/swaggo/http-swagger/v2"
)

type Handlers struct {
	Auth      *AuthHandler
	Pokemon   *PokemonHandler
	Magic     *MagicHandler
	Riftbound *RiftboundHandler
}

func NewRouter(h Handlers) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Get("/swagger/*", httpSwagger.WrapHandler)

	r.Route("/auth", func(r chi.Router) {
		r.Post("/register", h.Auth.Register)
		r.Post("/login", h.Auth.Login)
	})

	// Scrydex — consultas en vivo a la API externa
	r.Route("/scrydex", func(r chi.Router) {
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
		r.Route("/pokemon", func(r chi.Router) {
			r.Get("/expansions", h.Pokemon.ListExpansions)
		})
	})

	// Admin — sincronización e importación
	r.Route("/admin", func(r chi.Router) {
		r.Route("/pokemon", func(r chi.Router) {
			r.Post("/expansions/sync", h.Pokemon.SyncExpansions)
			r.Post("/cards/import", h.Pokemon.ImportCard)
		})
	})

	return r
}
