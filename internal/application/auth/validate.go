package auth

import (
	"errors"
	"net/mail"
	"strings"
)

var (
	ErrInvalidEmail     = errors.New("email inválido")
	ErrInvalidName      = errors.New("nombre y apellido son requeridos")
	ErrPasswordTooShort = errors.New("la contraseña debe tener al menos 6 caracteres")
)

// validateEmail valida el formato del email y rechaza inputs con espacios
// o display-names (ej: "John <john@x.com>").
func validateEmail(email string) error {
	trimmed := strings.TrimSpace(email)
	if trimmed == "" {
		return ErrInvalidEmail
	}
	addr, err := mail.ParseAddress(trimmed)
	if err != nil || addr.Address != trimmed {
		return ErrInvalidEmail
	}
	return nil
}
