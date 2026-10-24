package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"trample-back/internal/domain/payment"
	"trample-back/internal/domain/reservation"
	"trample-back/internal/domain/sale"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SaleRepository struct {
	db *pgxpool.Pool
}

func NewSaleRepository(db *pgxpool.Pool) *SaleRepository {
	return &SaleRepository{db: db}
}

func (r *SaleRepository) Create(ctx context.Context, s sale.Sale) (sale.Sale, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return sale.Sale{}, err
	}
	defer tx.Rollback(ctx)

	// La confirmación de las reservas va en la misma transacción que la venta: si
	// registrar la venta falla, las reservas siguen activas y el cliente no pierde
	// el carrito. Una recogida pendiente sigue siendo una reserva hasta que se
	// pague; solo las ventas ya cobradas registran el historial 'sold'.
	logStatus := reservation.LogSold
	if s.Status == sale.StatusPendingPickup {
		logStatus = reservation.LogReservada
	}
	if len(s.ReservationIDs) > 0 {
		if err := confirmReservationsStrictTx(ctx, tx, s.UserID, s.ReservationIDs, logStatus); err != nil {
			return sale.Sale{}, err
		}
	}

	if len(s.ProductReservationIDs) > 0 {
		if err := confirmProductReservationsStrictTx(ctx, tx, s.UserID, s.ProductReservationIDs, logStatus); err != nil {
			return sale.Sale{}, err
		}
	}

	created, err := insertSale(ctx, tx, s, true)
	if err != nil {
		return sale.Sale{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sale.Sale{}, err
	}
	return created, nil
}

