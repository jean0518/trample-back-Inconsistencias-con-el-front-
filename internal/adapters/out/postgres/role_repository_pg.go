package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type RoleRepository struct {
	db *pgxpool.Pool
}

func NewRoleRepository(db *pgxpool.Pool) *RoleRepository {
	return &RoleRepository{db: db}
}

func (r *RoleRepository) GetPermissions(ctx context.Context, roleName string) ([]string, error) {
	rows, err := r.db.Query(ctx,
		`SELECT rp.permission
		 FROM role_permissions rp
		 JOIN roles ro ON ro.id = rp.role_id
		 WHERE ro.name = $1`,
		roleName,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var perms []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		perms = append(perms, p)
	}
	if perms == nil {
		perms = []string{}
	}
	return perms, rows.Err()
}
