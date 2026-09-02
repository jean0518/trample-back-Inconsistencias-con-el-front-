package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
	"trample-back/internal/domain/catalog"
	"trample-back/internal/ports/out"

	"github.com/jackc/pgx/v5/pgxpool"
)

type CardRepository struct {
	db *pgxpool.Pool
}

func NewCardRepository(db *pgxpool.Pool) *CardRepository {
	return &CardRepository{db: db}
}

func (r *CardRepository) SyncCard(ctx context.Context, gameCode string, card catalog.Card) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// 1. game_id
	var gameID int64
	if err := tx.QueryRow(ctx, `SELECT id FROM games WHERE code = $1`, gameCode).Scan(&gameID); err != nil {
		return fmt.Errorf("juego %q no encontrado: %w", gameCode, err)
	}

	// 2. UPSERT expansión
	var expansionID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO expansions (game_id, external_id, name, code, series, total, release_date, logo_url, symbol_url)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (game_id, external_id) DO UPDATE SET
			name         = EXCLUDED.name,
			code         = EXCLUDED.code,
			series       = EXCLUDED.series,
			total        = EXCLUDED.total,
			release_date = EXCLUDED.release_date,
			logo_url     = EXCLUDED.logo_url,
			symbol_url   = EXCLUDED.symbol_url,
			updated_at   = now()
		RETURNING id
	`, gameID, card.Expansion.ExternalID, card.Expansion.Name, card.Expansion.Code,
		card.Expansion.Series, card.Expansion.Total, card.Expansion.ReleaseDate,
		card.Expansion.Logo, card.Expansion.Symbol,
	).Scan(&expansionID)
	if err != nil {
		return fmt.Errorf("upsert expansión: %w", err)
	}

	// 3. UPSERT carta
	var cardID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO cards (game_id, expansion_id, external_id, name, number, rarity)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (game_id, external_id) DO UPDATE SET
			name         = EXCLUDED.name,
			number       = EXCLUDED.number,
			rarity       = EXCLUDED.rarity,
			updated_at   = now()
		RETURNING id
	`, gameID, expansionID, card.ExternalID, card.Name, card.Number, card.Rarity,
	).Scan(&cardID)
	if err != nil {
		return fmt.Errorf("upsert carta: %w", err)
	}

	// 4. Imágenes de la carta (delete + re-insert para mantenerlas actualizadas)
	if _, err := tx.Exec(ctx, `DELETE FROM card_images WHERE card_id = $1`, cardID); err != nil {
		return fmt.Errorf("limpiar imágenes de carta: %w", err)
	}
	for _, img := range card.Images {
		if _, err := tx.Exec(ctx, `
			INSERT INTO card_images (card_id, image_type, small_url, medium_url, large_url)
			VALUES ($1, $2, $3, $4, $5)
		`, cardID, img.Type, img.Small, img.Medium, img.Large); err != nil {
			return fmt.Errorf("insertar imagen de carta: %w", err)
		}
	}

	// 5. Variantes
	for _, variant := range card.Variants {
		var variantID int64
		err = tx.QueryRow(ctx, `
			INSERT INTO card_variants (card_id, variant_name)
			VALUES ($1, $2)
			ON CONFLICT (card_id, variant_name) DO UPDATE SET updated_at = now()
			RETURNING id
		`, cardID, variant.Name).Scan(&variantID)
		if err != nil {
			return fmt.Errorf("upsert variante %q: %w", variant.Name, err)
		}

		// Imágenes de la variante
		if _, err := tx.Exec(ctx, `DELETE FROM card_images WHERE variant_id = $1`, variantID); err != nil {
			return fmt.Errorf("limpiar imágenes de variante: %w", err)
		}
		for _, img := range variant.Images {
			if _, err := tx.Exec(ctx, `
				INSERT INTO card_images (variant_id, image_type, small_url, medium_url, large_url)
				VALUES ($1, $2, $3, $4, $5)
			`, variantID, img.Type, img.Small, img.Medium, img.Large); err != nil {
				return fmt.Errorf("insertar imagen de variante: %w", err)
			}
		}

		// Precio NM
		if variant.NMPrice != nil {
			_, err = tx.Exec(ctx, `
				INSERT INTO variant_prices (variant_id, condition, price_usd, price_cop, trm_used)
				VALUES ($1, 'near_mint', $2, $3, $4)
				ON CONFLICT (variant_id, condition) DO UPDATE SET
					price_usd  = EXCLUDED.price_usd,
					price_cop  = EXCLUDED.price_cop,
					trm_used   = EXCLUDED.trm_used,
					fetched_at = now()
			`, variantID, variant.NMPrice.MarketUSD, variant.NMPrice.MarketCOP, variant.NMPrice.TRMUsed)
			if err != nil {
				return fmt.Errorf("upsert precio variante %q: %w", variant.Name, err)
			}

			if _, err := tx.Exec(ctx, `UPDATE card_variants SET last_price_check_at = now() WHERE id = $1`, variantID); err != nil {
				return fmt.Errorf("actualizar last_price_check_at: %w", err)
			}
		}
	}

	return tx.Commit(ctx)
}

