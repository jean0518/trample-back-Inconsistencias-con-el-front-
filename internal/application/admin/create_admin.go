package admin

import (
	"context"
	"errors"
	"strings"
	"trample-back/internal/domain/auth"
	"trample-back/internal/ports/out"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidName      = errors.New("nombre y apellido son requeridos")
	ErrPasswordTooShort = errors.New("la contraseña debe tener al menos 6 caracteres")
	ErrInvalidPermission = errors.New("permiso inválido")
)

var validPermissions = map[string]struct{}{
	auth.PermVentas:       {},
	auth.PermInventario:   {},
	auth.PermPropietarios: {},
	auth.PermCatalogo:     {},
}

type CreateAdminInput struct {
	FirstName   string
	LastName    string
	Email       string
	Password    string
	Permissions []string
}

type CreateAdminUseCase struct {
	users out.UserRepository
}

func NewCreateAdminUseCase(users out.UserRepository) *CreateAdminUseCase {
	return &CreateAdminUseCase{users: users}
}

func (uc *CreateAdminUseCase) Execute(ctx context.Context, in CreateAdminInput) (auth.User, error) {
	firstName := strings.TrimSpace(in.FirstName)
	lastName := strings.TrimSpace(in.LastName)
	email := strings.ToLower(strings.TrimSpace(in.Email))

	if firstName == "" || lastName == "" {
		return auth.User{}, ErrInvalidName
	}
	if email == "" {
		return auth.User{}, errors.New("email es requerido")
	}
	if len(in.Password) < 6 {
		return auth.User{}, ErrPasswordTooShort
	}
	for _, p := range in.Permissions {
		if _, ok := validPermissions[p]; !ok {
			return auth.User{}, ErrInvalidPermission
		}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return auth.User{}, err
	}

	perms := in.Permissions
	if perms == nil {
		perms = []string{}
	}

	return uc.users.Create(ctx, auth.User{
		FirstName:   firstName,
		LastName:    lastName,
		Email:       email,
		Password:    string(hash),
		Role:        auth.RoleAdmin,
		Permissions: perms,
	})
}
