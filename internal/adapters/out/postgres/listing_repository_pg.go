package postgres

import (
	"context"
	"fmt"
	"trample-back/internal/domain/listing"

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
		INSERT INTO inventory_listings (seller_id, variant_id, quantity, price_usd, language)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, seller_id, variant_id, quantity, price_usd, price_cop, status, language, created_at, updated_at
	`, input.SellerID, input.VariantID, input.Quantity, input.PriceUSD, input.Language,
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
			il.quantity, il.price_usd, il.price_cop,
			il.status, il.language, il.created_at, il.updated_at
		FROM inventory_listings il
		JOIN card_variants cv ON cv.id = il.variant_id
		JOIN cards c ON c.id = cv.card_id
		JOIN games g ON g.id = c.game_id
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
			&l.Quantity, &l.PriceUSD, &l.PriceCOP,
			&l.Status, &l.Language, &l.CreatedAt, &l.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("escanear listing: %w", err)
		}
		listings = append(listings, l)
	}
	return listings, rows.Err()
}

func (r *ListingRepository) Delete(ctx context.Context, id, sellerID int64) error {
	tag, err := r.db.Exec(ctx, `
		DELETE FROM inventory_listings WHERE id = $1 AND seller_id = $2
	`, id, sellerID)
	if err != nil {
		return fmt.Errorf("eliminar listing: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("listing %d no encontrado o no pertenece al vendedor", id)
	}
	return nil
}
