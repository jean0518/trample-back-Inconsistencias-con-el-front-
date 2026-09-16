package out

import (
	"context"
	"trample-back/internal/domain/owner"
)

type OwnerRepository interface {
	Create(ctx context.Context, input owner.CreateInput) (owner.Owner, error)
	Update(ctx context.Context, id int64, input owner.UpdateInput) (owner.Owner, error)
	ListAll(ctx context.Context) ([]owner.Owner, error)
	Delete(ctx context.Context, id int64) error
}
