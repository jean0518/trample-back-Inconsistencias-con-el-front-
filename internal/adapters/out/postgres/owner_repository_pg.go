package postgres

import (
	"context"
	"errors"
	"fmt"
	"trample-back/internal/domain/owner"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type OwnerRepository struct {
	db *pgxpool.Pool
}

func NewOwnerRepository(db *pgxpool.Pool) *OwnerRepository {
	return &OwnerRepository{db: db}
}

func (r *OwnerRepository) Create(ctx context.Context, input owner.CreateInput) (owner.Owner, error) {
	var o owner.Owner
	err := r.db.QueryRow(ctx, `
		INSERT INTO owners (name, phone, email, is_default)
		VALUES ($1, $2, $3, $4)
		RETURNING id, name, phone, email, is_default
	`, input.Name, input.Phone, input.Email, input.IsDefault,
	).Scan(&o.ID, &o.Name, &o.Phone, &o.Email, &o.IsDefault)
	if err != nil {
		return owner.Owner{}, fmt.Errorf("crear owner: %w", err)
	}
	return o, nil
}

func (r *OwnerRepository) Update(ctx context.Context, id int64, input owner.UpdateInput) (owner.Owner, error) {
	var o owner.Owner
	err := r.db.QueryRow(ctx, `
		UPDATE owners
		SET name = $2, phone = $3, email = $4, updated_at = now()
		WHERE id = $1
		RETURNING id, name, phone, email, is_default
	`, id, input.Name, input.Phone, input.Email,
	).Scan(&o.ID, &o.Name, &o.Phone, &o.Email, &o.IsDefault)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return owner.Owner{}, owner.ErrNotFound
		}
		return owner.Owner{}, fmt.Errorf("actualizar owner %d: %w", id, err)
	}
	return o, nil
}

func (r *OwnerRepository) ListAll(ctx context.Context) ([]owner.Owner, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, name, phone, email, is_default
		FROM owners
		ORDER BY is_default DESC, name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("listar owners: %w", err)
	}
	defer rows.Close()

	var owners []owner.Owner
	for rows.Next() {
		var o owner.Owner
		if err := rows.Scan(&o.ID, &o.Name, &o.Phone, &o.Email, &o.IsDefault); err != nil {
			return nil, fmt.Errorf("escanear owner: %w", err)
		}
		owners = append(owners, o)
	}
	return owners, rows.Err()
}

func (r *OwnerRepository) Delete(ctx context.Context, id int64) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM owners WHERE id = $1 AND is_default = false`, id)
	if err != nil {
		return fmt.Errorf("eliminar owner: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("owner %d no encontrado o es el predeterminado: %w", id, owner.ErrNotFound)
	}
	return nil
}
