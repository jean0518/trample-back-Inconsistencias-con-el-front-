package postgres

import (
	"context"
	"fmt"
	"time"

	"trample-back/internal/domain/dashboard"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DashboardRepository struct {
	db *pgxpool.Pool
}

func NewDashboardRepository(db *pgxpool.Pool) *DashboardRepository {
	return &DashboardRepository{db: db}
}

// InventoryKPIs resuelve los tres indicadores en una sola pasada. Los listings
// se filtran por status = 'active' porque son los que ve la tienda; las cartas
// se cuentan todas, tengan o no inventario.
func (r *DashboardRepository) InventoryKPIs(ctx context.Context) (dashboard.KPIs, error) {
	var kpis dashboard.KPIs
	err := r.db.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM cards),
			(SELECT count(*) FROM inventory_listings WHERE status = 'active'),
			(SELECT COALESCE(sum(quantity), 0) FROM inventory_listings WHERE status = 'active')
	`).Scan(&kpis.CatalogCards, &kpis.ActiveListings, &kpis.InventoryUnits)
	if err != nil {
		return kpis, fmt.Errorf("consultar indicadores de inventario: %w", err)
	}
	return kpis, nil
}

// SalesTotals cuenta los pedidos de la ventana y los dos cortes del panel en
// una sola consulta. Los conteos usan FILTER para no repetir la tabla cuatro
// veces.
func (r *DashboardRepository) SalesTotals(ctx context.Context, since, todayStart time.Time) (dashboard.SalesTotals, error) {
	var t dashboard.SalesTotals
	err := r.db.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE created_at >= $2)::int AS orders_today,
			count(*) FILTER (WHERE created_at >= $3)::int AS orders_last7,
			count(*)::int                                    AS orders,
			COALESCE(sum(total_cop), 0)::bigint              AS total_cop,
			COALESCE(sum(total_usd), 0)::numeric             AS total_usd
		FROM sales
		WHERE created_at >= $1
	`, since, todayStart, todayStart.AddDate(0, 0, -6)).Scan(
		&t.OrdersToday, &t.OrdersLast7, &t.Orders, &t.TotalCOP, &t.TotalUSD,
	)
	if err != nil {
		return t, fmt.Errorf("consultar totales de ventas: %w", err)
	}
	return t, nil
}

// SalesSeries agrupa por día en la zona horaria del servidor, que es la misma
// con la que el panel cuenta "pedidos de hoy".
func (r *DashboardRepository) SalesSeries(ctx context.Context, since time.Time) ([]dashboard.DayCount, error) {
	rows, err := r.db.Query(ctx, `
		SELECT
			to_char(date_trunc('day', created_at), 'YYYY-MM-DD')       AS day,
			count(*)::int                                             AS orders,
			COALESCE(sum(total_cop), 0)::bigint                       AS total_cop,
			COALESCE(sum(total_usd), 0)::numeric                      AS total_usd
		FROM sales
		WHERE created_at >= $1
		GROUP BY date_trunc('day', created_at)
		ORDER BY date_trunc('day', created_at)
	`, since)
	if err != nil {
		return nil, fmt.Errorf("consultar serie de ventas: %w", err)
	}
	defer rows.Close()

	var out []dashboard.DayCount
	for rows.Next() {
		var d dashboard.DayCount
		if err := rows.Scan(&d.Date, &d.Orders, &d.TotalCOP, &d.TotalUSD); err != nil {
			return nil, fmt.Errorf("escanear día de la serie: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// RecentSales une con users para traer el nombre del comprador y cuenta los
// renglones en una subconsulta agregada, para no duplicar el pedido cuando
// tiene varios items.
func (r *DashboardRepository) RecentSales(ctx context.Context, limit int) ([]dashboard.RecentOrder, error) {
	rows, err := r.db.Query(ctx, `
		SELECT
			s.id,
			s.status,
			s.total_cop,
			s.total_usd,
			COALESCE(i.items, 0)::int AS items,
			COALESCE(i.units, 0)::int AS units,
			trim(concat_ws(' ', u.first_name, u.last_name)) AS customer_name,
			to_char(s.created_at, 'YYYY-MM-DD"T"HH24:MI:SS')  AS created_at
		FROM sales s
		LEFT JOIN users u ON u.id = s.user_id
		LEFT JOIN (
			SELECT sale_id, count(*) AS items, sum(quantity) AS units
			FROM sale_items
			GROUP BY sale_id
		) i ON i.sale_id = s.id
		ORDER BY s.created_at DESC, s.id DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("consultar pedidos recientes: %w", err)
	}
	defer rows.Close()

	out := make([]dashboard.RecentOrder, 0, limit)
	for rows.Next() {
		var o dashboard.RecentOrder
		if err := rows.Scan(
			&o.ID, &o.Status, &o.TotalCOP, &o.TotalUSD,
			&o.Items, &o.Units, &o.CustomerName, &o.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("escanear pedido reciente: %w", err)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
