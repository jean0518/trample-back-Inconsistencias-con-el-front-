package listing

import "time"

type Listing struct {
	ID        int64
	SellerID  int64
	VariantID int64
	GameID    int64
	GameName  string
	Quantity  int
	PriceUSD  float64
	PriceCOP  float64
	Status    string
	Language  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type CreateInput struct {
	SellerID  int64
	VariantID int64
	Quantity  int
	PriceUSD  float64
	Language  string
}
