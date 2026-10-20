package postgres

import (
	"context"
	"errors"
	"time"
	"trample-back/internal/domain/auth"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type UserRepository struct {
	db *pgxpool.Pool
}

func NewUserRepository(db *pgxpool.Pool) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) Create(ctx context.Context, user auth.User) (auth.User, error) {
	if user.Permissions == nil {
		user.Permissions = []string{}
	}
	err := r.db.QueryRow(ctx,
		`INSERT INTO users (first_name, last_name, email, password_hash, role, permissions)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id`,
		user.FirstName, user.LastName, user.Email, user.Password, user.Role, user.Permissions,
	).Scan(&user.ID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return auth.User{}, auth.ErrEmailTaken
		}
		return auth.User{}, err
	}
	return user, nil
}

const userSelect = `
	SELECT u.id, u.first_name, u.last_name, u.email, u.password_hash,
	       (CASE WHEN au.id IS NOT NULL THEN au.role ELSE u.role END),
	       (CASE WHEN au.id IS NOT NULL THEN
	           COALESCE((SELECT ARRAY_AGG(aup.panel_code ORDER BY aup.panel_code)
	                     FROM admin_user_panels aup WHERE aup.admin_user_id = au.id), '{}')
	         ELSE COALESCE(u.permissions, '{}') END),
	       u.email_verified,
	       COALESCE(u.email_verify_token, ''),
	       COALESCE(u.email_verify_expires_at, '0001-01-01'::timestamptz)
	FROM users u
	LEFT JOIN admin_users au ON au.user_id = u.id
`

func (r *UserRepository) findOne(ctx context.Context, where string, arg any) (auth.User, error) {
	var u auth.User
	err := r.db.QueryRow(ctx, userSelect+where, arg).Scan(
		&u.ID, &u.FirstName, &u.LastName, &u.Email, &u.Password, &u.Role, &u.Permissions,
		&u.EmailVerified, &u.EmailVerifyToken, &u.EmailVerifyExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.User{}, auth.ErrUserNotFound
	}
	return u, err
}

func (r *UserRepository) FindByEmail(ctx context.Context, email string) (auth.User, error) {
	return r.findOne(ctx, ` WHERE u.email = $1`, email)
}

func (r *UserRepository) FindByID(ctx context.Context, id string) (auth.User, error) {
	return r.findOne(ctx, ` WHERE u.id = $1`, id)
}

func (r *UserRepository) FindByEmailVerifyToken(ctx context.Context, token string) (auth.User, error) {
	return r.findOne(ctx, ` WHERE u.email_verify_token = $1`, token)
}

func (r *UserRepository) SetEmailVerifyToken(ctx context.Context, id, token string, expiresAt time.Time) error {
	_, err := r.db.Exec(ctx,
		`UPDATE users SET email_verify_token = $1, email_verify_expires_at = $2, updated_at = now() WHERE id = $3`,
		token, expiresAt, id)
	return err
}

func (r *UserRepository) MarkEmailVerified(ctx context.Context, id string) error {
	_, err := r.db.Exec(ctx,
		`UPDATE users SET email_verified = TRUE, email_verify_token = NULL, email_verify_expires_at = NULL, updated_at = now() WHERE id = $1`,
		id)
	return err
}

// ListAdminEmails devuelve los correos de los usuarios con rol admin. El rol del
// staff vive en admin_users; el de users solo cuenta si no tiene registro ahí
// (mismo criterio que userSelect).
func (r *UserRepository) ListAdminEmails(ctx context.Context) ([]string, error) {
	rows, err := r.db.Query(ctx, `
		SELECT u.email
		FROM users u
		LEFT JOIN admin_users au ON au.user_id = u.id
		WHERE (CASE WHEN au.id IS NOT NULL THEN au.role ELSE u.role END) = $1
		  AND u.email <> ''
		ORDER BY u.email
	`, auth.RoleAdmin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var emails []string
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			return nil, err
		}
		emails = append(emails, e)
	}
	return emails, rows.Err()
}
