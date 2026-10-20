package dashboard

import "time"

// MaxBreakdownDays acota el rango del desglose: la agregación recorre las
// ventas del período y un rango sin límite haría una consulta desproporcionada.
const MaxBreakdownDays = 731

// Límites del top de cartas y productos.
const (
	DefaultBreakdownTop = 10
	MaxBreakdownTop     = 50
)

// BreakdownSale es lo que la agregación necesita de una venta del período.
// Lines solo viene en las ventas pagadas (las únicas que suman ingresos).
type BreakdownSale struct {
	ID            int64
	Status        string
	TotalCOP      int64
	ShippingCOP   int64
	PaymentMethod string
	Fulfillment   string
	CreatedAt     time.Time
	Lines         []BreakdownLine
}

// BreakdownLine es una línea vendida: carta o producto sellado.
type BreakdownLine struct {
	// Sealed distingue el producto sellado de la carta.
	Sealed bool
	// CardID es el id de la carta; ProductID el del producto (nil si se borró).
	CardID    int64
	ProductID *int64
	Name      string
	Language  string
	Quantity  int
	PriceCOP  int64
}

// Breakdown es la respuesta de GET /admin/dashboard/sales-breakdown.
type Breakdown struct {
	From        string `json:"from"`
	To          string `json:"to"`
	Granularity string `json:"granularity"` // day | month

	Totals    BreakdownTotals `json:"totals"`
	Previous  PeriodTotals    `json:"previous"`
	Cards     CategoryTotals  `json:"cards"`
	Sealed    CategoryTotals  `json:"sealed"`
	Pending   OrdersAmount    `json:"pending"`
	Cancelled OrdersAmount    `json:"cancelled"`

	Series        []SeriesBucket `json:"series"`
	TopCards      []TopCard      `json:"top_cards"`
	TopSealed     []TopSealed    `json:"top_sealed"`
	ByPayment     []KeyOrders    `json:"by_payment"`
	ByFulfillment []KeyOrders    `json:"by_fulfillment"`
	ByLanguage    []KeyUnits     `json:"by_language"`
	// Heatmap cuenta los pedidos pagados por día de la semana (0 = lunes) y
	// hora, en hora de Colombia: 7 filas de 24 columnas.
	Heatmap [7][24]int `json:"heatmap"`
}

// BreakdownTotals son los totales de las ventas pagadas del período.
// RevenueCOP incluye el envío.
type BreakdownTotals struct {
	RevenueCOP   int64 `json:"revenue_cop"`
	Orders       int   `json:"orders"`
	Units        int   `json:"units"`
	AvgTicketCOP int64 `json:"avg_ticket_cop"`
	ShippingCOP  int64 `json:"shipping_cop"`
}

// PeriodTotals son los totales del período anterior, para comparar.
type PeriodTotals struct {
	RevenueCOP int64 `json:"revenue_cop"`
	Orders     int   `json:"orders"`
	Units      int   `json:"units"`
}

// CategoryTotals son los totales de cartas o de sellado, sin el envío. Orders
// cuenta los pedidos con al menos una línea de la categoría.
type CategoryTotals struct {
	RevenueCOP int64 `json:"revenue_cop"`
	Units      int   `json:"units"`
	Orders     int   `json:"orders"`
}

// OrdersAmount cuenta pedidos e importe.
type OrdersAmount struct {
	Orders   int   `json:"orders"`
	TotalCOP int64 `json:"total_cop"`
}

// SeriesBucket es un día o un mes de la serie (incluye los vacíos).
type SeriesBucket struct {
	Start     string `json:"start"`
	CardsCOP  int64  `json:"cards_cop"`
	SealedCOP int64  `json:"sealed_cop"`
	Orders    int    `json:"orders"`
}

// TopCard es una carta del top, por carta e idioma.
type TopCard struct {
	CardID     int64  `json:"card_id"`
	Name       string `json:"name"`
	Language   string `json:"language"`
	Units      int    `json:"units"`
	RevenueCOP int64  `json:"revenue_cop"`
}

// TopSealed es un producto sellado del top. ProductID es nil si se borró (se
// agrupa por título).
type TopSealed struct {
	ProductID  *int64 `json:"product_id"`
	Title      string `json:"title"`
	Units      int    `json:"units"`
	RevenueCOP int64  `json:"revenue_cop"`
}

// KeyOrders es un desglose por método de pago o forma de entrega.
type KeyOrders struct {
	Key        string `json:"key"`
	Orders     int    `json:"orders"`
	RevenueCOP int64  `json:"revenue_cop"`
}

// KeyUnits es el desglose por idioma de las cartas.
type KeyUnits struct {
	Key        string `json:"key"`
	Units      int    `json:"units"`
	RevenueCOP int64  `json:"revenue_cop"`
}

// SealedInventory resume el inventario de producto sellado para el Resumen.
type SealedInventory struct {
	Products       int    `json:"products"`
	Active         int    `json:"active"`
	Hidden         int    `json:"hidden"`
	StockUnits     int    `json:"stock_units"`
	ReservedUnits  int    `json:"reserved_units"`
	AvailableUnits int    `json:"available_units"`
	StockValueCOP  int64  `json:"stock_value_cop"`
	SoldOut        []Item `json:"sold_out"`
	LowStock       []Low  `json:"low_stock"`
}

// Item identifica un producto en una lista del resumen.
type Item struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
}

// Low es un producto activo con pocas unidades disponibles.
type Low struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Available int    `json:"available"`
}

// SealedProduct es lo que el resumen necesita de cada producto sellado.
type SealedProduct struct {
	ID       int64
	Title    string
	Active   bool
	Stock    int
	Reserved int
	PriceCOP int64
}

// LowStockMax es el máximo de unidades disponibles para "pocas unidades".
const LowStockMax = 3
