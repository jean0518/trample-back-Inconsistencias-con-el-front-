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
	ErrInvalidRole      = errors.New("rol inválido: debe ser 'colaborador' o 'sup_colaborador'")
)

var validAdminRoles = map[string]struct{}{
	auth.RoleColaborador:    {},
	auth.RoleSupColaborador: {},
}

type CreateAdminInput struct {
	FirstName string
	LastName  string
	Email     string
	Password  string
	Role      string
}

type CreateAdminUseCase struct {
	users out.UserRepository
	roles out.RoleRepository
}

func NewCreateAdminUseCase(users out.UserRepository, roles out.RoleRepository) *CreateAdminUseCase {
	return &CreateAdminUseCase{users: users, roles: roles}
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
	if _, ok := validAdminRoles[in.Role]; !ok {
		return auth.User{}, ErrInvalidRole
	}

	perms, err := uc.roles.GetPermissions(ctx, in.Role)
	if err != nil {
		return auth.User{}, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return auth.User{}, err
	}

	return uc.users.Create(ctx, auth.User{
		FirstName:   firstName,
		LastName:    lastName,
		Email:       email,
		Password:    string(hash),
		Role:        in.Role,
		Permissions: perms,
	})
}
