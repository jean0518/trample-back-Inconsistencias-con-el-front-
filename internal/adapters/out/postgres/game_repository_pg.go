package postgres

import (
	"context"
	"fmt"
	"trample-back/internal/domain/catalog"

	"github.com/jackc/pgx/v5/pgxpool"
)

type GameRepository struct {
	db *pgxpool.Pool
}

func NewGameRepository(db *pgxpool.Pool) *GameRepository {
	return &GameRepository{db: db}
}

func (r *GameRepository) ListGames(ctx context.Context) ([]catalog.Game, error) {
	rows, err := r.db.Query(ctx, `SELECT id, code, name FROM games ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("listar juegos: %w", err)
	}
	defer rows.Close()

	var games []catalog.Game
	for rows.Next() {
		var g catalog.Game
		if err := rows.Scan(&g.ID, &g.Code, &g.Name); err != nil {
			return nil, err
		}
		games = append(games, g)
	}
	return games, rows.Err()
}
