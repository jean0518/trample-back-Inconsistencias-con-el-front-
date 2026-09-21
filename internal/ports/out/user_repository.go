package out

import (
	"context"
	"trample-back/internal/domain/auth"
)

type UserRepository interface {
	Create(ctx context.Context, user auth.User) (auth.User, error)
	FindByEmail(ctx context.Context, email string) (auth.User, error)
}