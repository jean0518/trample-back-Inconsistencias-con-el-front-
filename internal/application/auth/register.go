package auth

import (
	"context"
	"errors"
	"trample-back/internal/domain/auth"
	"trample-back/internal/ports/out"

	"golang.org/x/crypto/bcrypt"
)

type RegisterUseCase struct {
	users out.UserRepository
}

func NewRegisterUseCase(users out.UserRepository) *RegisterUseCase {
	return &RegisterUseCase{users: users}
}

type RegisterInput struct {
	FirstName string
	LastName  string
	Email     string
	Password  string
}

var ErrPasswordTooShort = errors.New("password must be at least 6 characters")

func (uc *RegisterUseCase) Execute(ctx context.Context, in RegisterInput) (auth.User, error) {
	if len(in.Password) < 6 {
		return auth.User{}, ErrPasswordTooShort
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return auth.User{}, err
	}

	return uc.users.Create(ctx, auth.User{
		FirstName: in.FirstName,
		LastName:  in.LastName,
		Email:     in.Email,
		Password:  string(hash),
	})
}
