package auth

import (
	"context"
	"errors"
	"time"
	"trample-back/internal/ports/out"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type LoginUseCase struct {
	users     out.UserRepository
	jwtSecret []byte
}

func NewLoginUseCase(users out.UserRepository, jwtSecret string) *LoginUseCase {
	return &LoginUseCase{users: users, jwtSecret: []byte(jwtSecret)}
}

type LoginInput struct {
	Email    string
	Password string
}

var ErrInvalidCredentials = errors.New("invalid credentials")

func (uc *LoginUseCase) Execute(ctx context.Context, in LoginInput) (string, error) {
	user, err := uc.users.FindByEmail(ctx, in.Email)
	if err != nil {
		return "", ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(in.Password)); err != nil {
		return "", ErrInvalidCredentials
	}

	claims := jwt.MapClaims{
		"sub":   user.ID,
		"email": user.Email,
		"name":  user.FirstName + " " + user.LastName,
		"exp":   time.Now().Add(24 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(uc.jwtSecret)
	if err != nil {
		return "", err
	}
	return signed, nil
}
