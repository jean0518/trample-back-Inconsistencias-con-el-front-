package auth

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	"trample-back/internal/domain/auth"
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

var ErrInvalidCredentials = errors.New("credenciales inválidas")

type LoginResult struct {
	User  auth.User
	Token string
}

func (uc *LoginUseCase) Execute(ctx context.Context, in LoginInput) (LoginResult, error) {
	email := strings.TrimSpace(in.Email)
	if err := validateEmail(email); err != nil {
		return LoginResult{}, ErrInvalidCredentials
	}

	user, err := uc.users.FindByEmail(ctx, email)
	if err != nil {
		return LoginResult{}, ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(in.Password)); err != nil {
		return LoginResult{}, ErrInvalidCredentials
	}

	signed, err := uc.IssueToken(ctx, user)
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{User: user, Token: signed}, nil
}

// IssueToken firma un JWT para un usuario ya autenticado (login o registro).
func (uc *LoginUseCase) IssueToken(_ context.Context, user auth.User) (string, error) {
	perms := user.Permissions
	if perms == nil {
		perms = []string{}
	}
	now := time.Now()
	claims := jwt.MapClaims{
		"sub":         strconv.FormatInt(user.ID, 10),
		"email":       user.Email,
		"first_name":  user.FirstName,
		"last_name":   user.LastName,
		"role":        user.Role,
		"permissions": perms,
		"iss":         "trample-api",
		"aud":         "trample-web",
		"iat":         now.Unix(),
		"exp":         now.Add(24 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(uc.jwtSecret)
	if err != nil {
		return "", err
	}
	return signed, nil
}
