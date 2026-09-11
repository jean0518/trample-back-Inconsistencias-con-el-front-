package http

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httprate"
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
	Owners         *OwnerHandler
	Cart           *CartHandler
	Sales          *SaleHandler
	AdminUsers     *AdminUserHandler
	AuthMiddleware *AuthMiddleware
	AllowedOrigins []string
}

func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "0")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		next.ServeHTTP(w, r)
	})
}

func corsMiddleware(allowedOrigins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		allowed[o] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if _, ok := allowed[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func NewRouter(h Handlers) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(securityHeadersMiddleware)
	r.Use(corsMiddleware(h.AllowedOrigins))
	r.Use(httprate.LimitByIP(100, time.Minute))

	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Get("/games", h.Games.List)
	r.Get("/expansions", h.Games.ListExpansions)

	r.Route("/auth", func(r chi.Router) {
		r.Use(httprate.LimitByIP(10, time.Minute))
		r.Post("/register", h.Auth.Register)
		r.Post("/login", h.Auth.Login)
		r.Post("/logout", h.Auth.Logout)
		r.With(h.AuthMiddleware.RequireAuth).Get("/me", h.Auth.Me)
	})

	// Scrydex — consultas en vivo a la API externa (admin y superadmin)
	r.Route("/scrydex", func(r chi.Router) {
		r.Use(h.AuthMiddleware.RequireAuth)
		r.Use(h.AuthMiddleware.RequireRole(auth.RoleAdmin, auth.RoleSuperAdmin))
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
		r.Get("/cards/{id}", h.Catalog.GetCard)
		r.Route("/pokemon", func(r chi.Router) {
			r.Get("/expansions", h.Pokemon.ListExpansions)
		})
		r.Route("/magic", func(r chi.Router) {
			r.Get("/expansions", h.Magic.ListExpansions)
		})
		r.Route("/riftbound", func(r chi.Router) {
			r.Get("/expansions", h.Riftbound.ListExpansions)
		})
	})

	// Listings — inventario del vendedor autenticado
	r.Route("/listings", func(r chi.Router) {
		r.Use(h.AuthMiddleware.RequireAuth)
		r.Get("/", h.Listings.List)
		r.Post("/", h.Listings.Create)
		r.Patch("/{id}", h.Listings.UpdateStock)
		r.Delete("/{id}", h.Listings.Delete)
	})

	// Cart — stock temporal de 5 minutos al agregar al carrito
	r.Route("/cart", func(r chi.Router) {
		r.Use(h.AuthMiddleware.RequireAuth)
		r.Get("/", h.Cart.GetCart)
		r.Post("/", h.Cart.AddToCart)
		r.Delete("/{id}", h.Cart.RemoveFromCart)
	})

	// Sales — registro local de ventas/pedidos e historial
	r.Route("/sales", func(r chi.Router) {
		r.Use(h.AuthMiddleware.RequireAuth)
		r.Post("/", h.Sales.ConfirmSale)
		r.Get("/", h.Sales.ListSales)
	})

	// Admin — sincronización e importación (admin y superadmin)
	r.Route("/admin", func(r chi.Router) {
		r.Use(h.AuthMiddleware.RequireAuth)
		r.Use(h.AuthMiddleware.RequireRole(auth.RoleAdmin, auth.RoleSuperAdmin))

		r.With(h.AuthMiddleware.RequirePermission(auth.PermCatalogo)).
			Post("/cards/import-listing", h.Pokemon.ImportToListing)

		r.Route("/owners", func(r chi.Router) {
			r.Use(h.AuthMiddleware.RequirePermission(auth.PermPropietarios))
			r.Get("/", h.Owners.List)
			r.Post("/", h.Owners.Create)
			r.Delete("/{id}", h.Owners.Delete)
		})

		r.With(h.AuthMiddleware.RequirePermission(auth.PermVentas)).
			Get("/reservation-logs", h.Cart.ListReservationLogs)
		r.With(h.AuthMiddleware.RequirePermission(auth.PermVentas)).
			Get("/sales-stats", h.Sales.SalesStats)

		r.Route("/pokemon", func(r chi.Router) {
			r.Use(h.AuthMiddleware.RequirePermission(auth.PermCatalogo))
			r.Post("/expansions/sync", h.Pokemon.SyncExpansions)
			r.Put("/cards/{id}", h.Pokemon.RefreshCard)
			r.Delete("/cards/{id}", h.Pokemon.DeleteCard)
		})
		r.Route("/magic", func(r chi.Router) {
			r.Use(h.AuthMiddleware.RequirePermission(auth.PermCatalogo))
			r.Post("/expansions/sync", h.Magic.SyncExpansions)
			r.Post("/cards/import-listing", h.Magic.ImportToListing)
			r.Put("/cards/{id}", h.Magic.RefreshCard)
			r.Delete("/cards/{id}", h.Magic.DeleteCard)
		})
		r.Route("/riftbound", func(r chi.Router) {
			r.Use(h.AuthMiddleware.RequirePermission(auth.PermCatalogo))
			r.Post("/expansions/sync", h.Riftbound.SyncExpansions)
		})
	})

	// Superadmin — gestión de usuarios admin y sus permisos
	r.Route("/superadmin", func(r chi.Router) {
		r.Use(h.AuthMiddleware.RequireAuth)
		r.Use(h.AuthMiddleware.RequireRole(auth.RoleSuperAdmin))
		r.Route("/users", func(r chi.Router) {
			r.Get("/", h.AdminUsers.List)
			r.Post("/", h.AdminUsers.Create)
			r.Patch("/{id}/permissions", h.AdminUsers.UpdatePermissions)
			r.Delete("/{id}", h.AdminUsers.Delete)
		})
	})

	return r
}
