package postgres

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"trample-back/internal/domain/catalog"

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

	// 4. UPSERT detalles de Pokémon
	hp, _ := strconv.Atoi(card.HP)
	stage := ""
	if len(card.Subtypes) > 0 {
		stage = card.Subtypes[0]
	}
	evolvesFrom := strings.Join(card.EvolvesFrom, ", ")

	_, err = tx.Exec(ctx, `
		INSERT INTO pokemon_card_details (card_id, hp, types, evolves_from, stage, attacks, weaknesses, resistances, retreat_cost)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (card_id) DO UPDATE SET
			hp           = EXCLUDED.hp,
			types        = EXCLUDED.types,
			evolves_from = EXCLUDED.evolves_from,
			stage        = EXCLUDED.stage,
			attacks      = EXCLUDED.attacks,
			weaknesses   = EXCLUDED.weaknesses,
			resistances  = EXCLUDED.resistances,
			retreat_cost = EXCLUDED.retreat_cost
	`, cardID, hp, card.Types, evolvesFrom, stage,
		nullableJSON(card.Attacks), nullableJSON(card.Weaknesses), nullableJSON(card.Resistances),
		len(card.RetreatCost),
	)
	if err != nil {
		return fmt.Errorf("upsert pokemon_card_details: %w", err)
	}

	// 5. Imágenes de la carta (delete + re-insert para mantenerlas actualizadas)
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

	// 6. Variantes
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

func nullableJSON(raw []byte) interface{} {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return string(raw)
}
