package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"trample-back/internal/domain/auth"
)

type fakeUsers struct {
	users map[string]*auth.User
}

func (f *fakeUsers) Create(_ context.Context, u auth.User) (auth.User, error) {
	u.ID = "u" + u.Email
	f.users[u.ID] = &u
	return u, nil
}
func (f *fakeUsers) FindByEmail(_ context.Context, email string) (auth.User, error) {
	for _, u := range f.users {
		if u.Email == email {
			return *u, nil
		}
	}
	return auth.User{}, auth.ErrUserNotFound
}
func (f *fakeUsers) FindByID(_ context.Context, id string) (auth.User, error) {
	if u, ok := f.users[id]; ok {
		return *u, nil
	}
	return auth.User{}, auth.ErrUserNotFound
}
func (f *fakeUsers) FindByEmailVerifyToken(_ context.Context, token string) (auth.User, error) {
	for _, u := range f.users {
		if u.EmailVerifyToken == token && token != "" {
			return *u, nil
		}
	}
	return auth.User{}, auth.ErrUserNotFound
}
func (f *fakeUsers) SetEmailVerifyToken(_ context.Context, id, token string, exp time.Time) error {
	f.users[id].EmailVerifyToken, f.users[id].EmailVerifyExpiresAt = token, exp
	return nil
}
func (f *fakeUsers) MarkEmailVerified(_ context.Context, id string) error {
	u := f.users[id]
	u.EmailVerified, u.EmailVerifyToken, u.EmailVerifyExpiresAt = true, "", time.Time{}
	return nil
}

type sentMail struct{ to, name, url string }

type fakeMailer struct {
	sent []sentMail
	err  error
}

func (m *fakeMailer) SendEmailVerification(_ context.Context, to, name, url string) error {
	m.sent = append(m.sent, sentMail{to, name, url})
	return m.err
}

// newVerifier arma el caso de uso con envío síncrono y reloj controlable.
func newVerifier(t *testing.T) (*VerifyEmailUseCase, *fakeUsers, *fakeMailer, *time.Time) {
	t.Helper()
	users := &fakeUsers{users: map[string]*auth.User{}}
	mailer := &fakeMailer{}
	now := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	uc := NewVerifyEmailUseCase(users, mailer, "https://tramplestore.com/")
	uc.now = func() time.Time { return now }
	uc.async = func(f func()) { f() }
	return uc, users, mailer, &now
}

func TestRegistroEnviaCorreoDeVerificacion(t *testing.T) {
	uc, users, mailer, _ := newVerifier(t)
	reg := NewRegisterUseCase(users, uc)

	user, err := reg.Execute(context.Background(), RegisterInput{FirstName: "Ana", LastName: "P", Email: "ana@x.co", Password: "secreto1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(mailer.sent) != 1 || mailer.sent[0].to != "ana@x.co" || mailer.sent[0].name != "Ana" {
		t.Fatalf("correo inesperado: %+v", mailer.sent)
	}
	token := users.users[user.ID].EmailVerifyToken
	if token == "" || mailer.sent[0].url != "https://tramplestore.com/verificar-email?token="+token {
		t.Errorf("enlace inesperado: %q (token %q)", mailer.sent[0].url, token)
	}
}

func TestRegistroNoFallaSiElCorreoFalla(t *testing.T) {
	uc, users, mailer, _ := newVerifier(t)
	mailer.err = errors.New("resend caído")
	reg := NewRegisterUseCase(users, uc)
	if _, err := reg.Execute(context.Background(), RegisterInput{FirstName: "Ana", LastName: "P", Email: "ana@x.co", Password: "secreto1"}); err != nil {
		t.Fatalf("el registro no debe depender del correo: %v", err)
	}
}

func TestVerifyConfirmaElCorreo(t *testing.T) {
	uc, users, _, _ := newVerifier(t)
	u, _ := users.Create(context.Background(), auth.User{Email: "ana@x.co"})
	uc.SendAsync(u)
	token := users.users[u.ID].EmailVerifyToken

	if err := uc.Verify(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	if !users.users[u.ID].EmailVerified {
		t.Error("el usuario debe quedar verificado")
	}
	// El token se gasta: reutilizarlo es inválido.
	if err := uc.Verify(context.Background(), token); !errors.Is(err, auth.ErrInvalidVerifyToken) {
		t.Errorf("token reutilizado: err = %v", err)
	}
}

func TestVerifyRechazaTokenVacioDesconocidoOExpirado(t *testing.T) {
	uc, users, _, now := newVerifier(t)
	u, _ := users.Create(context.Background(), auth.User{Email: "ana@x.co"})
	uc.SendAsync(u)
	token := users.users[u.ID].EmailVerifyToken

	for _, bad := range []string{"", "  ", "no-existe"} {
		if err := uc.Verify(context.Background(), bad); !errors.Is(err, auth.ErrInvalidVerifyToken) {
			t.Errorf("Verify(%q) = %v", bad, err)
		}
	}
	*now = now.Add(auth.VerifyTokenTTL + time.Second)
	if err := uc.Verify(context.Background(), token); !errors.Is(err, auth.ErrInvalidVerifyToken) {
		t.Errorf("token expirado: err = %v", err)
	}
	if users.users[u.ID].EmailVerified {
		t.Error("un token expirado no debe verificar")
	}
}

func TestResend(t *testing.T) {
	uc, users, mailer, now := newVerifier(t)
	u, _ := users.Create(context.Background(), auth.User{Email: "ana@x.co", FirstName: "Ana"})

	// Sin token previo: envía.
	if err := uc.Resend(context.Background(), u.ID); err != nil || len(mailer.sent) != 1 {
		t.Fatalf("primer reenvío: err=%v enviados=%d", err, len(mailer.sent))
	}
	// Enseguida: frenado.
	if err := uc.Resend(context.Background(), u.ID); !errors.Is(err, ErrVerifyTooSoon) || len(mailer.sent) != 1 {
		t.Fatalf("reenvío inmediato: err=%v enviados=%d", err, len(mailer.sent))
	}
	// Pasado el minuto: envía con token nuevo.
	old := users.users[u.ID].EmailVerifyToken
	*now = now.Add(61 * time.Second)
	if err := uc.Resend(context.Background(), u.ID); err != nil || len(mailer.sent) != 2 {
		t.Fatalf("reenvío tras la espera: err=%v enviados=%d", err, len(mailer.sent))
	}
	if users.users[u.ID].EmailVerifyToken == old {
		t.Error("el reenvío debe generar un token nuevo")
	}
	// Ya verificado: no envía ni falla.
	users.users[u.ID].EmailVerified = true
	if err := uc.Resend(context.Background(), u.ID); err != nil || len(mailer.sent) != 2 {
		t.Fatalf("usuario verificado: err=%v enviados=%d", err, len(mailer.sent))
	}
}