// lockStockTx bloquea los listings de la venta y dice si alguno no tiene las
// unidades que se van a vender (o ya no existe). Se bloquean en orden de id para
// que dos ventas simultáneas no se interbloqueen.
func lockStockTx(ctx context.Context, tx pgx.Tx, items []sale.SaleItem) (bool, error) {
	need := map[int64]int{}
	short := false
	for _, it := range items {
		if it.ListingID == nil {
			short = true // el listing se eliminó: no hay de dónde descontar
			continue
		}
		need[*it.ListingID] += it.Quantity
	}
	ids := make([]int64, 0, len(need))
	for id := range need {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	for _, id := range ids {
		var qty int
		err := tx.QueryRow(ctx, `SELECT quantity FROM inventory_listings WHERE id = $1 FOR UPDATE`, id).Scan(&qty)
		if errors.Is(err, pgx.ErrNoRows) {
			short = true
			continue
		}
		if err != nil {
			return false, fmt.Errorf("bloquear listing %d: %w", id, err)
		}
		if qty < need[id] {
			short = true
		}
	}
	return short, nil
}

// insertSale persiste la venta y sus items, y consume el inventario. Solo se
// llama con ventas ya pagadas: efectivo, transferencia o un pago de la pasarela
// que el backend acaba de aprobar. Un pago en curso no pasa por aquí.
//
// Si el inventario ya no alcanza, el modo estricto (efectivo/transferencia, donde
// todavía no se cobró nada o lo registra el staff) falla con ErrInsufficientStock.
// El no estricto (pago aprobado: el dinero ya entró) crea la venta igual pero
// marcada para revisión, en vez de vender cartas que no existen sin que nadie lo vea.
func insertSale(ctx context.Context, tx pgx.Tx, s sale.Sale, strict bool) (sale.Sale, error) {
	short, err := lockStockTx(ctx, tx, s.Items)
	if err != nil {
		return sale.Sale{}, err
	}
	// Los productos se bloquean después de los listings, siempre en ese orden,
	// para que dos ventas simultáneas no se interbloqueen.
	productShort, err := lockProductStockTx(ctx, tx, s.ProductItems)
	if err != nil {
		return sale.Sale{}, err
	}
	short = short || productShort
	if short {
		if strict {
			return sale.Sale{}, sale.ErrInsufficientStock
		}
		s.RequiresReview = true
	}

	created := s
	err = tx.QueryRow(ctx, `
		INSERT INTO sales (user_id, total_cop, total_usd, shipping_cop, shipping_usd, fulfillment, address, city, phone, payment_method, status, payment_reference, bold_payment_id, paid_at, requires_review, reservation_ids, pickup_deadline, product_reservation_ids)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		RETURNING id, created_at
	`, s.UserID, s.TotalCOP, s.TotalUSD, s.ShippingCOP, s.ShippingUSD, s.Fulfillment, s.Address, s.City, s.Phone, s.PaymentMethod, s.Status,
		nullableStr(s.PaymentReference), nullableStr(s.BoldPaymentID), nullableTime(s.PaidAt), s.RequiresReview, nullableIDs(s.ReservationIDs), nullableTime(s.PickupDeadline), nullableIDs(s.ProductReservationIDs),
	).Scan(&created.ID, &created.CreatedAt)
	if err != nil {
		return sale.Sale{}, fmt.Errorf("crear venta: %w", err)
	}

	created.Items = make([]sale.SaleItem, 0, len(s.Items))
	for _, it := range s.Items {
		var item sale.SaleItem
		// La variante viene de la reserva; si falta (checkouts anteriores a este
		// campo) se toma del listing. El juego y la expansión salen de la carta.
		err := tx.QueryRow(ctx, `
			INSERT INTO sale_items (sale_id, listing_id, card_id, card_name, language, quantity, price_cop, price_usd,
			                        variant_name, game_code, game_name, expansion_name)
			SELECT $1, $2, $3, $4, $5, $6, $7, $8,
			       COALESCE(NULLIF($9, ''), (SELECT cv.variant_name FROM inventory_listings il
			                                 JOIN card_variants cv ON cv.id = il.variant_id WHERE il.id = $2)),
			       g.code, g.name, e.name
			FROM (SELECT 1) one
			LEFT JOIN cards c ON c.id = $3
			LEFT JOIN games g ON g.id = c.game_id
			LEFT JOIN expansions e ON e.id = c.expansion_id
			RETURNING id, listing_id, card_id, card_name, language, quantity, price_cop, price_usd,
			          COALESCE(variant_name, ''), COALESCE(game_code, ''), COALESCE(game_name, ''), COALESCE(expansion_name, '')
		`, created.ID, it.ListingID, it.CardID, it.CardName, it.Language, it.Quantity, it.PriceCOP, it.PriceUSD, it.VariantName).Scan(
			&item.ID, &item.ListingID, &item.CardID, &item.CardName,
			&item.Language, &item.Quantity, &item.PriceCOP, &item.PriceUSD,
			&item.VariantName, &item.GameCode, &item.GameName, &item.ExpansionName,
		)
		if err != nil {
			return sale.Sale{}, fmt.Errorf("crear item de venta: %w", err)
		}
		created.Items = append(created.Items, item)
	}

	created.ProductItems, err = insertProductItemsTx(ctx, tx, created.ID, s.ProductItems)
	if err != nil {
		return sale.Sale{}, err
	}

	// Al vender se descuenta el inventario (la carta se consume aquí, no al
	// reservar): si el listing llega a 0 unidades pasa a 'inactive' y deja de
	// aparecer en el catálogo público; si le quedan unidades, permanece activo
	// y vendible. El detalle de la venta queda en el historial de ventas.
	for _, it := range s.Items {
		if _, err := tx.Exec(ctx, `
			UPDATE inventory_listings
			SET quantity = CASE WHEN quantity - $2 <= 0 THEN 0 ELSE quantity - $2 END,
			    status = CASE WHEN quantity - $2 <= 0 THEN 'inactive' ELSE status END,
			    updated_at = now()
			WHERE id = $1
		`, it.ListingID, it.Quantity); err != nil {
			return sale.Sale{}, fmt.Errorf("descontar listing vendido: %w", err)
		}
	}
	if err := decrementProductStockTx(ctx, tx, s.ProductItems); err != nil {
		return sale.Sale{}, err
	}

	return created, nil
}

func nullableStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

// nullableIDs evita enviar NULL a la columna reservation_ids (NOT NULL): las
// ventas en efectivo o transferencia no tienen reservas asociadas y se guardan
// como un arreglo vacío.
func nullableIDs(ids []int64) any {
	if len(ids) == 0 {
		return []int64{}
	}
	return ids
}

// saleSelect lee la venta (alias s) con el nombre y correo del comprador (alias
// u). Quien la use debe calificar las columnas con s. en sus filtros.
const saleSelect = `
	SELECT s.id, s.user_id, s.total_cop, s.total_usd, s.shipping_cop, s.shipping_usd, s.fulfillment, s.address, s.city, s.phone,
	       s.payment_method, s.status, s.created_at,
	       COALESCE(s.payment_reference, ''), COALESCE(s.bold_payment_id, ''), COALESCE(s.paid_at, to_timestamp(0)),
	       s.requires_review, s.reservation_ids, COALESCE(s.pickup_deadline, to_timestamp(0)),
	       COALESCE(s.cancel_requested_at, to_timestamp(0)), COALESCE(s.shipped_at, to_timestamp(0)),
	       COALESCE(s.carrier, ''), COALESCE(s.tracking_number, ''), COALESCE(s.delivered_at, to_timestamp(0)),
	       COALESCE(trim(concat_ws(' ', u.first_name, u.last_name)), ''), COALESCE(u.email, '')
	FROM sales s
	LEFT JOIN users u ON u.id = s.user_id
`

func scanSale(row pgx.Row) (sale.Sale, error) {
	var s sale.Sale
	err := row.Scan(
		&s.ID, &s.UserID, &s.TotalCOP, &s.TotalUSD, &s.ShippingCOP, &s.ShippingUSD, &s.Fulfillment, &s.Address,
		&s.City, &s.Phone, &s.PaymentMethod, &s.Status, &s.CreatedAt,
		&s.PaymentReference, &s.BoldPaymentID, &s.PaidAt, &s.RequiresReview, &s.ReservationIDs, &s.PickupDeadline,
		&s.CancelRequestedAt, &s.ShippedAt, &s.Carrier, &s.TrackingNumber, &s.DeliveredAt,
		&s.CustomerName, &s.CustomerEmail,
	)
	// Las fechas ausentes llegan como epoch (COALESCE): se dejan en cero para
	// que IsZero() siga significando "no aplica".
	for _, t := range []*time.Time{&s.PaidAt, &s.PickupDeadline, &s.CancelRequestedAt, &s.ShippedAt, &s.DeliveredAt} {
		if t.Unix() == 0 {
			*t = time.Time{}
		}
	}
	return s, err
}

func (r *SaleRepository) loadItems(ctx context.Context, s *sale.Sale) error {
	rows, err := r.db.Query(ctx, `
		SELECT id, listing_id, card_id, card_name, language, quantity, price_cop, price_usd,
		       COALESCE(variant_name, ''), COALESCE(game_code, ''), COALESCE(game_name, ''), COALESCE(expansion_name, '')
		FROM sale_items WHERE sale_id = $1 ORDER BY id
	`, s.ID)
	if err != nil {
		return err
	}
	defer rows.Close()

	var items []sale.SaleItem
	for rows.Next() {
		var it sale.SaleItem
		if err := rows.Scan(&it.ID, &it.ListingID, &it.CardID, &it.CardName,
			&it.Language, &it.Quantity, &it.PriceCOP, &it.PriceUSD,
			&it.VariantName, &it.GameCode, &it.GameName, &it.ExpansionName); err != nil {
			return err
		}
		items = append(items, it)
	}
	s.Items = items
	if err := rows.Err(); err != nil {
		return err
	}
	return r.loadProductItems(ctx, s)
}

// FindByID devuelve una venta con sus items.
func (r *SaleRepository) FindByID(ctx context.Context, id int64) (sale.Sale, error) {
	s, err := scanSale(r.db.QueryRow(ctx, saleSelect+` WHERE s.id = $1`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sale.Sale{}, sale.ErrPaymentNotFound
		}
		return sale.Sale{}, fmt.Errorf("buscar venta %d: %w", id, err)
	}
	if err := r.loadItems(ctx, &s); err != nil {
		return sale.Sale{}, fmt.Errorf("cargar items de la venta %d: %w", s.ID, err)
	}
	if err := r.loadCreditForSale(ctx, &s); err != nil {
		return sale.Sale{}, err
	}
	return s, nil
}

// ---------------------------------------------------------------------------
// Checkouts: la intención de pago que vive mientras el cliente paga en la
// pasarela. Nada de esto es un pedido todavía.
// ---------------------------------------------------------------------------

const checkoutColumns = `reference, user_id::text, total_cop, total_usd, shipping_cop, shipping_usd,
	       fulfillment, address, city, phone, items, reservation_ids, status,
	       COALESCE(sale_id, 0), COALESCE(bold_payment_id, ''), created_at,
	       COALESCE(paid_at, to_timestamp(0)), expires_at,
	       product_items, product_reservation_ids, COALESCE(payment_pending_at, to_timestamp(0))`

const checkoutSelect = `SELECT ` + checkoutColumns + ` FROM payment_checkouts `

// scanCheckout lee un checkout en el orden de checkoutColumns y deserializa el
// snapshot de cartas (items) y de productos (product_items).
func scanCheckout(row pgx.Row) (sale.CheckoutIntent, error) {
	var (
		c            sale.CheckoutIntent
		items        []byte
		productItems []byte
	)
	err := row.Scan(
		&c.Reference, &c.UserID, &c.TotalCOP, &c.TotalUSD, &c.ShippingCOP, &c.ShippingUSD,
		&c.Fulfillment, &c.Address, &c.City, &c.Phone, &items, &c.ReservationIDs, &c.Status,
		&c.SaleID, &c.BoldPaymentID, &c.CreatedAt, &c.PaidAt, &c.ExpiresAt,
		&productItems, &c.ProductReservationIDs, &c.PaymentPendingAt,
	)
	if err != nil {
		return sale.CheckoutIntent{}, err
	}
	for _, t := range []*time.Time{&c.PaidAt, &c.PaymentPendingAt} {
		if t.Unix() == 0 {
			*t = time.Time{}
		}
	}
	if err := json.Unmarshal(items, &c.Items); err != nil {
		return sale.CheckoutIntent{}, fmt.Errorf("leer items del checkout %s: %w", c.Reference, err)
	}
	if err := json.Unmarshal(productItems, &c.ProductItems); err != nil {
		return sale.CheckoutIntent{}, fmt.Errorf("leer productos del checkout %s: %w", c.Reference, err)
	}
	return c, nil
}

// CreateCheckout guarda la intención de pago. El stock NO se toca: lo retienen
// las reservas del carrito, extendidas por el caso de uso.
func (r *SaleRepository) CreateCheckout(ctx context.Context, c sale.CheckoutIntent) (sale.CheckoutIntent, error) {
	items, err := json.Marshal(c.Items)
	if err != nil {
		return sale.CheckoutIntent{}, fmt.Errorf("serializar items del checkout: %w", err)
	}
	if len(items) == 0 {
		items = []byte("[]")
	}

	productItems, err := json.Marshal(c.ProductItems)
	if err != nil {
		return sale.CheckoutIntent{}, fmt.Errorf("serializar productos del checkout: %w", err)
	}
	if len(productItems) == 0 || string(productItems) == "null" {
		productItems = []byte("[]")
	}

	var createdAt string
	err = r.db.QueryRow(ctx, `
		INSERT INTO payment_checkouts
			(reference, user_id, fulfillment, address, city, phone,
			 total_cop, total_usd, shipping_cop, shipping_usd,
			 items, reservation_ids, status, expires_at,
			 product_items, product_reservation_ids)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, $12, $13, $14, $15::jsonb, $16)
		RETURNING created_at
	`, c.Reference, c.UserID, c.Fulfillment, c.Address, c.City, c.Phone,
		c.TotalCOP, c.TotalUSD, c.ShippingCOP, c.ShippingUSD,
		string(items), nullableIDs(c.ReservationIDs), sale.CheckoutOpen, c.ExpiresAt,
		string(productItems), nullableIDs(c.ProductReservationIDs),
	).Scan(&createdAt)
	if err != nil {
		// Índice único parcial: un solo checkout abierto por usuario. Si dos
		// "Pagar" simultáneos llegan aquí, el segundo pierde y lo resuelve el caso
		// de uso reutilizando el que ganó.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return sale.CheckoutIntent{}, sale.ErrCheckoutConflict
		}
		return sale.CheckoutIntent{}, fmt.Errorf("crear checkout: %w", err)
	}

	created := c
	created.Status = sale.CheckoutOpen
	created.CreatedAt = createdAt
	return created, nil
}

