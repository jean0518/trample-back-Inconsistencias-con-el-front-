package out

import (
	"context"
	"time"
	"trample-back/internal/domain/auth"
)

type UserRepository interface {
	Create(ctx context.Context, user auth.User) (auth.User, error)
	FindByEmail(ctx context.Context, email string) (auth.User, error)
	FindByID(ctx context.Context, id string) (auth.User, error)
	// FindByEmailVerifyToken devuelve ErrUserNotFound si ningún usuario tiene ese token.
	FindByEmailVerifyToken(ctx context.Context, token string) (auth.User, error)
	// SetEmailVerifyToken guarda un token nuevo (reemplaza el anterior).
	SetEmailVerifyToken(ctx context.Context, id, token string, expiresAt time.Time) error
	// MarkEmailVerified marca el correo como verificado y borra el token.
	MarkEmailVerified(ctx context.Context, id string) error
}