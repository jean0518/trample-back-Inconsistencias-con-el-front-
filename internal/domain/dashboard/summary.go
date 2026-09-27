// Package dashboard modela la vista de resumen del panel administrativo. Sus
// tipos son deliberadamente planos: son la forma exacta en que la respuesta
// viaja al frontend, sin entidades ni reglas de negocio propias.
package dashboard

// Límites de la ventana del resumen. Acotan lo que el frontend puede pedir
// para que un valor a mano no genere una consulta desproporcionada.
const (
	DefaultSeriesDays = 14
	MinSeriesDays     = 1
	MaxSeriesDays     = 90
)

// Summary es el estado completo del panel "Resumen": los indicadores de
// catálogo e inventario, el conteo de pedidos y los más recientes.
//
// Antes el frontend armaba estos números descargando hasta 500 ventas y hasta
// 1000 listings para contarlos en el navegador; aquí llegan ya agregados desde
// PostgreSQL.
type Summary struct {
	// CatalogCards cuenta todas las cartas registradas, tengan o no inventario
	// y vengan de Scrydex o del alta manual.
	CatalogCards int64 `json:"catalog_cards"`
	// ActiveListings cuenta los listings visibles en la tienda (status 'active').
	ActiveListings int64 `json:"active_listings"`
	// InventoryUnits suma las unidades de esos mismos listings activos.
	InventoryUnits int64 `json:"inventory_units"`

	// OrdersToday y OrdersLast7 cuentan pedidos por fecha de creación, en la
	// zona horaria del servidor.
	OrdersToday int `json:"orders_today"`
	OrdersLast7 int `json:"orders_last7"`
	// PeriodOrders, PeriodTotalCOP y PeriodTotalUSD resumen la ventana completa
	// que cubre la serie.
	PeriodOrders   int     `json:"period_orders"`
	PeriodTotalCOP int64   `json:"period_total_cop"`
	PeriodTotalUSD float64 `json:"period_total_usd"`

	// SeriesDays es la cantidad de días que cubre la serie, para que el
	// frontend sepa cuántas barras dibujar sin contar el arreglo.
	SeriesDays int `json:"series_days"`
	// Series tiene un elemento por día de la ventana, en orden ascendente. Los
	// días sin pedidos vienen con Orders en cero para que el gráfico no tenga
	// que rellenar huecos.
	Series []DayCount `json:"series"`
	// RecentOrders son los pedidos más recientes, del más nuevo al más viejo.
	RecentOrders []RecentOrder `json:"recent_orders"`
}

// DayCount es un día de la serie de pedidos.
type DayCount struct {
	// Date es el día en formato YYYY-MM-DD, en la zona horaria del servidor.
	Date     string  `json:"date"`
	Orders   int     `json:"orders"`
	TotalCOP int64   `json:"total_cop"`
	TotalUSD float64 `json:"total_usd"`
}

// KPIs son los tres indicadores de catálogo e inventario.
type KPIs struct {
	CatalogCards   int64
	ActiveListings int64
	InventoryUnits int64
}

// SalesTotals son los conteos e importes de la ventana del resumen. Orders
// cuenta todos sus pedidos; los otros dos son los cortes que pide el panel.
type SalesTotals struct {
	OrdersToday int
	OrdersLast7 int
	Orders      int
	TotalCOP    int64
	TotalUSD    float64
}

// RecentOrder es la vista mínima de un pedido para la tabla del resumen. No
// incluye items ni datos de envío porque el resumen solo los muestra como
// referencia; el detalle se pide al endpoint de ventas.
type RecentOrder struct {
	ID       int64   `json:"id"`
	Status   string  `json:"status"`
	TotalCOP int64   `json:"total_cop"`
	TotalUSD float64 `json:"total_usd"`
	// Items es la cantidad de renglones del pedido, no de unidades.
	Items int `json:"items"`
	// Units es el total de unidades vendidas.
	Units int `json:"units"`
	// CustomerName junta nombre y apellido del comprador. Viaja como texto
	// para que la tabla no necesite una segunda consulta.
	CustomerName string `json:"customer_name"`
	// CreatedAt es la fecha y hora local del servidor, en ISO-8601.
	CreatedAt string `json:"created_at"`
}