// ExtendCheckout corre la expiración de un checkout abierto. Si ya no está
// abierto (se pagó, falló o venció), no lo toca: devolver el error permite que
// el caso de uso prepare uno nuevo en vez de resucitar un pago cerrado. Es una
// sola sentencia, así que no puede pisar un cierre que ocurra a la vez.
func (r *SaleRepository) ExtendCheckout(ctx context.Context, reference string, expiresAt time.Time) (sale.CheckoutIntent, error) {
	c, err := scanCheckout(r.db.QueryRow(ctx, `
		UPDATE payment_checkouts SET expires_at = $3
		WHERE reference = $1 AND status = $2
		RETURNING `+checkoutColumns, reference, sale.CheckoutOpen, expiresAt))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sale.CheckoutIntent{}, sale.ErrPaymentNotFound
		}
		return sale.CheckoutIntent{}, fmt.Errorf("extender checkout %s: %w", reference, err)
	}
	return c, nil
}

// FindCheckoutByReference devuelve la intención de pago de esa referencia.
func (r *SaleRepository) FindCheckoutByReference(ctx context.Context, reference string) (sale.CheckoutIntent, error) {
	c, err := scanCheckout(r.db.QueryRow(ctx, checkoutSelect+` WHERE reference = $1`, reference))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sale.CheckoutIntent{}, sale.ErrPaymentNotFound
		}
		return sale.CheckoutIntent{}, fmt.Errorf("buscar checkout por referencia: %w", err)
	}
	return c, nil
}

// FindOpenCheckoutByUser devuelve el checkout abierto del usuario más reciente,
// para reutilizar su referencia si vuelve a pulsar "Pagar".
func (r *SaleRepository) FindOpenCheckoutByUser(ctx context.Context, userID string) (sale.CheckoutIntent, error) {
	c, err := scanCheckout(r.db.QueryRow(ctx, checkoutSelect+`
		WHERE user_id = $1 AND status = $2
		ORDER BY created_at DESC
		LIMIT 1
	`, userID, sale.CheckoutOpen))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sale.CheckoutIntent{}, sale.ErrPaymentNotFound
		}
		return sale.CheckoutIntent{}, fmt.Errorf("buscar checkout abierto: %w", err)
	}
	return c, nil
}

