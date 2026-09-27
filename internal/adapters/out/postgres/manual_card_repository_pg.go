package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"trample-back/internal/domain/catalog"
)

// ManualCardRepositoryPG da de alta cartas que el staff registra a mano.
// Vive aparte de CardRepository porque su forma de consultar es distinta: el
// alta manual se identifica por external_id derivado del nombre, no por el
// número de carta de Scrydex, y necesita resolver tres niveles (expansión,
// carta, variante) en una sola transacción.
type ManualCardRepositoryPG struct {
	db *pgxpool.Pool
}

// NewManualCardRepositoryPG construye el repositorio sobre el pool de pgx.
func NewManualCardRepositoryPG(db *pgxpool.Pool) *ManualCardRepositoryPG {
	return &ManualCardRepositoryPG{db: db}
}

// UpsertManualCard resuelve o crea expansión, carta y variante, y devuelve los
// IDs persistidos junto con los datos canónicos que quedaron guardados.
//
// Todo ocurre en una transacción porque las tres filas están encadenadas: si la
// variante se creara y la carta fallara, quedaría una variante huérfana que el
// catálogo mostraría sin carta.
//
// Es idempotente: volver a registrar la misma carta con el mismo external_id
// devuelve la fila existente en lugar de duplicarla, y el stock se suma al
// listing que ya había.
func (r *ManualCardRepositoryPG) UpsertManualCard(ctx context.Context, card catalog.ManualCard) (catalog.ManualCardResult, error) {
	var result catalog.ManualCardResult
	card.ExternalID = catalog.ManualExternalIDFor(card.ExternalID, card.Name, card.Number)

	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		gameID, err := lookupGameID(ctx, tx, card.GameCode)
		if err != nil {
			return err
		}

		expansionID, expansionCreated, err := resolveExpansion(ctx, tx, gameID, card)
		if err != nil {
			return err
		}

		cardID, cardCreated, storedName, err := upsertCard(ctx, tx, gameID, expansionID, card)
		if err != nil {
			return err
		}

		variantID, storedVariant, err := upsertVariant(ctx, tx, cardID, card.VariantName)
		if err != nil {
			return err
		}

		result = catalog.ManualCardResult{
			CardID:           cardID,
			ExpansionID:      expansionID,
			VariantID:        variantID,
			CardName:         storedName,
			ExternalID:       card.ExternalID,
			VariantName:      storedVariant,
			CardCreated:      cardCreated,
			ExpansionCreated: expansionCreated,
		}
		return nil
	})
	if err != nil {
		return catalog.ManualCardResult{}, err
	}
	return result, nil
}

// lookupGameID traduce el código de juego ("pokemon", "tcg") al id interno.
func lookupGameID(ctx context.Context, tx pgx.Tx, gameCode string) (int64, error) {
	var gameID int64
	const q = `SELECT id FROM games WHERE code = $1`
	if err := tx.QueryRow(ctx, q, gameCode).Scan(&gameID); err != nil {
		return 0, fmt.Errorf("juego %q no encontrado en DB: %w", gameCode, err)
	}
	return gameID, nil
}

// resolveExpansion usa la expansión indicada por el staff o, si el alta trajo
// una nueva, la crea. El external_id derivado del nombre hace que registrar dos
// veces el mismo set reutilice la fila existente.
func resolveExpansion(ctx context.Context, tx pgx.Tx, gameID int64, card catalog.ManualCard) (int64, bool, error) {
	if card.ExpansionID > 0 {
		const q = `SELECT id FROM expansions WHERE id = $1 AND game_id = $2`
		var id int64
		if err := tx.QueryRow(ctx, q, card.ExpansionID, gameID).Scan(&id); err != nil {
			return 0, false, fmt.Errorf("la expansión seleccionada no existe en este juego: %w", err)
		}
		return id, false, nil
	}

	externalID, err := catalog.ExpansionExternalIDForManual(card.ExpansionName)
	if err != nil {
		return 0, false, err
	}

	var id int64
	var created bool
	const q = `
		INSERT INTO expansions (game_id, external_id, name, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (game_id, external_id) DO UPDATE SET updated_at = now()
		RETURNING id, (xmax = 0) AS created`
	// xmax = 0 distingue un INSERT real de un UPDATE sobre la fila existente;
	// es el truco estándar con ON CONFLICT, ya que RETURNING no dice cuál de
	// los dos caminos se ejecutó.
	if err := tx.QueryRow(ctx, q, gameID, externalID, card.ExpansionName).Scan(&id, &created); err != nil {
		return 0, false, fmt.Errorf("no se pudo guardar la expansión: %w", err)
	}
	return id, created, nil
}

// upsertCard crea la carta o reutiliza la existente, y devuelve también el
// nombre canónico que quedó guardado: si la carta ya venía de Scrydex, su
// nombre es el de la API y el frontend debe mostrar ese, no el que escribió el
// staff a mano.
func upsertCard(ctx context.Context, tx pgx.Tx, gameID, expansionID int64, card catalog.ManualCard) (cardID int64, created bool, storedName string, err error) {
	const q = `
		INSERT INTO cards (game_id, expansion_id, external_id, name, number, rarity, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, now())
		ON CONFLICT (game_id, external_id) DO UPDATE SET
			name         = EXCLUDED.name,
			number       = EXCLUDED.number,
			rarity       = EXCLUDED.rarity,
			expansion_id = EXCLUDED.expansion_id,
			updated_at   = now()
		RETURNING id, name, (xmax = 0) AS created`
	if err := tx.QueryRow(ctx, q, gameID, expansionID, card.ExternalID, card.Name, card.Number, card.Rarity).Scan(&cardID, &storedName, &created); err != nil {
		return 0, false, "", fmt.Errorf("no se pudo guardar la carta: %w", err)
	}
	return cardID, created, storedName, nil
}

// upsertVariant resuelve el acabado pedido, reutilizando la variante si la
// carta ya la tenía. El unique (card_id, variant_name) hace el trabajo.
func upsertVariant(ctx context.Context, tx pgx.Tx, cardID int64, variantName string) (int64, string, error) {
	const q = `
		INSERT INTO card_variants (card_id, variant_name, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (card_id, variant_name) DO UPDATE SET updated_at = now()
		RETURNING id, variant_name`
	var id int64
	var stored string
	if err := tx.QueryRow(ctx, q, cardID, variantName).Scan(&id, &stored); err != nil {
		return 0, "", fmt.Errorf("no se pudo guardar la variante: %w", err)
	}
	return id, stored, nil
}
