package http

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"trample-back/internal/domain/auth"
)

const authCtxKey ctxKey = "auth_user"
const AuthCookieName = "trample_token"

type ctxKey string

type AuthUser struct {
	ID          int64
	Email       string
	FirstName   string
	LastName    string
	Role        string
	Permissions []string
}

type authClaims struct {
	Email       string   `json:"email"`
	FirstName   string   `json:"first_name"`
	LastName    string   `json:"last_name"`
	Role        string   `json:"role"`
	Permissions []string `json:"permissions"`
	jwt.RegisteredClaims
}

type AuthMiddleware struct {
	secret []byte
}

func NewAuthMiddleware(secret []byte) *AuthMiddleware {
	return &AuthMiddleware{secret: secret}
}

// RequireAuth valida el JWT desde la cookie HttpOnly o el header Authorization: Bearer <token>.
func (m *AuthMiddleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawToken := ""

		if cookie, err := r.Cookie(AuthCookieName); err == nil {
			rawToken = cookie.Value
		} else {
			header := r.Header.Get("Authorization")
			if strings.HasPrefix(header, "Bearer ") {
				rawToken = strings.TrimPrefix(header, "Bearer ")
			}
		}

		if rawToken == "" {
			Error(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		claims := &authClaims{}
		token, err := jwt.ParseWithClaims(
			rawToken,
			claims,
			func(t *jwt.Token) (any, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, errors.New("unexpected signing method")
				}
				return m.secret, nil
			},
			jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
			jwt.WithIssuer("trample-api"),
			jwt.WithAudience("trample-web"),
			jwt.WithExpirationRequired(),
		)
		if err != nil || !token.Valid {
			Error(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		sub, err := claims.GetSubject()
		if err != nil {
			Error(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		id, err := strconv.ParseInt(sub, 10, 64)
		if err != nil {
			Error(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		perms := claims.Permissions
		if perms == nil {
			perms = []string{}
		}
		ctx := context.WithValue(r.Context(), authCtxKey, AuthUser{
			ID:          id,
			Email:       claims.Email,
			FirstName:   claims.FirstName,
			LastName:    claims.LastName,
			Role:        claims.Role,
			Permissions: perms,
		})

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireRole restringe el acceso a usuarios autenticados con alguno de los roles dados.
func (m *AuthMiddleware) RequireRole(roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := AuthFromContext(r.Context())
			if !ok {
				Error(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			for _, role := range roles {
				if user.Role == role {
					next.ServeHTTP(w, r)
					return
				}
			}
			Error(w, http.StatusForbidden, "forbidden")
		})
	}
}

// RequirePermission permite el acceso si el usuario tiene el permiso dado.
// Los superadmins siempre pasan sin importar el permiso.
func (m *AuthMiddleware) RequirePermission(perm string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := AuthFromContext(r.Context())
			if !ok {
				Error(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			if user.Role == auth.RoleSuperAdmin {
				next.ServeHTTP(w, r)
				return
			}
			for _, p := range user.Permissions {
				if p == perm {
					next.ServeHTTP(w, r)
					return
				}
			}
			Error(w, http.StatusForbidden, "forbidden")
		})
	}
}

func AuthFromContext(ctx context.Context) (AuthUser, bool) {
	user, ok := ctx.Value(authCtxKey).(AuthUser)
	return user, ok
}