// ApprovePayment es el ÚNICO camino por el que un pago se convierte en pedido:
// inserta la venta y sus items, confirma las reservas y consume el inventario,
// todo en la misma transacción que marca el checkout como pagado. Si algo falla,
// no queda ningún pedido a medio crear.
//
// Es idempotente porque la pasarela reintenta el mismo evento hasta 5 veces: si
// el checkout ya estaba pagado se devuelve el pedido que se creó la primera vez.
func (r *SaleRepository) ApprovePayment(ctx context.Context, reference, paymentID string, paidCOP int64) (sale.Sale, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return sale.Sale{}, err
	}
	defer tx.Rollback(ctx)

	c, err := scanCheckout(tx.QueryRow(ctx, checkoutSelect+` WHERE reference = $1 FOR UPDATE`, reference))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sale.Sale{}, sale.ErrPaymentNotFound
		}
		return sale.Sale{}, fmt.Errorf("buscar checkout a aprobar: %w", err)
	}

	// Ya se había aplicado (webhook repetido o doble consulta): no se toca nada.
	if c.Status == sale.CheckoutPaid && c.SaleID != 0 {
		if err := tx.Commit(ctx); err != nil {
			return sale.Sale{}, err
		}
		return r.FindByID(ctx, c.SaleID)
	}

	// Confirmar las reservas que sostenían el pago. Si alguna ya no estaba
	// activa (el checkout expiró y el stock se liberó antes de que llegara el
	// dinero) el pedido se crea igual, pero marcado para revisión manual: el
	// cliente pagó y hay que resolverlo a mano.
	review, err := confirmReservationsTx(ctx, tx, c.ReservationIDs)
	if err != nil {
		return sale.Sale{}, err
	}
	productReview, err := confirmProductReservationsTx(ctx, tx, c.ProductReservationIDs)
	if err != nil {
		return sale.Sale{}, err
	}
	review = review || productReview
	// También se revisa a mano un pago que llega cuando el checkout ya no estaba
	// abierto (venció, falló o se reemplazó por un carrito nuevo) o cuyo monto no
	// coincide con el del pedido: el dinero entró, pero algo no es lo esperado.
	if c.Status != sale.CheckoutOpen {
		review = true
	}
	if paidCOP > 0 && paidCOP != c.TotalCOP {
		review = true
	}

	created, err := insertSale(ctx, tx, sale.Sale{
		UserID:           c.UserID,
		TotalCOP:         c.TotalCOP,
		TotalUSD:         c.TotalUSD,
		ShippingCOP:      c.ShippingCOP,
		ShippingUSD:      c.ShippingUSD,
		Fulfillment:      c.Fulfillment,
		Address:          c.Address,
		City:             c.City,
		Phone:            c.Phone,
		PaymentMethod:    sale.PaymentBold,
		Status:           sale.StatusPaid,
		PaymentReference: c.Reference,
		BoldPaymentID:    paymentID,
		PaidAt:           time.Now(),
		RequiresReview:   review,
		ReservationIDs:   c.ReservationIDs,
		Items:            c.Items,

		ProductItems:          c.ProductItems,
		ProductReservationIDs: c.ProductReservationIDs,
	}, false)
	if err != nil {
		return sale.Sale{}, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE payment_checkouts
		SET status = $2, sale_id = $3, bold_payment_id = $4, paid_at = now()
		WHERE reference = $1
	`, reference, sale.CheckoutPaid, created.ID, nullableStr(paymentID)); err != nil {
		return sale.Sale{}, fmt.Errorf("marcar checkout como pagado: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return sale.Sale{}, err
	}
	created.JustCreated = true
	return created, nil
}

// RejectPayment marca el checkout como fallido y libera sus reservas. No crea
// ninguna venta: un pago rechazado nunca fue un pedido. Es idempotente.
func (r *SaleRepository) RejectPayment(ctx context.Context, reference string) (sale.CheckoutIntent, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return sale.CheckoutIntent{}, err
	}
	defer tx.Rollback(ctx)

	var (
		c                     sale.CheckoutIntent
		reservationIDs        []int64
		productReservationIDs []int64
		status                string
	)
	err = tx.QueryRow(ctx, `
		SELECT reference, reservation_ids, product_reservation_ids, status FROM payment_checkouts WHERE reference = $1 FOR UPDATE
	`, reference).Scan(&c.Reference, &reservationIDs, &productReservationIDs, &status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sale.CheckoutIntent{}, sale.ErrPaymentNotFound
		}
		return sale.CheckoutIntent{}, fmt.Errorf("buscar checkout a rechazar: %w", err)
	}

	if status == sale.CheckoutOpen {
		if err := releaseReservationsTx(ctx, tx, reservationIDs, reservation.ReturnPaymentFailed); err != nil {
			return sale.CheckoutIntent{}, err
		}
		if err := releaseProductReservationsTx(ctx, tx, productReservationIDs, reservation.ReturnPaymentFailed); err != nil {
			return sale.CheckoutIntent{}, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE payment_checkouts SET status = $2 WHERE reference = $1
		`, reference, sale.CheckoutFailed); err != nil {
			return sale.CheckoutIntent{}, fmt.Errorf("marcar checkout como fallido: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return sale.CheckoutIntent{}, err
	}
	return r.FindCheckoutByReference(ctx, reference)
}

// CloseOpenCheckouts cierra los checkouts abiertos del usuario sin tocar sus
// reservas. Se usa cuando el carrito cambió: el checkout anterior ya no
// corresponde a lo que el cliente quiere pagar y se reemplaza por uno nuevo.
//
// NO libera las reservas a propósito. Las del checkout anterior casi siempre
// son las mismas que sostienen el carrito actual (el cliente solo cambió el
// envío, una cantidad o quitó una línea): liberarlas dejaría el checkout nuevo
// sin stock y el cliente perdería su carrito. Las líneas que el cliente quitó ya
// las libera el propio carrito, y las abandonadas vencen solas por su expiración.
func (r *SaleRepository) CloseOpenCheckouts(ctx context.Context, userID string) (int64, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE payment_checkouts SET status = $3
		WHERE user_id = $1 AND status = $2
	`, userID, sale.CheckoutOpen, sale.CheckoutExpired)
	if err != nil {
		return 0, fmt.Errorf("cerrar checkouts abiertos: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ExpireCheckouts cierra los checkouts que el cliente no pagó dentro de la
// ventana de retención, libera el stock que retenían y los devuelve.
func (r *SaleRepository) ExpireCheckouts(ctx context.Context, grace time.Duration) ([]sale.CheckoutIntent, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, checkoutSelect+`
		WHERE status = $1 AND expires_at <= now() - make_interval(secs => $2)
		FOR UPDATE
	`, sale.CheckoutOpen, grace.Seconds())
	if err != nil {
		return nil, fmt.Errorf("buscar checkouts vencidos: %w", err)
	}
	var list []sale.CheckoutIntent
	for rows.Next() {
		c, err := scanCheckout(rows)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("escanear checkout vencido: %w", err)
		}
		list = append(list, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i, c := range list {
		if err := releaseReservationsTx(ctx, tx, c.ReservationIDs, reservation.ReturnExpired); err != nil {
			return nil, err
		}
		if err := releaseProductReservationsTx(ctx, tx, c.ProductReservationIDs, reservation.ReturnExpired); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE payment_checkouts SET status = $2 WHERE reference = $1
		`, c.Reference, sale.CheckoutExpired); err != nil {
			return nil, fmt.Errorf("cerrar checkout vencido: %w", err)
		}
		list[i].Status = sale.CheckoutExpired
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return list, nil
}

