package owner

import (
	"context"
	"fmt"
	"strings"

	"trample-back/internal/domain/owner"
	"trample-back/internal/ports/out"
)

type UpdateOwnerUseCase struct {
	repo out.OwnerRepository
}

func NewUpdateOwnerUseCase(repo out.OwnerRepository) *UpdateOwnerUseCase {
	return &UpdateOwnerUseCase{repo: repo}
}

func (uc *UpdateOwnerUseCase) Execute(ctx context.Context, id int64, input owner.UpdateInput) (owner.Owner, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return owner.Owner{}, fmt.Errorf("el nombre del propietario es requerido")
	}
	phone := strings.TrimSpace(input.Phone)
	email := strings.TrimSpace(input.Email)
	if email != "" && !strings.Contains(email, "@") {
		return owner.Owner{}, fmt.Errorf("el correo electrónico es inválido")
	}
	input.Name = name
	input.Phone = phone
	input.Email = email
	return uc.repo.Update(ctx, id, input)
}