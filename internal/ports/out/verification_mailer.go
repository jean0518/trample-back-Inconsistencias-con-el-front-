package out

import "context"

// VerificationMailer envía el correo para confirmar la dirección de un usuario.
type VerificationMailer interface {
	// SendEmailVerification manda el enlace (verifyURL) con el que el cliente
	// confirma que el correo es suyo.
	SendEmailVerification(ctx context.Context, to, name, verifyURL string) error
}
