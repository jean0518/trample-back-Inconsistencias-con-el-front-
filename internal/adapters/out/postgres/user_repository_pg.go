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

func (r *UserRepository) FindByEmail(ctx context.Context, email string) (auth.User, error) {
	var u auth.User
	err := r.db.QueryRow(ctx,
		`SELECT id, first_name, last_name, email, password_hash, role, COALESCE(permissions, '{}')
		 FROM users WHERE email = $1`,
		email,
	).Scan(&u.ID, &u.FirstName, &u.LastName, &u.Email, &u.Password, &u.Role, &u.Permissions)
	return u, err
}

func (r *UserRepository) FindByID(ctx context.Context, id int64) (auth.User, error) {
	var u auth.User
	err := r.db.QueryRow(ctx,
		`SELECT id, first_name, last_name, email, password_hash, role, COALESCE(permissions, '{}')
		 FROM users WHERE id = $1`,
		id,
	).Scan(&u.ID, &u.FirstName, &u.LastName, &u.Email, &u.Password, &u.Role, &u.Permissions)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.User{}, auth.ErrUserNotFound
	}
	return u, err
}

func (r *UserRepository) ListAdmins(ctx context.Context) ([]auth.User, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, first_name, last_name, email, role, COALESCE(permissions, '{}'), created_at
		 FROM users WHERE role IN ('admin', 'superadmin')
		 ORDER BY id ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []auth.User
	for rows.Next() {
		var u auth.User
		var createdAt any
		if err := rows.Scan(&u.ID, &u.FirstName, &u.LastName, &u.Email, &u.Role, &u.Permissions, &createdAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (r *UserRepository) UpdatePermissions(ctx context.Context, id int64, permissions []string) error {
	tag, err := r.db.Exec(ctx,
		`UPDATE users SET permissions = $1, updated_at = now() WHERE id = $2 AND role = 'admin'`,
		permissions, id,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return auth.ErrUserNotFound
	}
	return nil
}

func (r *UserRepository) Delete(ctx context.Context, id int64) error {
	tag, err := r.db.Exec(ctx,
		`DELETE FROM users WHERE id = $1 AND role = 'admin'`,
		id,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return auth.ErrUserNotFound
	}
	return nil
}
