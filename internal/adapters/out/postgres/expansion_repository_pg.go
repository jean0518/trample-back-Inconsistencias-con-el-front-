package postgres

import (
	"context"
	"fmt"
	"trample-back/internal/domain/catalog"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ExpansionRepository struct {
	db *pgxpool.Pool
}

func NewExpansionRepository(db *pgxpool.Pool) *ExpansionRepository {
	return &ExpansionRepository{db: db}
}

func (r *ExpansionRepository) SyncExpansions(ctx context.Context, gameCode string, expansions []catalog.Expansion) error {
	var gameID int64
	err := r.db.QueryRow(ctx, `SELECT id FROM games WHERE code = $1`, gameCode).Scan(&gameID)
	if err != nil {
		return fmt.Errorf("juego %q no encontrado en DB: %w", gameCode, err)
	}

	for _, e := range expansions {
		_, err := r.db.Exec(ctx, `
			INSERT INTO expansions (game_id, external_id, name, code, series, total, release_date, logo_url, symbol_url, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now())
			ON CONFLICT (game_id, external_id) DO UPDATE SET
				name         = EXCLUDED.name,
				code         = EXCLUDED.code,
				series       = EXCLUDED.series,
				total        = EXCLUDED.total,
				release_date = EXCLUDED.release_date,
				logo_url     = EXCLUDED.logo_url,
				symbol_url   = EXCLUDED.symbol_url,
				updated_at   = now()
		`, gameID, e.ExternalID, e.Name, e.Code, e.Series, e.Total, e.ReleaseDate, e.Logo, e.Symbol)
		if err != nil {
			return fmt.Errorf("upsert expansión %q: %w", e.ExternalID, err)
		}
	}
	return nil
}

func (r *ExpansionRepository) FindByName(ctx context.Context, gameCode, name string) (*catalog.Expansion, error) {
	var e catalog.Expansion
	err := r.db.QueryRow(ctx, `
		SELECT e.external_id, e.name, e.code, e.series, e.total, e.release_date, e.logo_url, e.symbol_url
		FROM expansions e
		JOIN games g ON g.id = e.game_id
		WHERE g.code = $1 AND e.name ILIKE $2
		ORDER BY e.release_date DESC
		LIMIT 1
	`, gameCode, name).Scan(&e.ExternalID, &e.Name, &e.Code, &e.Series, &e.Total, &e.ReleaseDate, &e.Logo, &e.Symbol)
	if err != nil {
		return nil, fmt.Errorf("expansión %q no encontrada en DB: %w", name, err)
	}
	return &e, nil
}

func (r *ExpansionRepository) ListExpansions(ctx context.Context, gameCode string) ([]catalog.Expansion, error) {
	rows, err := r.db.Query(ctx, `
		SELECT e.external_id, e.name, e.code, e.series, e.total, e.release_date, e.logo_url, e.symbol_url
		FROM expansions e
		JOIN games g ON g.id = e.game_id
		WHERE g.code = $1
		ORDER BY e.release_date DESC
	`, gameCode)
	if err != nil {
		return nil, fmt.Errorf("listar expansiones: %w", err)
	}
	defer rows.Close()

	var result []catalog.Expansion
	for rows.Next() {
		var e catalog.Expansion
		if err := rows.Scan(&e.ExternalID, &e.Name, &e.Code, &e.Series, &e.Total, &e.ReleaseDate, &e.Logo, &e.Symbol); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}
