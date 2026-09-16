package postgres

import (
	"context"

	"trample-back/internal/ports/out"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PanelRepository struct {
	db *pgxpool.Pool
}

func NewPanelRepository(db *pgxpool.Pool) *PanelRepository {
	return &PanelRepository{db: db}
}

func (r *PanelRepository) GetAll(ctx context.Context) ([]out.Panel, error) {
	rows, err := r.db.Query(ctx,
		`SELECT code, name, sort_order
		 FROM admin_panels
		 ORDER BY sort_order ASC, code ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var panels []out.Panel
	for rows.Next() {
		var p out.Panel
		if err := rows.Scan(&p.Code, &p.Name, &p.SortOrder); err != nil {
			return nil, err
		}
		panels = append(panels, p)
	}
	if panels == nil {
		panels = []out.Panel{}
	}
	return panels, rows.Err()
}