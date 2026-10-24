package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"trample-back/internal/domain/catalog"
)

type GameRepository struct {
	db *pgxpool.Pool
}

func NewGameRepository(db *pgxpool.Pool) *GameRepository {
	return &GameRepository{db: db}
}

const gameColumns = `id, code, name, COALESCE(short_name,''), COALESCE(accent,''), COALESCE(tagline,''),
	COALESCE(icon_url,''), COALESCE(logo_url,''), icon_image_id, logo_image_id, COALESCE(has_cards,true), COALESCE(active,true)`

func scanGame(row pgx.Row) (catalog.Game, error) {
	var g catalog.Game
	err := row.Scan(&g.ID, &g.Code, &g.Name, &g.ShortName, &g.Accent, &g.Tagline, &g.IconURL, &g.LogoURL, &g.IconImageID, &g.LogoImageID, &g.HasCards, &g.Active)
	return g, err
}

func (r *GameRepository) ListGames(ctx context.Context) ([]catalog.Game, error) {
	rows, err := r.db.Query(ctx, `SELECT `+gameColumns+` FROM games WHERE COALESCE(active,true)=true ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("listar juegos: %w", err)
	}
	defer rows.Close()

	var games []catalog.Game
	for rows.Next() {
		g, err := scanGame(rows)
		if err != nil {
			return nil, err
		}
		games = append(games, g)
	}
	return games, rows.Err()
}

const adminGameColumns = `g.id, g.code, g.name, COALESCE(g.short_name,''), COALESCE(g.accent,''), COALESCE(g.tagline,''),
	COALESCE(g.icon_url,''), COALESCE(g.logo_url,''), g.icon_image_id, g.logo_image_id, COALESCE(g.has_cards,true), COALESCE(g.active,true),
	(SELECT count(*) FROM products p
	  WHERE LOWER(TRIM(p.game)) = LOWER(TRIM(g.code))
	     OR (LOWER(TRIM(g.code)) = 'mtg' AND LOWER(TRIM(p.game)) = 'magic'))::bigint,
	(SELECT count(DISTINCT c.id)
	   FROM cards c
	   JOIN card_variants cv ON cv.card_id = c.id
	   JOIN inventory_listings l ON l.variant_id = cv.id
	  WHERE c.game_id = g.id AND l.status = 'active' AND l.quantity > 0)::bigint,
	(SELECT count(*) FROM cards c WHERE c.game_id = g.id)::bigint,
	(SELECT count(*) FROM expansions e WHERE e.game_id = g.id)::bigint`

func scanAdminGame(row pgx.Row) (catalog.Game, error) {
	var g catalog.Game
	err := row.Scan(&g.ID, &g.Code, &g.Name, &g.ShortName, &g.Accent, &g.Tagline, &g.IconURL, &g.LogoURL,
		&g.IconImageID, &g.LogoImageID, &g.HasCards, &g.Active, &g.ProductsCount, &g.CardsCount, &g.CatalogCardsCount, &g.ExpansionsCount)
	return g, err
}

func (r *GameRepository) ListGamesAdmin(ctx context.Context) ([]catalog.Game, error) {
	rows, err := r.db.Query(ctx, `SELECT `+adminGameColumns+` FROM games g ORDER BY g.id`)
	if err != nil {
		return nil, fmt.Errorf("listar juegos admin: %w", err)
	}
	defer rows.Close()

	var games []catalog.Game
	for rows.Next() {
		g, err := scanAdminGame(rows)
		if err != nil {
			return nil, err
		}
		games = append(games, g)
	}
	return games, rows.Err()
}

// DeleteWithProducts borra el juego y sus productos sellados en una transacción.
// Las fotos de producto caen por ON DELETE CASCADE y las líneas de venta quedan
// con product_id NULL (histórico intacto). Se cierran antes los pagos abiertos
// que sostenían reservas de esos productos.
func (r *GameRepository) DeleteWithProducts(ctx context.Context, id int64) (int64, error) {
	var deletedProducts int64
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		var code string
		if err := tx.QueryRow(ctx, `SELECT code FROM games WHERE id = $1 FOR UPDATE`, id).Scan(&code); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return catalog.ErrGameNotFound
			}
			return fmt.Errorf("bloquear juego: %w", err)
		}

		var hasCards, hasExpansions bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM cards WHERE game_id = $1),
			       EXISTS (SELECT 1 FROM expansions WHERE game_id = $1)`, id).Scan(&hasCards, &hasExpansions); err != nil {
			return fmt.Errorf("revisar cartas del juego: %w", err)
		}
		if hasCards || hasExpansions {
			return catalog.ErrGameHasCards
		}

		games := productGameFilterValues(code)

		if _, err := tx.Exec(ctx, `
			UPDATE payment_checkouts SET status = 'expired'
			 WHERE status = 'open'
			   AND product_reservation_ids && COALESCE((
			         SELECT array_agg(pr.id)
			           FROM product_reservations pr
			           JOIN products p ON p.id = pr.product_id
			          WHERE LOWER(TRIM(p.game)) = ANY($1)
			            AND pr.status = 'active'
			       ), '{}'::bigint[])`, games); err != nil {
			return fmt.Errorf("cerrar pagos de los productos del juego: %w", err)
		}

		if _, err := tx.Exec(ctx, `
			DELETE FROM product_reservations
			 WHERE product_id IN (SELECT id FROM products WHERE LOWER(TRIM(game)) = ANY($1))`, games); err != nil {
			return fmt.Errorf("borrar reservas de los productos del juego: %w", err)
		}

		tag, err := tx.Exec(ctx, `DELETE FROM products WHERE LOWER(TRIM(game)) = ANY($1)`, games)
		if err != nil {
			return fmt.Errorf("borrar productos del juego: %w", err)
		}
		deletedProducts = tag.RowsAffected()

		if _, err := tx.Exec(ctx, `DELETE FROM games WHERE id = $1`, id); err != nil {
			return fmt.Errorf("borrar juego: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return deletedProducts, nil
}

func (r *GameRepository) GetByID(ctx context.Context, id int64) (catalog.Game, error) {
	g, err := scanGame(r.db.QueryRow(ctx, `SELECT `+gameColumns+` FROM games WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return catalog.Game{}, fmt.Errorf("juego no encontrado")
	}
	return g, err
}

func (r *GameRepository) GetByCode(ctx context.Context, code string) (catalog.Game, error) {
	g, err := scanGame(r.db.QueryRow(ctx, `SELECT `+gameColumns+` FROM games WHERE LOWER(code)=LOWER($1)`, code))
	if errors.Is(err, pgx.ErrNoRows) {
		return catalog.Game{}, fmt.Errorf("juego no encontrado")
	}
	return g, err
}

func (r *GameRepository) Create(ctx context.Context, in catalog.GameInput) (catalog.Game, error) {
	code := strings.ToUpper(strings.TrimSpace(in.Code))
	name := strings.TrimSpace(in.Name)
	if code == "" || name == "" {
		return catalog.Game{}, fmt.Errorf("código y nombre obligatorios")
	}
	shortName := strings.TrimSpace(in.ShortName)
	if shortName == "" {
		shortName = strings.Fields(name)[0]
	}
	accent := strings.TrimSpace(in.Accent)
	if accent == "" {
		accent = "#888888"
	}
	hasCards := true
	if in.HasCards != nil {
		hasCards = *in.HasCards
	}
	var id int64
	err := r.db.QueryRow(ctx, `
		INSERT INTO games (code, name, short_name, accent, tagline, icon_url, logo_url, has_cards, active)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,true)
		RETURNING id`,
		code, name, shortName, accent, strings.TrimSpace(in.Tagline), strings.TrimSpace(in.IconURL), strings.TrimSpace(in.LogoURL), hasCards,
	).Scan(&id)
	if err != nil {
		return catalog.Game{}, fmt.Errorf("crear juego: %w", err)
	}
	return r.GetByID(ctx, id)
}

func (r *GameRepository) Update(ctx context.Context, id int64, in catalog.GameInput) (catalog.Game, error) {
	sets := []string{"updated_at = now()"}
	args := []any{id}
	idx := 2

	if strings.TrimSpace(in.Name) != "" {
		sets = append(sets, fmt.Sprintf("name = $%d", idx))
		args = append(args, strings.TrimSpace(in.Name))
		idx++
	}
	if strings.TrimSpace(in.ShortName) != "" {
		sets = append(sets, fmt.Sprintf("short_name = $%d", idx))
		args = append(args, strings.TrimSpace(in.ShortName))
		idx++
	}
	if strings.TrimSpace(in.Accent) != "" {
		sets = append(sets, fmt.Sprintf("accent = $%d", idx))
		args = append(args, strings.TrimSpace(in.Accent))
		idx++
	}
	if strings.TrimSpace(in.Tagline) != "" || in.Tagline == "" {
		sets = append(sets, fmt.Sprintf("tagline = $%d", idx))
		args = append(args, strings.TrimSpace(in.Tagline))
		idx++
	}
	if strings.TrimSpace(in.IconURL) != "" {
		sets = append(sets, fmt.Sprintf("icon_url = $%d", idx))
		args = append(args, strings.TrimSpace(in.IconURL))
		idx++
	}
	if strings.TrimSpace(in.LogoURL) != "" {
		sets = append(sets, fmt.Sprintf("logo_url = $%d", idx))
		args = append(args, strings.TrimSpace(in.LogoURL))
		idx++
	}
	if in.HasCards != nil {
		sets = append(sets, fmt.Sprintf("has_cards = $%d", idx))
		args = append(args, *in.HasCards)
		idx++
	}
	if in.Active != nil {
		sets = append(sets, fmt.Sprintf("active = $%d", idx))
		args = append(args, *in.Active)
		idx++
	}

	tag, err := r.db.Exec(ctx, `UPDATE games SET `+strings.Join(sets, ", ")+` WHERE id = $1`, args...)
	if err != nil {
		return catalog.Game{}, fmt.Errorf("actualizar juego: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return catalog.Game{}, fmt.Errorf("juego no encontrado")
	}
	return r.GetByID(ctx, id)
}

func (r *GameRepository) SetActive(ctx context.Context, id int64, active bool) (catalog.Game, error) {
	tag, err := r.db.Exec(ctx, `UPDATE games SET active = $2, updated_at = now() WHERE id = $1`, id, active)
	if err != nil {
		return catalog.Game{}, err
	}
	if tag.RowsAffected() == 0 {
		return catalog.Game{}, fmt.Errorf("juego no encontrado")
	}
	return r.GetByID(ctx, id)
}

func (r *GameRepository) AttachIconImage(ctx context.Context, id int64, url string, imageID *int64) error {
	_, err := r.db.Exec(ctx, `UPDATE games SET icon_url = $2, icon_image_id = $3, updated_at = now() WHERE id = $1`, id, url, imageID)
	return err
}

func (r *GameRepository) AttachLogoImage(ctx context.Context, id int64, url string, imageID *int64) error {
	_, err := r.db.Exec(ctx, `UPDATE games SET logo_url = $2, logo_image_id = $3, updated_at = now() WHERE id = $1`, id, url, imageID)
	return err
}
