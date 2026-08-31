package owner

import (
	"context"
	"fmt"
	"strings"

	"trample-back/internal/domain/owner"
	"trample-back/internal/ports/out"
)

type CreateOwnerUseCase struct {
	repo out.OwnerRepository
}

func NewCreateOwnerUseCase(repo out.OwnerRepository) *CreateOwnerUseCase {
	return &CreateOwnerUseCase{repo: repo}
}

func (uc *CreateOwnerUseCase) Execute(ctx context.Context, input owner.CreateInput) (owner.Owner, error) {
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
	return uc.repo.Create(ctx, input)
}
