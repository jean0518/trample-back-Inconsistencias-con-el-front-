package postgres

import (
	"context"
	"errors"
	"fmt"
	"trample-back/internal/domain/listing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ListingRepository struct {
	db *pgxpool.Pool
}

func NewListingRepository(db *pgxpool.Pool) *ListingRepository {
	return &ListingRepository{db: db}
}

func (r *ListingRepository) Create(ctx context.Context, input listing.CreateInput) (listing.Listing, error) {
	var l listing.Listing
	err := r.db.QueryRow(ctx, `
		INSERT INTO inventory_listings (seller_id, variant_id, quantity, price_usd, price_cop, language)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, seller_id, variant_id, quantity, price_usd, price_cop, status, language, created_at, updated_at
	`, input.SellerID, input.VariantID, input.Quantity, input.PriceUSD, input.PriceCOP, input.Language,
	).Scan(
		&l.ID, &l.SellerID, &l.VariantID,
		&l.Quantity, &l.PriceUSD, &l.PriceCOP, &l.Status, &l.Language,
		&l.CreatedAt, &l.UpdatedAt,
	)
	if err != nil {
		return listing.Listing{}, fmt.Errorf("crear listing: %w", err)
	}
	return l, nil
}

func (r *ListingRepository) ListBySeller(ctx context.Context, sellerID int64, limit, offset int) ([]listing.Listing, error) {
	rows, err := r.db.Query(ctx, `
		SELECT
			il.id, il.seller_id, il.variant_id,
			c.id AS game_id, g.name AS game_name,
			c.name AS card_name,
			COALESCE(
				(SELECT vi.small_url FROM card_images vi WHERE vi.variant_id = cv.id AND vi.small_url <> '' LIMIT 1),
				(SELECT ci.small_url FROM card_images ci WHERE ci.card_id = c.id AND ci.small_url <> '' LIMIT 1),
				''
			) AS card_image,
			e.name AS expansion_name,
			cv.variant_name,
			il.quantity, il.price_usd, il.price_cop,
			il.status, il.language, il.created_at, il.updated_at
		FROM inventory_listings il
		JOIN card_variants cv ON cv.id = il.variant_id
		JOIN cards c ON c.id = cv.card_id
		JOIN games g ON g.id = c.game_id
		JOIN expansions e ON e.id = c.expansion_id
		WHERE il.seller_id = $1
		ORDER BY il.created_at DESC
		LIMIT $2 OFFSET $3
	`, sellerID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("listar inventory_listings: %w", err)
	}
	defer rows.Close()

	var listings []listing.Listing
	for rows.Next() {
		var l listing.Listing
		if err := rows.Scan(
			&l.ID, &l.SellerID, &l.VariantID,
			&l.GameID, &l.GameName,
			&l.CardName, &l.CardImage, &l.ExpansionName, &l.VariantName,
			&l.Quantity, &l.PriceUSD, &l.PriceCOP,
			&l.Status, &l.Language, &l.CreatedAt, &l.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("escanear listing: %w", err)
		}
		listings = append(listings, l)
	}
	return listings, rows.Err()
}

// UpdateQuantity cambia la cantidad y ajusta el estado automáticamente:
// quantity 0 ⇒ 'inactive'; quantity > 0 reactiva un listing 'inactive'.
// El estado 'sold' nunca se modifica desde aquí.
func (r *ListingRepository) UpdateQuantity(ctx context.Context, input listing.UpdateStockInput) (listing.Listing, error) {
	var l listing.Listing
	err := r.db.QueryRow(ctx, `
		UPDATE inventory_listings
		SET quantity = $3,
		    status = CASE
		        WHEN $3 = 0 THEN 'inactive'
		        WHEN status = 'inactive' THEN 'active'
		        ELSE status
		    END,
		    updated_at = now()
		WHERE id = $1 AND seller_id = $2
		RETURNING id, seller_id, variant_id, quantity, price_usd, price_cop, status, language, created_at, updated_at
	`, input.ID, input.SellerID, input.Quantity,
	).Scan(
		&l.ID, &l.SellerID, &l.VariantID,
		&l.Quantity, &l.PriceUSD, &l.PriceCOP, &l.Status, &l.Language,
		&l.CreatedAt, &l.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return listing.Listing{}, fmt.Errorf("listing %d: %w", input.ID, listing.ErrNotFound)
		}
		return listing.Listing{}, fmt.Errorf("actualizar cantidad del listing %d: %w", input.ID, err)
	}
	return l, nil
}

// FindBySellerAndVariant devuelve el listing vigente (no 'sold') del vendedor
// para una variante. Se usa al importar para sumar stock en vez de duplicar.
func (r *ListingRepository) FindBySellerAndVariant(ctx context.Context, sellerID, variantID int64) (listing.Listing, error) {
	var l listing.Listing
	err := r.db.QueryRow(ctx, `
		SELECT id, seller_id, variant_id, quantity, price_usd, price_cop, status, language, created_at, updated_at
		FROM inventory_listings
		WHERE seller_id = $1 AND variant_id = $2 AND status <> 'sold'
		ORDER BY updated_at DESC
		LIMIT 1
	`, sellerID, variantID,
	).Scan(
		&l.ID, &l.SellerID, &l.VariantID,
		&l.Quantity, &l.PriceUSD, &l.PriceCOP, &l.Status, &l.Language,
		&l.CreatedAt, &l.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return listing.Listing{}, listing.ErrNotFound
		}
		return listing.Listing{}, fmt.Errorf("buscar listing por variante: %w", err)
	}
	return l, nil
}

// AddQuantity suma cantidad al listing y reactiva un 'inactive'
// (la cantidad a sumar siempre es > 0). Nunca toca un 'sold'.
func (r *ListingRepository) AddQuantity(ctx context.Context, input listing.UpdateStockInput) (listing.Listing, error) {
	var l listing.Listing
	err := r.db.QueryRow(ctx, `
		UPDATE inventory_listings
		SET quantity = quantity + $3,
		    status = CASE WHEN status = 'inactive' THEN 'active' ELSE status END,
		    updated_at = now()
		WHERE id = $1 AND seller_id = $2 AND status <> 'sold'
		RETURNING id, seller_id, variant_id, quantity, price_usd, price_cop, status, language, created_at, updated_at
	`, input.ID, input.SellerID, input.Quantity,
	).Scan(
		&l.ID, &l.SellerID, &l.VariantID,
		&l.Quantity, &l.PriceUSD, &l.PriceCOP, &l.Status, &l.Language,
		&l.CreatedAt, &l.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return listing.Listing{}, fmt.Errorf("listing %d: %w", input.ID, listing.ErrNotFound)
		}
		return listing.Listing{}, fmt.Errorf("sumar stock del listing %d: %w", input.ID, err)
	}
	return l, nil
}

func (r *ListingRepository) Delete(ctx context.Context, id, sellerID int64) error {
	tag, err := r.db.Exec(ctx, `
		DELETE FROM inventory_listings WHERE id = $1 AND seller_id = $2
	`, id, sellerID)
	if err != nil {
		return fmt.Errorf("eliminar listing: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("listing %d: %w", id, listing.ErrNotFound)
	}
	return nil
}
