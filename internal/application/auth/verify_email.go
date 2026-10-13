package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"trample-back/internal/domain/auth"
	"trample-back/internal/ports/out"
)

// ErrVerifyTooSoon se devuelve al pedir otro correo de verificación demasiado
// pronto (evita que el endpoint se use para enviar spam).
var ErrVerifyTooSoon = errors.New("espera un minuto antes de pedir otro correo de verificación")

const (
	resendCooldown = time.Minute
	sendTimeout    = 20 * time.Second
)

// VerifyEmailUseCase manda y confirma la verificación del correo de un usuario.
// El envío es asíncrono y nunca bloquea ni hace fallar el registro.
type VerifyEmailUseCase struct {
	users   out.UserRepository
	mailer  out.VerificationMailer
	siteURL string
	now     func() time.Time
	// async permite a los tests esperar el envío; en producción es `go f()`.
	async func(func())
}

func NewVerifyEmailUseCase(users out.UserRepository, mailer out.VerificationMailer, siteURL string) *VerifyEmailUseCase {
	return &VerifyEmailUseCase{
		users: users, mailer: mailer, siteURL: strings.TrimRight(siteURL, "/"),
		now: time.Now, async: func(f func()) { go f() },
	}
}

// SendAsync genera un token nuevo y envía el correo en segundo plano. Los
// errores solo se registran: el usuario puede pedir otro desde su cuenta.
func (uc *VerifyEmailUseCase) SendAsync(user auth.User) {
	if uc == nil {
		return
	}
	uc.async(func() {
		ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
		defer cancel()
		if err := uc.send(ctx, user); err != nil {
			slog.Error("verificación de correo: envío fallido", slog.String("user_id", user.ID), slog.Any("error", err))
		}
	})
}

func (uc *VerifyEmailUseCase) send(ctx context.Context, user auth.User) error {
	token, err := newToken()
	if err != nil {
		return err
	}
	if err := uc.users.SetEmailVerifyToken(ctx, user.ID, token, uc.now().Add(auth.VerifyTokenTTL)); err != nil {
		return err
	}
	link := uc.siteURL + "/verificar-email?token=" + url.QueryEscape(token)
	return uc.mailer.SendEmailVerification(ctx, user.Email, user.FirstName, link)
}

// Resend reenvía el correo al usuario autenticado. Si ya está verificado no hace
// nada; si pidió otro hace menos de un minuto devuelve ErrVerifyTooSoon.
func (uc *VerifyEmailUseCase) Resend(ctx context.Context, userID string) error {
	user, err := uc.users.FindByID(ctx, userID)
	if err != nil {
		return err
	}
	if user.EmailVerified {
		return nil
	}
	// El token vigente se emitió TTL antes de su vencimiento.
	if !user.EmailVerifyExpiresAt.IsZero() &&
		uc.now().Sub(user.EmailVerifyExpiresAt.Add(-auth.VerifyTokenTTL)) < resendCooldown {
		return ErrVerifyTooSoon
	}
	uc.SendAsync(user)
	return nil
}

// Verify confirma el correo con el token del enlace. Repetir el enlace de un
// correo ya verificado no es un error: el token se borra al usarse, así que ese
// caso lo cubre el frontend consultando /auth/me.
func (uc *VerifyEmailUseCase) Verify(ctx context.Context, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return auth.ErrInvalidVerifyToken
	}
	user, err := uc.users.FindByEmailVerifyToken(ctx, token)
	if err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			return auth.ErrInvalidVerifyToken
		}
		return err
	}
	if uc.now().After(user.EmailVerifyExpiresAt) {
		return auth.ErrInvalidVerifyToken
	}
	return uc.users.MarkEmailVerified(ctx, user.ID)
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
