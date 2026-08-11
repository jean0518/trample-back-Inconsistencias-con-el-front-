package postgres

import (
	"context"
	"trample-back/internal/domain/auth"

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
		`INSERT INTO users (first_name, last_name, email, password)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id`,
		user.FirstName, user.LastName, user.Email, user.Password,
	).Scan(&user.ID)
	return user, err
}

func (r *UserRepository) FindByEmail(ctx context.Context, email string) (auth.User, error) {
	var u auth.User
	err := r.db.QueryRow(ctx,
		`SELECT id, first_name, last_name, email, password FROM users WHERE email = $1`,
		email,
	).Scan(&u.ID, &u.FirstName, &u.LastName, &u.Email, &u.Password)
	return u, err
}