// RecordPaymentEvent guarda una notificación de la pasarela. Devuelve true si
// es la primera vez que llega (el evento se procesa); false si ya se había
// registrado, que es la base de la idempotencia: Bold reintenta el mismo
// evento hasta 5 veces.
func (r *SaleRepository) RecordPaymentEvent(ctx context.Context, evt payment.Event) (bool, error) {
	payload := string(evt.Raw)
	if payload == "" {
		payload = "{}"
	}
	tag, err := r.db.Exec(ctx, `
		INSERT INTO payment_events (event_id, event_type, reference, payment_id, payload)
		VALUES ($1, $2, $3, $4, $5::jsonb)
		ON CONFLICT (event_id) DO NOTHING
	`, evt.ID, evt.Type, evt.Reference, evt.PaymentID, payload)
	if err != nil {
		return false, fmt.Errorf("registrar evento de pago: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// ForgetPaymentEvent borra el registro de una notificación que no se pudo
// procesar, para que el reintento de la pasarela no se descarte como duplicado.
func (r *SaleRepository) ForgetPaymentEvent(ctx context.Context, eventID string) error {
	if _, err := r.db.Exec(ctx, `DELETE FROM payment_events WHERE event_id = $1`, eventID); err != nil {
		return fmt.Errorf("olvidar evento de pago %s: %w", eventID, err)
	}
	return nil
}

// FlagSaleForReview marca para revisión manual la venta de esa referencia.
func (r *SaleRepository) FlagSaleForReview(ctx context.Context, reference string) error {
	if _, err := r.db.Exec(ctx, `
		UPDATE sales SET requires_review = true WHERE payment_reference = $1
	`, reference); err != nil {
		return fmt.Errorf("marcar venta %s para revisión: %w", reference, err)
	}
	return nil
}

// unsettledWindow es cuánto tiempo después de cerrarse se sigue preguntando a la
// pasarela por un checkout no pagado: cubre los pagos que quedan pendientes (PSE)
// y los reintentos tras un rechazo, sin consultar para siempre.
const unsettledWindow = "2 hours"

// ListUnsettledCheckouts devuelve las referencias que pueden haberse cobrado sin
// que el backend lo sepa: abiertos ya vencidos (esperando el margen de gracia),
// abiertos con el pago en proceso (PSE: se consultan hasta que se resuelvan) y
// cerrados por vencimiento o rechazo recientemente.
func (r *SaleRepository) ListUnsettledCheckouts(ctx context.Context, limit int) ([]string, error) {
	rows, err := r.db.Query(ctx, `
		SELECT reference
		FROM payment_checkouts
		WHERE (status = $1 AND (expires_at <= now() OR payment_pending_at IS NOT NULL))
		   OR (status IN ($2, $3) AND expires_at > now() - interval '`+unsettledWindow+`')
		ORDER BY expires_at DESC
		LIMIT $4
	`, sale.CheckoutOpen, sale.CheckoutExpired, sale.CheckoutFailed, limit)
	if err != nil {
		return nil, fmt.Errorf("listar pagos sin resolver: %w", err)
	}
	defer rows.Close()

	var refs []string
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			return nil, fmt.Errorf("escanear pago sin resolver: %w", err)
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

// reserveToLogSQL marca las reservas con un estado terminal y deja el registro
// en el historial de reservas, en una sola sentencia (CTE).
const reserveToLogSQL = `
	WITH updated AS (
		UPDATE cart_reservations SET status = $2
		WHERE id = ANY($1) AND status = 'active'
		RETURNING id, listing_id, quantity, user_id
	)
	INSERT INTO cart_reservation_logs
		(reservation_id, user_id, customer_name, customer_email,
		 card_id, card_name, variant_name, language, quantity,
		 price_usd, price_cop, status, return_reason)
	SELECT u.id, u.user_id,
	       TRIM(COALESCE(usr.first_name,'') || ' ' || COALESCE(usr.last_name,'')), COALESCE(usr.email,''),
	       c.id, c.name, cv.variant_name, il.language, u.quantity,
	       il.price_usd, il.price_cop, $3, NULLIF($4, '')
	FROM updated u
	JOIN inventory_listings il ON il.id = u.listing_id
	JOIN card_variants cv ON cv.id = il.variant_id
	JOIN cards c ON c.id = cv.card_id
	JOIN users usr ON usr.id = u.user_id
`

// confirmReservationsTx confirma las reservas que sostenían el pago y registra
// el historial 'sold'. Devuelve true si alguna ya no estaba activa (el stock
// pudo haberse liberado), lo que obliga a revisar el pedido a mano.
func confirmReservationsTx(ctx context.Context, tx pgx.Tx, ids []int64) (bool, error) {
	if len(ids) == 0 {
		return false, nil
	}
	var active int
	if err := tx.QueryRow(ctx, `
		SELECT count(*)::int FROM cart_reservations WHERE id = ANY($1) AND status = 'active'
	`, ids).Scan(&active); err != nil {
		return false, fmt.Errorf("contar reservas activas: %w", err)
	}
	if _, err := tx.Exec(ctx, reserveToLogSQL, ids, "confirmed", "sold", ""); err != nil {
		return false, fmt.Errorf("confirmar reservas del pago: %w", err)
	}
	return active < len(ids), nil
}

// releaseReservationsTx libera las reservas (el stock vuelve al inventario) y
// registra el historial 'returned' con su motivo. Es idempotente: solo toca las
// 'active'.
func releaseReservationsTx(ctx context.Context, tx pgx.Tx, ids []int64, reason string) error {
	if len(ids) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, reserveToLogSQL, ids, "released", "returned", reason); err != nil {
		return fmt.Errorf("liberar reservas: %w", err)
	}
	return nil
}

func (r *SaleRepository) ListByUser(ctx context.Context, userID string, limit, offset int) ([]sale.Sale, error) {
	rows, err := r.db.Query(ctx, saleSelect+` WHERE s.user_id = $1 ORDER BY s.created_at DESC LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("listar ventas del usuario: %w", err)
	}
	defer rows.Close()

	var out []sale.Sale
	for rows.Next() {
		s, err := scanSale(rows)
		if err != nil {
			return nil, fmt.Errorf("escanear venta: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Carga items de cada venta.
	for i := range out {
		if err := r.loadItems(ctx, &out[i]); err != nil {
			return nil, fmt.Errorf("cargar items de venta %d: %w", out[i].ID, err)
		}
	}
	// El crédito incluye notas internas y el admin aprobador; no se expone en
	// el historial del cliente. El estado `credit` sí queda visible en la venta.
	return out, nil
}

// Stats agrega las ventas en buckets (día o semana ISO) desde `since`.
func (r *SaleRepository) Stats(ctx context.Context, period string, since time.Time) ([]sale.StatBucket, error) {
	trunc := "day"
	if period == "week" {
		trunc = "week"
	}

	rows, err := r.db.Query(ctx, `
		SELECT
			to_char(date_trunc($1, created_at), 'YYYY-MM-DD') AS start,
			count(*)::int AS orders,
			COALESCE(sum(total_cop), 0)::bigint AS total_cop,
			COALESCE(sum(total_usd), 0)::numeric AS total_usd
		FROM sales
		WHERE created_at >= $2 AND status = 'paid'
		GROUP BY date_trunc($1, created_at)
		ORDER BY date_trunc($1, created_at)
	`, trunc, since)
	if err != nil {
		return nil, fmt.Errorf("agregar ventas: %w", err)
	}
	defer rows.Close()

	var out []sale.StatBucket
	for rows.Next() {
		var b sale.StatBucket
		if err := rows.Scan(&b.Start, &b.Orders, &b.TotalCOP, &b.TotalUSD); err != nil {
			return nil, fmt.Errorf("escanear bucket de ventas: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Pedidos de recogida en tienda: se pagan en el local y vencen a los 2 días
// hábiles. Mientras están pendientes tienen el stock descontado; al cancelarse
// (a mano o por vencimiento) vuelve al inventario.
// ---------------------------------------------------------------------------

// lockPendingPickupTx bloquea el pedido y comprueba que siga pendiente de
// recogida. Devuelve ErrPaymentNotFound si no existe y ErrNotPendingPickup si ya
// se pagó o canceló, de modo que pagar y cancelar (o el barrido por vencimiento)
// no puedan pisarse.
func lockPendingPickupTx(ctx context.Context, tx pgx.Tx, id int64) error {
	var status string
	err := tx.QueryRow(ctx, `SELECT status FROM sales WHERE id = $1 FOR UPDATE`, id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return sale.ErrPaymentNotFound
	}
	if err != nil {
		return fmt.Errorf("bloquear pedido %d: %w", id, err)
	}
	if status != sale.StatusPendingPickup {
		return sale.ErrNotPendingPickup
	}
	return nil
}

// cancelPickupTx marca el pedido como cancelado y devuelve sus cartas al
// inventario. Un listing que la venta dejó en 0 unidades (y por eso 'inactive')
// vuelve a estar activo; uno desactivado por otra razón conserva su estado.
func cancelPickupTx(ctx context.Context, tx pgx.Tx, id int64, reason string) error {
	rows, err := tx.Query(ctx, `SELECT listing_id, quantity FROM sale_items WHERE sale_id = $1`, id)
	if err != nil {
		return fmt.Errorf("leer items del pedido %d: %w", id, err)
	}
	type line struct {
		listingID int64
		quantity  int
	}
	var lines []line
	for rows.Next() {
		var listingID *int64
		var qty int
		if err := rows.Scan(&listingID, &qty); err != nil {
			rows.Close()
			return fmt.Errorf("escanear item del pedido %d: %w", id, err)
		}
		if listingID != nil { // el listing se eliminó: no hay a dónde devolver
			lines = append(lines, line{*listingID, qty})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	slices.SortFunc(lines, func(a, b line) int { return int(a.listingID - b.listingID) })

	for _, l := range lines {
		if _, err := tx.Exec(ctx, `
			UPDATE inventory_listings
			SET status = CASE WHEN quantity = 0 AND status = 'inactive' THEN 'active' ELSE status END,
			    quantity = quantity + $2,
			    updated_at = now()
			WHERE id = $1
		`, l.listingID, l.quantity); err != nil {
			return fmt.Errorf("devolver stock del listing %d: %w", l.listingID, err)
		}
	}
	if err := restoreProductStockTx(ctx, tx, id); err != nil {
		return err
	}
	// El historial de cada reserva deja de estar 'reservada' y pasa a
	// 'returned': la recogida no se completó y el stock volvió al inventario.
	if err := logSaleCardReservationsTx(ctx, tx, id, reservation.LogReturned, reason); err != nil {
		return err
	}
	if err := logSaleProductReservationsTx(ctx, tx, id, reservation.LogReturned, reason); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE sales SET status = $2 WHERE id = $1`, id, sale.StatusCancelled); err != nil {
		return fmt.Errorf("cancelar pedido %d: %w", id, err)
	}
	// Si el pedido se había entregado a crédito, la deuda deja de ser exigible.
	// Se conserva paid_cop como registro de lo que el cliente ya había abonado.
	if _, err := tx.Exec(ctx, `
		UPDATE store_credits
		   SET status = $2, balance_cop = 0, updated_at = now()
		 WHERE sale_id = $1 AND status = $3
	`, id, sale.CreditCancelled, sale.CreditOpen); err != nil {
		return fmt.Errorf("cancelar crédito del pedido %d: %w", id, err)
	}
	return nil
}

// logSaleCardReservationsTx agrega un evento al historial de las reservas de
// carta que sostienen un pedido. Se usa al cobrar (sold) y al cancelar
// (returned), para no confundir una recogida pendiente con una venta.
func logSaleCardReservationsTx(ctx context.Context, tx pgx.Tx, saleID int64, status, reason string) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO cart_reservation_logs
			(reservation_id, user_id, customer_name, customer_email,
			 card_id, card_name, variant_name, language, quantity,
			 price_usd, price_cop, status, return_reason)
		SELECT cr.id, cr.user_id,
		       TRIM(COALESCE(u.first_name, '') || ' ' || COALESCE(u.last_name, '')), COALESCE(u.email, ''),
		       c.id, c.name, cv.variant_name, il.language, cr.quantity,
		       il.price_usd, il.price_cop, $2, NULLIF($3, '')
		FROM sales s
		JOIN cart_reservations cr ON cr.id = ANY(s.reservation_ids)
		JOIN inventory_listings il ON il.id = cr.listing_id
		JOIN card_variants cv ON cv.id = il.variant_id
		JOIN cards c ON c.id = cv.card_id
		JOIN users u ON u.id = cr.user_id
		WHERE s.id = $1
	`, saleID, status, reason); err != nil {
		return fmt.Errorf("registrar historial de cartas del pedido %d: %w", saleID, err)
	}
	return nil
}

// MarkPickupPaid registra que el cliente pagó en la tienda.
func (r *SaleRepository) MarkPickupPaid(ctx context.Context, id int64) (sale.Sale, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return sale.Sale{}, err
	}
	defer tx.Rollback(ctx)

	if err := lockPendingPickupTx(ctx, tx, id); err != nil {
		return sale.Sale{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE sales SET status = $2, paid_at = now(), delivered_at = now() WHERE id = $1`, id, sale.StatusPaid); err != nil {
		return sale.Sale{}, fmt.Errorf("marcar pedido %d como pagado: %w", id, err)
	}
	// Recién ahora la reserva se convierte en venta.
	if err := logSaleCardReservationsTx(ctx, tx, id, reservation.LogSold, ""); err != nil {
		return sale.Sale{}, err
	}
	if err := logSaleProductReservationsTx(ctx, tx, id, reservation.LogSold, ""); err != nil {
		return sale.Sale{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sale.Sale{}, err
	}
	return r.FindByID(ctx, id)
}

// ExtendPickup corre el plazo de una recogida pendiente. El pedido sigue en
// 'pending_pickup' y conserva el stock apartado.
func (r *SaleRepository) ExtendPickup(ctx context.Context, id int64, days int) (sale.Sale, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return sale.Sale{}, err
	}
	defer tx.Rollback(ctx)

	var deadline time.Time
	err = tx.QueryRow(ctx, `
		SELECT pickup_deadline FROM sales WHERE id = $1 AND status = $2 FOR UPDATE
	`, id, sale.StatusPendingPickup).Scan(&deadline)
	if errors.Is(err, pgx.ErrNoRows) {
		var status string
		if qerr := tx.QueryRow(ctx, `SELECT status FROM sales WHERE id = $1`, id).Scan(&status); errors.Is(qerr, pgx.ErrNoRows) {
			return sale.Sale{}, sale.ErrPaymentNotFound
		}
		return sale.Sale{}, sale.ErrNotPendingPickup
	}
	if err != nil {
		return sale.Sale{}, fmt.Errorf("buscar pedido %d para extender: %w", id, err)
	}

	newDeadline := sale.ExtendPickupDeadline(deadline, time.Now(), days)
	if _, err := tx.Exec(ctx, `
		UPDATE sales SET pickup_deadline = $2, pickup_reminder_sent_at = NULL WHERE id = $1
	`, id, newDeadline); err != nil {
		return sale.Sale{}, fmt.Errorf("extender pedido %d: %w", id, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return sale.Sale{}, err
	}
	return r.FindByID(ctx, id)
}

// CancelPickup cancela un pedido pendiente y devuelve su stock.
func (r *SaleRepository) CancelPickup(ctx context.Context, id int64) (sale.Sale, error) {
	return r.cancelPickup(ctx, id, reservation.ReturnCancelled)
}

func (r *SaleRepository) cancelPickup(ctx context.Context, id int64, reason string) (sale.Sale, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return sale.Sale{}, err
	}
	defer tx.Rollback(ctx)

	if err := lockPendingPickupTx(ctx, tx, id); err != nil {
		return sale.Sale{}, err
	}
	if err := cancelPickupTx(ctx, tx, id, reason); err != nil {
		return sale.Sale{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sale.Sale{}, err
	}
	return r.FindByID(ctx, id)
}

// ExpirePickups cancela los pedidos pendientes cuyo plazo ya venció y devuelve
// los que canceló, para avisar a sus clientes. Cada pedido va en su propia
// transacción: uno que falle no deja sin cancelar a los demás.
func (r *SaleRepository) ExpirePickups(ctx context.Context, now time.Time) ([]sale.Sale, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id FROM sales
		WHERE status = $1 AND pickup_deadline IS NOT NULL AND pickup_deadline <= $2
		ORDER BY id
	`, sale.StatusPendingPickup, now)
	if err != nil {
		return nil, fmt.Errorf("buscar pedidos de recogida vencidos: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("escanear pedido vencido: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var cancelled []sale.Sale
	var firstErr error
	for _, id := range ids {
		s, err := r.cancelPickup(ctx, id, reservation.ReturnPickupExpired)
		if errors.Is(err, sale.ErrNotPendingPickup) {
			continue // lo pagaron o cancelaron justo ahora
		}
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("cancelar pedido vencido %d: %w", id, err)
			}
			continue
		}
		cancelled = append(cancelled, s)
	}
	return cancelled, firstErr
}

// ---------------------------------------------------------------------------
// Pagos en proceso, cancelación solicitada, recordatorios y despacho.
// ---------------------------------------------------------------------------

// MarkCheckoutPending registra que el pago del checkout quedó en proceso en la
// pasarela y alarga su retención hasta `window` desde que se creó el checkout
// (sus reservas, `grace` más). Solo actúa la primera vez, con el checkout
// abierto y dentro de la ventana; devuelve true en ese caso (hay que avisar al
// cliente).
func (r *SaleRepository) MarkCheckoutPending(ctx context.Context, reference string, window, grace time.Duration) (sale.CheckoutIntent, bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return sale.CheckoutIntent{}, false, err
	}
	defer tx.Rollback(ctx)

	c, err := scanCheckout(tx.QueryRow(ctx, checkoutSelect+` WHERE reference = $1 FOR UPDATE`, reference))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sale.CheckoutIntent{}, false, sale.ErrPaymentNotFound
		}
		return sale.CheckoutIntent{}, false, fmt.Errorf("buscar checkout en proceso: %w", err)
	}
	if c.Status != sale.CheckoutOpen || !c.PaymentPendingAt.IsZero() {
		return c, false, tx.Commit(ctx)
	}

	var stockUntil time.Time
	err = tx.QueryRow(ctx, `
		UPDATE payment_checkouts
		SET payment_pending_at = now(),
		    expires_at = GREATEST(expires_at, created_at + make_interval(secs => $2))
		WHERE reference = $1 AND created_at + make_interval(secs => $2) > now()
		RETURNING payment_pending_at, expires_at, expires_at + make_interval(secs => $3)
	`, reference, window.Seconds(), grace.Seconds()).Scan(&c.PaymentPendingAt, &c.ExpiresAt, &stockUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		// La ventana ya pasó: el pago se resuelve como tardío si llega a entrar.
		return c, false, tx.Commit(ctx)
	}
	if err != nil {
		return sale.CheckoutIntent{}, false, fmt.Errorf("marcar pago en proceso: %w", err)
	}
	if len(c.ReservationIDs) > 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE cart_reservations SET expires_at = GREATEST(expires_at, $2)
			WHERE id = ANY($1) AND status = 'active'
		`, c.ReservationIDs, stockUntil); err != nil {
			return sale.CheckoutIntent{}, false, fmt.Errorf("retener reservas del pago en proceso: %w", err)
		}
	}
	if len(c.ProductReservationIDs) > 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE product_reservations SET expires_at = GREATEST(expires_at, $2)
			WHERE id = ANY($1) AND status = 'active'
		`, c.ProductReservationIDs, stockUntil); err != nil {
			return sale.CheckoutIntent{}, false, fmt.Errorf("retener productos del pago en proceso: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return sale.CheckoutIntent{}, false, err
	}
	return c, true, nil
}

// RequestPickupCancel deja constancia de que el cliente pidió cancelar su pedido
// de recogida. No toca el stock: el pedido sigue apartado hasta que el staff
// decida.
func (r *SaleRepository) RequestPickupCancel(ctx context.Context, id int64, userID string) (sale.Sale, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return sale.Sale{}, err
	}
	defer tx.Rollback(ctx)

	var (
		owner       string
		status      string
		requestedAt *time.Time
	)
	err = tx.QueryRow(ctx, `SELECT user_id::text, status, cancel_requested_at FROM sales WHERE id = $1 FOR UPDATE`, id).
		Scan(&owner, &status, &requestedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return sale.Sale{}, sale.ErrPaymentNotFound
	}
	if err != nil {
		return sale.Sale{}, fmt.Errorf("bloquear pedido %d: %w", id, err)
	}
	switch {
	case owner != userID:
		return sale.Sale{}, sale.ErrForbidden
	case status != sale.StatusPendingPickup:
		return sale.Sale{}, sale.ErrNotPendingPickup
	case requestedAt != nil:
		return sale.Sale{}, sale.ErrCancelAlreadyRequested
	}
	if _, err := tx.Exec(ctx, `UPDATE sales SET cancel_requested_at = now() WHERE id = $1`, id); err != nil {
		return sale.Sale{}, fmt.Errorf("solicitar cancelación del pedido %d: %w", id, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return sale.Sale{}, err
	}
	return r.FindByID(ctx, id)
}

// RejectPickupCancel descarta la solicitud de cancelación: el pedido sigue
// apartado con su plazo original.
func (r *SaleRepository) RejectPickupCancel(ctx context.Context, id int64) (sale.Sale, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return sale.Sale{}, err
	}
	defer tx.Rollback(ctx)

	if err := lockPendingPickupTx(ctx, tx, id); err != nil {
		return sale.Sale{}, err
	}
	tag, err := tx.Exec(ctx, `UPDATE sales SET cancel_requested_at = NULL WHERE id = $1 AND cancel_requested_at IS NOT NULL`, id)
	if err != nil {
		return sale.Sale{}, fmt.Errorf("rechazar cancelación del pedido %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return sale.Sale{}, sale.ErrNoCancelRequest
	}
	if err := tx.Commit(ctx); err != nil {
		return sale.Sale{}, err
	}
	return r.FindByID(ctx, id)
}

// reminderLead es cuánto antes del plazo se recuerda la recogida: el plazo
// vence a las 23:59:59, así que 39 horas antes son las 9 de la mañana del día
// anterior.
const reminderLead = "39 hours"

// ClaimPickupReminders marca y devuelve los pedidos de recogida a los que toca
// recordarles que el plazo vence mañana. Marcar y devolver en la misma sentencia
// garantiza que cada recordatorio salga una sola vez aunque el bucle se solape.
// Se omiten los que tienen una cancelación solicitada.
func (r *SaleRepository) ClaimPickupReminders(ctx context.Context, now time.Time) ([]sale.Sale, error) {
	rows, err := r.db.Query(ctx, `
		UPDATE sales SET pickup_reminder_sent_at = now()
		WHERE status = $1
		  AND pickup_reminder_sent_at IS NULL
		  AND cancel_requested_at IS NULL
		  AND pickup_deadline > $2
		  AND pickup_deadline - interval '`+reminderLead+`' <= $2
		RETURNING id
	`, sale.StatusPendingPickup, now)
	if err != nil {
		return nil, fmt.Errorf("marcar recordatorios de recogida: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("escanear recordatorio de recogida: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]sale.Sale, 0, len(ids))
	for _, id := range ids {
		s, err := r.FindByID(ctx, id)
		if err != nil {
			return out, err
		}
		out = append(out, s)
	}
	return out, nil
}

// MarkShipped registra el despacho de un pedido pagado con envío a domicilio.
// Un pedido marcado para revisión no se despacha: primero hay que resolverlo.
func (r *SaleRepository) MarkShipped(ctx context.Context, id int64, carrier, tracking string) (sale.Sale, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE sales SET shipped_at = now(), carrier = $2, tracking_number = $3
		WHERE id = $1 AND status = $4 AND fulfillment = $5 AND NOT requires_review AND shipped_at IS NULL
	`, id, carrier, tracking, sale.StatusPaid, sale.FulfillmentShipping)
	if err != nil {
		return sale.Sale{}, fmt.Errorf("marcar pedido %d como enviado: %w", id, err)
	}
	return r.afterDispatchUpdate(ctx, id, tag.RowsAffected())
}

