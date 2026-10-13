package auth

import (
	"context"
	"strings"
	"trample-back/internal/domain/auth"
	"trample-back/internal/ports/out"

	"golang.org/x/crypto/bcrypt"
)

type RegisterUseCase struct {
	users  out.UserRepository
	verify *VerifyEmailUseCase
}

// NewRegisterUseCase arma el registro. `verify` puede ser nil (no se envía correo).
func NewRegisterUseCase(users out.UserRepository, verify *VerifyEmailUseCase) *RegisterUseCase {
	return &RegisterUseCase{users: users, verify: verify}
}

type RegisterInput struct {
	FirstName string
	LastName  string
	Email     string
	Password  string
}

func (uc *RegisterUseCase) Execute(ctx context.Context, in RegisterInput) (auth.User, error) {
	firstName := strings.TrimSpace(in.FirstName)
	lastName := strings.TrimSpace(in.LastName)
	email := strings.TrimSpace(in.Email)

	if firstName == "" || lastName == "" {
		return auth.User{}, ErrInvalidName
	}
	if err := validateEmail(email); err != nil {
		return auth.User{}, err
	}
	if len(in.Password) < 6 {
		return auth.User{}, ErrPasswordTooShort
	}

	if existing, err := uc.users.FindByEmail(ctx, email); err == nil && existing.ID != "" {
		return auth.User{}, auth.ErrEmailTaken
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return auth.User{}, err
	}

	user, err := uc.users.Create(ctx, auth.User{
		FirstName: firstName,
		LastName:  lastName,
		Email:     email,
		Password:  string(hash),
		Role:      auth.RoleCustomer,
	})
	if err != nil {
		return auth.User{}, err
	}
	// El correo de verificación sale en segundo plano: si falla, el usuario lo
	// pide de nuevo desde su cuenta; el registro no se cae por eso.
	uc.verify.SendAsync(user)
	return user, nil
}
