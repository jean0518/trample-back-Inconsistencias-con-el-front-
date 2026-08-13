package http

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

const authCtxKey ctxKey = "auth_user"

type ctxKey string

type AuthUser struct {
	ID    int64
	Email string
	Name  string
	Role  string
}

type authClaims struct {
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
	jwt.RegisteredClaims
}

type AuthMiddleware struct {
	secret []byte
}

func NewAuthMiddleware(secret []byte) *AuthMiddleware {
	return &AuthMiddleware{secret: secret}
}

// RequireAuth valida el JWT de la request (header Authorization: Bearer <token>)
// y guarda los claims en el contexto.
func (m *AuthMiddleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			Error(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		claims := &authClaims{}
		token, err := jwt.ParseWithClaims(
			strings.TrimPrefix(header, "Bearer "),
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

		ctx := context.WithValue(r.Context(), authCtxKey, AuthUser{
			ID:    id,
			Email: claims.Email,
			Name:  claims.Name,
			Role:  claims.Role,
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

func AuthFromContext(ctx context.Context) (AuthUser, bool) {
	user, ok := ctx.Value(authCtxKey).(AuthUser)
	return user, ok
}
