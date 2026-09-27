package postgres

import (
	"context"
	"errors"
	"fmt"
	"trample-back/internal/domain/catalog"

	"github.com/jackc/pgx/v5"
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

func (r *ExpansionRepository) ListExpansions(ctx context.Context, gameCode string) ([]catalog.Expansion, error) {
	rows, err := r.db.Query(ctx, `
		SELECT e.id, e.game_id, e.external_id, e.name, e.code, e.series, e.total, e.release_date, e.logo_url, e.symbol_url
		FROM expansions e
		JOIN games g ON g.id = e.game_id
		WHERE g.code = $1
		ORDER BY e.release_date DESC
	`, gameCode)
	if err != nil {
		return nil, fmt.Errorf("listar expansiones: %w", err)
	}
	defer rows.Close()
	return scanExpansions(rows)
}

func (r *ExpansionRepository) ListByGameID(ctx context.Context, gameID int64) ([]catalog.Expansion, error) {
	rows, err := r.db.Query(ctx, `
		SELECT e.id, e.game_id, e.external_id, e.name, e.code, e.series, e.total, e.release_date, e.logo_url, e.symbol_url
		FROM expansions e
		WHERE e.game_id = $1
		ORDER BY e.release_date DESC
	`, gameID)
	if err != nil {
		return nil, fmt.Errorf("listar expansiones por juego: %w", err)
	}
	defer rows.Close()
	return scanExpansions(rows)
}

func (r *ExpansionRepository) ListAll(ctx context.Context) ([]catalog.Expansion, error) {
	rows, err := r.db.Query(ctx, `
		SELECT e.id, e.game_id, e.external_id, e.name, e.code, e.series, e.total, e.release_date, e.logo_url, e.symbol_url
		FROM expansions e
		ORDER BY e.release_date DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("listar expansiones: %w", err)
	}
	defer rows.Close()
	return scanExpansions(rows)
}

type expansionRow interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanExpansions(rows expansionRow) ([]catalog.Expansion, error) {
	var result []catalog.Expansion
	for rows.Next() {
		var e catalog.Expansion
		if err := rows.Scan(&e.ID, &e.GameID, &e.ExternalID, &e.Name, &e.Code, &e.Series, &e.Total, &e.ReleaseDate, &e.Logo, &e.Symbol); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

// CreateManual registra una expansión que el staff crea desde el panel, sin que
// exista en Scrydex. Devuelve la expansión persistida y un booleano que indica
// si realmente se insertó: si el external_id derivado del nombre ya estaba, se
// devuelve la fila existente con created=false, que es lo que permite que el
// botón "+" del formulario se pueda pulsar dos veces sin duplicar el set.
//
// Cuando la expansión ya existe no se sobreescriben sus datos: puede haberse
// sincronizado desde Scrydex con información más completa, y pisar eso con lo
// que se escribió a mano sería una regresión.
func (r *ExpansionRepository) CreateManual(ctx context.Context, gameCode string, e catalog.Expansion) (catalog.Expansion, bool, error) {
	var gameID int64
	if err := r.db.QueryRow(ctx, `SELECT id FROM games WHERE code = $1`, gameCode).Scan(&gameID); err != nil {
		return catalog.Expansion{}, false, fmt.Errorf("juego %q no encontrado en DB: %w", gameCode, err)
	}

	// Si ya existe, se devuelve tal cual sin tocar sus campos.
	var existing catalog.Expansion
	const find = `
		SELECT id, game_id, external_id, name, code, series, total, release_date, logo_url, symbol_url
		FROM expansions WHERE game_id = $1 AND external_id = $2`
	err := r.db.QueryRow(ctx, find, gameID, e.ExternalID).Scan(
		&existing.ID, &existing.GameID, &existing.ExternalID, &existing.Name,
		&existing.Code, &existing.Series, &existing.Total, &existing.ReleaseDate,
		&existing.Logo, &existing.Symbol)
	if err == nil {
		return existing, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return catalog.Expansion{}, false, fmt.Errorf("no se pudo consultar la expansión: %w", err)
	}

	const insert = `
		INSERT INTO expansions (game_id, external_id, name, code, series, total, release_date, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now())
		RETURNING id`
	var id int64
	if err := r.db.QueryRow(ctx, insert, gameID, e.ExternalID, e.Name, e.Code, e.Series, e.Total, e.ReleaseDate).Scan(&id); err != nil {
		return catalog.Expansion{}, false, fmt.Errorf("no se pudo crear la expansión: %w", err)
	}

	e.ID = id
	e.GameID = gameID
	return e, true, nil
}