// MarkDelivered registra que el pedido enviado llegó al cliente.
func (r *SaleRepository) MarkDelivered(ctx context.Context, id int64) (sale.Sale, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE sales SET delivered_at = now()
		WHERE id = $1 AND status = $2 AND fulfillment = $3 AND shipped_at IS NOT NULL AND delivered_at IS NULL
	`, id, sale.StatusPaid, sale.FulfillmentShipping)
	if err != nil {
		return sale.Sale{}, fmt.Errorf("marcar pedido %d como entregado: %w", id, err)
	}
	return r.afterDispatchUpdate(ctx, id, tag.RowsAffected())
}

// afterDispatchUpdate devuelve el pedido actualizado o explica por qué no se
// tocó: no existe (ErrPaymentNotFound) o no estaba en el estado correcto.
func (r *SaleRepository) afterDispatchUpdate(ctx context.Context, id int64, affected int64) (sale.Sale, error) {
	s, err := r.FindByID(ctx, id)
	if err != nil {
		return sale.Sale{}, err
	}
	if affected == 0 {
		return sale.Sale{}, sale.ErrNotShippable
	}
	return s, nil
}

// ConfirmReview quita la marca de revisión de un pedido: el staff comprobó que
// el pago y el inventario están bien.
func (r *SaleRepository) ConfirmReview(ctx context.Context, id int64) (sale.Sale, error) {
	tag, err := r.db.Exec(ctx, `UPDATE sales SET requires_review = false WHERE id = $1 AND requires_review`, id)
	if err != nil {
		return sale.Sale{}, fmt.Errorf("confirmar revisión del pedido %d: %w", id, err)
	}
	s, err := r.FindByID(ctx, id)
	if err != nil {
		return sale.Sale{}, err
	}
	if tag.RowsAffected() == 0 {
		return sale.Sale{}, sale.ErrNotInReview
	}
	return s, nil
}
