package postgres

import (
	"context"
	"errors"

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

func (r *AdminUserRepository) CreateStaff(ctx context.Context, user auth.User, createdBy int64) (auth.User, error) {
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
		`SELECT u.id, u.first_name, u.last_name, u.email, au.role,
		        COALESCE(ARRAY_AGG(aup.panel_code ORDER BY aup.panel_code) FILTER (WHERE aup.panel_code IS NOT NULL), '{}')
		 FROM admin_users au
		 JOIN users u ON u.id = au.user_id
		 LEFT JOIN admin_user_panels aup ON aup.admin_user_id = au.id
		 WHERE au.role <> 'customer'
		 GROUP BY u.id, u.first_name, u.last_name, u.email, au.role
		 ORDER BY u.id ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []auth.User
	for rows.Next() {
		var u auth.User
		if err := rows.Scan(&u.ID, &u.FirstName, &u.LastName, &u.Email, &u.Role, &u.Permissions); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (r *AdminUserRepository) UpdateRole(ctx context.Context, userID int64, role string, permissions []string) error {
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

func (r *AdminUserRepository) UpdatePanels(ctx context.Context, userID int64, panels []string, role string) error {
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

func (r *AdminUserRepository) UpdateProfile(ctx context.Context, userID int64, user auth.User, role string) error {
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

func (r *AdminUserRepository) Delete(ctx context.Context, userID int64) error {
	tag, err := r.db.Exec(ctx,
		`DELETE FROM users
		 WHERE id = $1
		   AND EXISTS (SELECT 1 FROM admin_users au
		               WHERE au.user_id = users.id
		                 AND au.role <> 'admin')`,
		userID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return auth.ErrUserNotFound
	}
	return nil
}