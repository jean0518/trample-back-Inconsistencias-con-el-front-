package postgres

import (
	"context"
	"errors"
	"trample-back/internal/domain/auth"

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
		`INSERT INTO users (first_name, last_name, email, password_hash, role)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id`,
		user.FirstName, user.LastName, user.Email, user.Password, user.Role,
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
		`SELECT id, first_name, last_name, email, password_hash, role
		 FROM users WHERE email = $1`,
		email,
	).Scan(&u.ID, &u.FirstName, &u.LastName, &u.Email, &u.Password, &u.Role)
	return u, err
}
