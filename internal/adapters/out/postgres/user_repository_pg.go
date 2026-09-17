package postgres

import (
	"context"
	"errors"
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
	         ELSE COALESCE(u.permissions, '{}') END)
	FROM users u
	LEFT JOIN admin_users au ON au.user_id = u.id
`

func (r *UserRepository) FindByEmail(ctx context.Context, email string) (auth.User, error) {
	var u auth.User
	err := r.db.QueryRow(ctx, userSelect+` WHERE u.email = $1`, email).
		Scan(&u.ID, &u.FirstName, &u.LastName, &u.Email, &u.Password, &u.Role, &u.Permissions)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.User{}, auth.ErrUserNotFound
	}
	return u, err
}