func (r *CardRepository) ListCards(ctx context.Context, p out.ListCardsParams) ([]catalog.CardSummary, int, error) {
	offset := (p.Page - 1) * p.PageSize
	rows, err := r.db.Query(ctx, `
		SELECT
			c.id,
			c.external_id,
			COALESCE(il_owner.language, '') AS card_language,
			COALESCE(il_owner.owner_name, '') AS card_owner_name,
			c.name,
			c.number,
			c.rarity,
			g.code,
			e.id,
			e.name,
			e.code,
			e.logo_url,
			e.symbol_url,
			COALESCE((SELECT ci.small_url  FROM card_images ci WHERE ci.card_id = c.id LIMIT 1), ''),
			COALESCE((SELECT ci.medium_url FROM card_images ci WHERE ci.card_id = c.id LIMIT 1), ''),
			COALESCE((SELECT ci.large_url  FROM card_images ci WHERE ci.card_id = c.id LIMIT 1), ''),
			COALESCE(
				(SELECT json_agg(json_build_object(
					'name',      cv.variant_name,
					'price_usd', COALESCE(vp.price_usd, 0),
					'price_cop', COALESCE(vp.price_cop, 0)
				))
				FROM card_variants cv
				LEFT JOIN variant_prices vp ON vp.variant_id = cv.id AND vp.condition = 'near_mint'
				WHERE cv.card_id = c.id),
				'[]'
			)::text,
			COALESCE((
				SELECT SUM(il.quantity)
				FROM inventory_listings il
				JOIN card_variants lcv ON lcv.id = il.variant_id
				WHERE lcv.card_id = c.id AND il.status = 'active' AND il.quantity > 0
			), 0)::int AS stock,
			COALESCE((
				SELECT json_agg(l ORDER BY l.stock DESC)
				FROM (
					SELECT il.language AS name, SUM(il.quantity)::int AS stock
					FROM inventory_listings il
					JOIN card_variants lcv ON lcv.id = il.variant_id
					WHERE lcv.card_id = c.id AND il.status = 'active' AND il.quantity > 0
					  AND il.language IS NOT NULL AND il.language <> ''
					GROUP BY il.language
				) l
			), '[]')::text AS languages,
			COUNT(*) OVER() AS total
		FROM cards c
		JOIN games      g ON g.id = c.game_id
		JOIN expansions e ON e.id = c.expansion_id
		LEFT JOIN LATERAL (
			SELECT il.language, COALESCE(o.name, 'trampleStore') AS owner_name
			FROM inventory_listings il
			JOIN card_variants lcv ON lcv.id = il.variant_id
			LEFT JOIN owners o ON o.id = il.owner_id
			WHERE lcv.card_id = c.id AND il.status = 'active' AND il.quantity > 0
			ORDER BY il.id ASC
			LIMIT 1
		) il_owner ON true
		WHERE ($1::text   = '' OR g.code          = $1)
		  AND ($2::bigint = 0  OR c.expansion_id  = $2)
		  AND ($3::text   = '' OR c.name ILIKE '%' || $3 || '%')
		  AND ($6::text   = '' OR c.rarity      = $6)
		  AND ($7::bigint = 0  OR EXISTS (
			SELECT 1 FROM inventory_listings il2
			JOIN card_variants lcv2 ON lcv2.id = il2.variant_id
			WHERE lcv2.card_id = c.id AND il2.owner_id = $7 AND il2.status = 'active' AND il2.quantity > 0
		  ))
		  AND ($8::text   = '' OR EXISTS (
			SELECT 1 FROM inventory_listings il3
			JOIN card_variants lcv3 ON lcv3.id = il3.variant_id
			WHERE lcv3.card_id = c.id AND il3.language = $8 AND il3.status = 'active' AND il3.quantity > 0
		  ))
		  -- El catálogo público refleja el inventario: solo cartas con
		  -- al menos un listing activo y con stock.
		  AND EXISTS (
			SELECT 1
			FROM inventory_listings il
			JOIN card_variants lcv ON lcv.id = il.variant_id
			WHERE lcv.card_id = c.id AND il.status = 'active' AND il.quantity > 0
		  )
		ORDER BY e.release_date DESC, c.id
		LIMIT $4 OFFSET $5
	`, p.GameCode, p.ExpansionID, p.Name, p.PageSize, offset, p.Rarity, p.OwnerID, p.Language)
	if err != nil {
		return nil, 0, fmt.Errorf("listar cartas: %w", err)
	}
	defer rows.Close()

	var (
		result []catalog.CardSummary
		total  int
	)
	for rows.Next() {
	var (
		s            catalog.CardSummary
		variantsJSON string
		languagesJSON string
	)
	if err := rows.Scan(
		&s.ID, &s.ExternalID, &s.Language, &s.OwnerName,
		&s.Name, &s.Number, &s.Rarity, &s.GameCode,
		&s.Expansion.ID, &s.Expansion.Name, &s.Expansion.Code,
		&s.Expansion.LogoURL, &s.Expansion.SymbolURL,
		&s.Image.Small, &s.Image.Medium, &s.Image.Large,
		&variantsJSON,
		&s.Stock,
		&languagesJSON,
		&total,
	); err != nil {
		return nil, 0, err
	}
	if err := json.Unmarshal([]byte(variantsJSON), &s.Variants); err != nil {
		return nil, 0, fmt.Errorf("parsear variantes: %w", err)
	}
	if err := json.Unmarshal([]byte(languagesJSON), &s.Languages); err != nil {
		return nil, 0, fmt.Errorf("parsear idiomas: %w", err)
	}
	result = append(result, s)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return result, total, nil
}

func (r *CardRepository) GetVariantID(ctx context.Context, gameCode, externalID, variantName string) (int64, error) {
	var variantID int64
	err := r.db.QueryRow(ctx, `
		SELECT cv.id
		FROM card_variants cv
		JOIN cards c ON c.id = cv.card_id
		JOIN games g ON g.id = c.game_id
		WHERE g.code = $1 AND c.external_id = $2 AND cv.variant_name = $3
	`, gameCode, externalID, variantName).Scan(&variantID)
	if err != nil {
		return 0, fmt.Errorf("variante %q de carta %q (%s): %w", variantName, externalID, gameCode, err)
	}
	return variantID, nil
}

func (r *CardRepository) GetExternalID(ctx context.Context, id int64) (string, error) {
	var externalID string
	err := r.db.QueryRow(ctx, `SELECT external_id FROM cards WHERE id = $1`, id).Scan(&externalID)
	if err != nil {
		return "", fmt.Errorf("carta %d no encontrada: %w", id, err)
	}
	return externalID, nil
}

// ListStaleCards devuelve cartas con al menos un listing activo cuyo precio
// (el mínimo entre sus variantes) no se consulta en Scrydex hace más de
// olderThan, o nunca se consultó. Las más desactualizadas primero.
func (r *CardRepository) ListStaleCards(ctx context.Context, olderThan time.Duration, limit int) ([]out.StaleCardRef, error) {
	threshold := time.Now().Add(-olderThan)
	rows, err := r.db.Query(ctx, `
		SELECT c.id, g.code, c.external_id
		FROM cards c
		JOIN games g ON g.id = c.game_id
		JOIN card_variants cv ON cv.card_id = c.id
		WHERE EXISTS (
			SELECT 1 FROM inventory_listings il
			WHERE il.variant_id = cv.id AND il.status = 'active' AND il.quantity > 0
		)
		GROUP BY c.id, g.code, c.external_id
		HAVING MIN(cv.last_price_check_at) IS NULL OR MIN(cv.last_price_check_at) < $1
		ORDER BY MIN(cv.last_price_check_at) ASC NULLS FIRST
		LIMIT $2
	`, threshold, limit)
	if err != nil {
		return nil, fmt.Errorf("listar cartas con precio desactualizado: %w", err)
	}
	defer rows.Close()

	var result []out.StaleCardRef
	for rows.Next() {
		var ref out.StaleCardRef
		if err := rows.Scan(&ref.ID, &ref.GameCode, &ref.ExternalID); err != nil {
			return nil, err
		}
		result = append(result, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *CardRepository) DeleteCard(ctx context.Context, id int64) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM cards WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("eliminar carta: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("carta %d no encontrada", id)
	}
	return nil
}
