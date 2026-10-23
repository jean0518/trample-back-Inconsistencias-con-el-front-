package postgres

import (
	"context"
	"errors"
	"fmt"

	"trample-back/internal/domain/auth"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AdminUserRepository struct {
	db *pgxpool.Pool
}

func NewAdminUserRepository(db *pgxpool.Pool) *AdminUserRepository {
	return &AdminUserRepository{db: db}
}

func (r *AdminUserRepository) CreateStaff(ctx context.Context, user auth.User, createdBy string) (auth.User, error) {
	if user.Permissions == nil {
		user.Permissions = []string{}
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return auth.User{}, err
	}
	defer tx.Rollback(ctx)

	// El usuario base ya fue creado por UserRepository.Create; aquí solo se
	// registra al staff y sus paneles de acceso.
	var adminUserID int64
	err = tx.QueryRow(ctx,
		`INSERT INTO admin_users (user_id, role, created_by)
		 VALUES ($1, $2, $3)
		 RETURNING id`,
		user.ID, user.Role, createdBy,
	).Scan(&adminUserID)
	if err != nil {
		return auth.User{}, err
	}

	for _, panel := range user.Permissions {
		if _, err := tx.Exec(ctx,
			`INSERT INTO admin_user_panels (admin_user_id, panel_code) VALUES ($1, $2)`,
			adminUserID, panel,
		); err != nil {
			return auth.User{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return auth.User{}, err
	}
	return user, nil
}

func (r *AdminUserRepository) List(ctx context.Context) ([]auth.User, error) {
	rows, err := r.db.Query(ctx,
		`SELECT u.id, u.first_name, u.last_name, u.email, au.role, au.status,
		        COALESCE(ARRAY_AGG(aup.panel_code ORDER BY aup.panel_code) FILTER (WHERE aup.panel_code IS NOT NULL), '{}')
		 FROM admin_users au
		 JOIN users u ON u.id = au.user_id
		 LEFT JOIN admin_user_panels aup ON aup.admin_user_id = au.id
		 WHERE au.role <> 'customer'
		 GROUP BY u.id, u.first_name, u.last_name, u.email, au.role, au.status
		 ORDER BY u.id ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []auth.User
	for rows.Next() {
		var u auth.User
		if err := rows.Scan(&u.ID, &u.FirstName, &u.LastName, &u.Email, &u.Role, &u.Status, &u.Permissions); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (r *AdminUserRepository) UpdateRole(ctx context.Context, userID string, role string, permissions []string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var adminUserID int64
	var currentRole string
	if err := tx.QueryRow(ctx,
		`SELECT id, role FROM admin_users WHERE user_id = $1`,
		userID,
	).Scan(&adminUserID, &currentRole); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return auth.ErrUserNotFound
		}
		return err
	}
	if currentRole == auth.RoleAdmin {
		return auth.ErrIsAdmin
	}

	if _, err := tx.Exec(ctx,
		`UPDATE admin_users SET role = $1, updated_at = now() WHERE id = $2`,
		role, adminUserID,
	); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM admin_user_panels WHERE admin_user_id = $1`,
		adminUserID,
	); err != nil {
		return err
	}

	for _, panel := range permissions {
		if _, err := tx.Exec(ctx,
			`INSERT INTO admin_user_panels (admin_user_id, panel_code) VALUES ($1, $2)`,
			adminUserID, panel,
		); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (r *AdminUserRepository) UpdatePanels(ctx context.Context, userID string, panels []string, role string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var adminUserID int64
	var currentRole string
	if err := tx.QueryRow(ctx,
		`SELECT id, role FROM admin_users WHERE user_id = $1`,
		userID,
	).Scan(&adminUserID, &currentRole); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return auth.ErrUserNotFound
		}
		return err
	}
	if currentRole == auth.RoleAdmin {
		return auth.ErrIsAdmin
	}

	if _, err := tx.Exec(ctx,
		`UPDATE admin_users SET role = $1, updated_at = now() WHERE id = $2`,
		role, adminUserID,
	); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM admin_user_panels WHERE admin_user_id = $1`,
		adminUserID,
	); err != nil {
		return err
	}

	for _, panel := range panels {
		if _, err := tx.Exec(ctx,
			`INSERT INTO admin_user_panels (admin_user_id, panel_code) VALUES ($1, $2)`,
			adminUserID, panel,
		); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (r *AdminUserRepository) UpdateProfile(ctx context.Context, userID string, user auth.User, role string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var adminUserID int64
	var currentRole string
	if err := tx.QueryRow(ctx,
		`SELECT id, role FROM admin_users WHERE user_id = $1`,
		userID,
	).Scan(&adminUserID, &currentRole); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return auth.ErrUserNotFound
		}
		return err
	}
	if currentRole == auth.RoleAdmin {
		return auth.ErrIsAdmin
	}

	if user.Password != "" {
		_, err = tx.Exec(ctx,
			`UPDATE users SET first_name = $1, last_name = $2, email = $3, password_hash = $4, updated_at = now() WHERE id = $5`,
			user.FirstName, user.LastName, user.Email, user.Password, userID,
		)
	} else {
		_, err = tx.Exec(ctx,
			`UPDATE users SET first_name = $1, last_name = $2, email = $3, updated_at = now() WHERE id = $4`,
			user.FirstName, user.LastName, user.Email, userID,
		)
	}
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return auth.ErrEmailTaken
		}
		return err
	}

	if _, err := tx.Exec(ctx,
		`UPDATE admin_users SET role = $1, updated_at = now() WHERE id = $2`,
		role, adminUserID,
	); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM admin_user_panels WHERE admin_user_id = $1`,
		adminUserID,
	); err != nil {
		return err
	}

	for _, panel := range user.Permissions {
		if _, err := tx.Exec(ctx,
			`INSERT INTO admin_user_panels (admin_user_id, panel_code) VALUES ($1, $2)`,
			adminUserID, panel,
		); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// Delete elimina definitivamente a un usuario de staff y todos sus datos
// asociados. Un administrador no se puede tocar (queda protegido por el rol).
//
// Las tablas hijas de users(id) no tienen ON DELETE CASCADE en todos los casos
// (ventas, reservas, inventario y pagos), así que el borrado se hace en una
// transacción limpiando primero esos registros. Los productos creados por el
// usuario se conservan y solo se desvinculan, para no romper el catálogo.
func (r *AdminUserRepository) Delete(ctx context.Context, userID string) error {
	return pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		var role string
		err := tx.QueryRow(ctx,
			`SELECT role FROM admin_users WHERE user_id = $1 FOR UPDATE`,
			userID,
		).Scan(&role)
		if errors.Is(err, pgx.ErrNoRows) {
			return auth.ErrUserNotFound
		}
		if err != nil {
			return err
		}
		if role == auth.RoleAdmin {
			return auth.ErrIsAdmin
		}

		if err := deleteUserDependents(ctx, tx, userID); err != nil {
			return err
		}

		tag, err := tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return auth.ErrUserNotFound
		}
		return nil
	})
}

// deleteUserDependents limpia en orden los registros que apuntan al usuario.
// El orden importa: primero los hijos de ventas (sale_product_items no tiene
// cascade), después las ventas, las reservas y, al final, el staff. Las
// reservas de cartas y productos se eliminan para que sus logs históricos no
// queden asociados a una cuenta que ya no existe.
func deleteUserDependents(ctx context.Context, tx pgx.Tx, userID string) error {
	steps := []struct {
		what  string
		query string
	}{
		{"desvincular productos", `UPDATE products SET created_by = NULL WHERE created_by = $1`},
		{"eliminar pagos en curso", `DELETE FROM payment_checkouts WHERE user_id = $1`},
		{"eliminar productos vendidos", `DELETE FROM sale_product_items WHERE sale_id IN (SELECT id FROM sales WHERE user_id = $1)`},
		{"eliminar ítems vendidos", `DELETE FROM sale_items WHERE sale_id IN (SELECT id FROM sales WHERE user_id = $1)`},
		{"eliminar ventas", `DELETE FROM sales WHERE user_id = $1`},
		{"eliminar reservas de cartas", `DELETE FROM cart_reservations WHERE user_id = $1`},
		{"eliminar reservas de productos", `DELETE FROM product_reservations WHERE user_id = $1`},
		{"eliminar historial de cartas", `DELETE FROM cart_reservation_logs WHERE user_id = $1`},
		{"eliminar historial de productos", `DELETE FROM product_reservation_logs WHERE user_id = $1`},
		{"eliminar inventario", `DELETE FROM inventory_listings WHERE seller_id = $1`},
		{"desvincular staff creado", `UPDATE admin_users SET created_by = NULL WHERE created_by = $1`},
		{"eliminar paneles del staff", `DELETE FROM admin_user_panels WHERE admin_user_id IN (SELECT id FROM admin_users WHERE user_id = $1)`},
		{"eliminar staff", `DELETE FROM admin_users WHERE user_id = $1`},
	}
	for _, step := range steps {
		if _, err := tx.Exec(ctx, step.query, userID); err != nil {
			return fmt.Errorf("%s: %w", step.what, err)
		}
	}
	return nil
}

// SetStatus cambia el estado del staff ("active" | "inactive"). Nunca afecta a
// un administrador ni a un usuario que no sea staff.
func (r *AdminUserRepository) SetStatus(ctx context.Context, userID string, status string) error {
	if status != auth.StatusActive && status != auth.StatusInactive {
		return auth.ErrUserNotFound
	}
	tag, err := r.db.Exec(ctx,
		`UPDATE admin_users
		 SET status = $2, updated_at = now()
		 WHERE user_id = $1
		   AND role <> 'admin'`,
		userID, status,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return auth.ErrUserNotFound
	}
	return nil
}
