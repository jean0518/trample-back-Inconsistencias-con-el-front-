package out

import (
	"context"
	"trample-back/internal/domain/auth"
)

type UserRepository interface {
	Create(ctx context.Context, user auth.User) (auth.User, error)
	FindByEmail(ctx context.Context, email string) (auth.User, error)
	FindByID(ctx context.Context, id int64) (auth.User, error)
	ListAdmins(ctx context.Context) ([]auth.User, error)
	UpdatePermissions(ctx context.Context, id int64, permissions []string) error
	Delete(ctx context.Context, id int64) error
}
