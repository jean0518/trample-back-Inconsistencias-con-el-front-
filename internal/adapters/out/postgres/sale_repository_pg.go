package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"trample-back/internal/domain/payment"
	"trample-back/internal/domain/sale"

	"github.com/jackc/pgx/v5"
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

	created, err := insertSale(ctx, tx, s)
	if err != nil {
		return sale.Sale{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sale.Sale{}, err
	}
	return created, nil
}

// insertSale persiste la venta y sus items, y consume el inventario. Solo se
// llama con ventas ya pagadas: efectivo, transferencia o un pago de la pasarela
// que el backend acaba de aprobar. Un pago en curso no pasa por aquí.
func insertSale(ctx context.Context, tx pgx.Tx, s sale.Sale) (sale.Sale, error) {
	created := s
	err := tx.QueryRow(ctx, `
		INSERT INTO sales (user_id, total_cop, total_usd, shipping_cop, shipping_usd, fulfillment, address, city, phone, payment_method, status, payment_reference, bold_payment_id, paid_at, requires_review, reservation_ids)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		RETURNING id, created_at
	`, s.UserID, s.TotalCOP, s.TotalUSD, s.ShippingCOP, s.ShippingUSD, s.Fulfillment, s.Address, s.City, s.Phone, s.PaymentMethod, s.Status,
		nullableStr(s.PaymentReference), nullableStr(s.BoldPaymentID), nullableTime(s.PaidAt), s.RequiresReview, nullableIDs(s.ReservationIDs),
	).Scan(&created.ID, &created.CreatedAt)
	if err != nil {
		return sale.Sale{}, fmt.Errorf("crear venta: %w", err)
	}

	created.Items = make([]sale.SaleItem, 0, len(s.Items))
	for _, it := range s.Items {
		var item sale.SaleItem
		err := tx.QueryRow(ctx, `
			INSERT INTO sale_items (sale_id, listing_id, card_id, card_name, language, quantity, price_cop, price_usd)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			RETURNING listing_id, card_id, card_name, language, quantity, price_cop, price_usd
		`, created.ID, it.ListingID, it.CardID, it.CardName, it.Language, it.Quantity, it.PriceCOP, it.PriceUSD).Scan(
			&item.ListingID, &item.CardID, &item.CardName,
			&item.Language, &item.Quantity, &item.PriceCOP, &item.PriceUSD,
		)
		if err != nil {
			return sale.Sale{}, fmt.Errorf("crear item de venta: %w", err)
		}
		created.Items = append(created.Items, item)
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

const saleSelect = `
	SELECT id, user_id, total_cop, total_usd, shipping_cop, shipping_usd, fulfillment, address, city, phone, payment_method, status, created_at,
	       COALESCE(payment_reference, ''), COALESCE(bold_payment_id, ''), COALESCE(paid_at, to_timestamp(0)),
	       requires_review, reservation_ids
	FROM sales
`

func scanSale(row pgx.Row) (sale.Sale, error) {
	var s sale.Sale
	err := row.Scan(
		&s.ID, &s.UserID, &s.TotalCOP, &s.TotalUSD, &s.ShippingCOP, &s.ShippingUSD, &s.Fulfillment, &s.Address,
		&s.City, &s.Phone, &s.PaymentMethod, &s.Status, &s.CreatedAt,
		&s.PaymentReference, &s.BoldPaymentID, &s.PaidAt, &s.RequiresReview, &s.ReservationIDs,
	)
	return s, err
}

func (r *SaleRepository) loadItems(ctx context.Context, s *sale.Sale) error {
	rows, err := r.db.Query(ctx, `
		SELECT listing_id, card_id, card_name, language, quantity, price_cop, price_usd
		FROM sale_items WHERE sale_id = $1 ORDER BY id
	`, s.ID)
	if err != nil {
		return err
	}
	defer rows.Close()

	var items []sale.SaleItem
	for rows.Next() {
		var it sale.SaleItem
		if err := rows.Scan(&it.ListingID, &it.CardID, &it.CardName,
			&it.Language, &it.Quantity, &it.PriceCOP, &it.PriceUSD); err != nil {
			return err
		}
		items = append(items, it)
	}
	s.Items = items
	return rows.Err()
}

// FindByID devuelve una venta con sus items.
func (r *SaleRepository) FindByID(ctx context.Context, id int64) (sale.Sale, error) {
	s, err := scanSale(r.db.QueryRow(ctx, saleSelect+` WHERE id = $1`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sale.Sale{}, sale.ErrPaymentNotFound
		}
		return sale.Sale{}, fmt.Errorf("buscar venta %d: %w", id, err)
	}
	if err := r.loadItems(ctx, &s); err != nil {
		return sale.Sale{}, fmt.Errorf("cargar items de la venta %d: %w", s.ID, err)
	}
	return s, nil
}

// ---------------------------------------------------------------------------
// Checkouts: la intención de pago que vive mientras el cliente paga en la
// pasarela. Nada de esto es un pedido todavía.
// ---------------------------------------------------------------------------

const checkoutSelect = `
	SELECT reference, user_id::text, total_cop, total_usd, shipping_cop, shipping_usd,
	       fulfillment, address, city, phone, items, reservation_ids, status,
	       COALESCE(sale_id, 0), bold_payment_id, created_at,
	       COALESCE(paid_at, to_timestamp(0)), expires_at
	FROM payment_checkouts
`

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

	var createdAt string
	err = r.db.QueryRow(ctx, `
		INSERT INTO payment_checkouts
			(reference, user_id, fulfillment, address, city, phone,
			 total_cop, total_usd, shipping_cop, shipping_usd,
			 items, reservation_ids, status, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, $12, $13, $14)
		RETURNING created_at
	`, c.Reference, c.UserID, c.Fulfillment, c.Address, c.City, c.Phone,
		c.TotalCOP, c.TotalUSD, c.ShippingCOP, c.ShippingUSD,
		string(items), nullableIDs(c.ReservationIDs), sale.CheckoutOpen, c.ExpiresAt,
	).Scan(&createdAt)
	if err != nil {
		return sale.CheckoutIntent{}, fmt.Errorf("crear checkout: %w", err)
	}

	created := c
	created.Status = sale.CheckoutOpen
	created.CreatedAt = createdAt
	return created, nil
}

// ExtendCheckout corre la expiración de un checkout abierto. Si ya no está
// abierto (se pagó, falló o venció), no lo toca: devolver el error permite que
// el caso de uso preparo uno nuevo en vez de resurrectir un pago cerrado.
func (r *SaleRepository) ExtendCheckout(ctx context.Context, reference string, expiresAt time.Time) (sale.CheckoutIntent, error) {
	row := r.db.QueryRow(ctx,
		checkoutSelect+` WHERE reference = $1 AND status = $2 FOR UPDATE`,
		reference, sale.CheckoutOpen)

	var (
		c     sale.CheckoutIntent
		items []byte
	)
	err := row.Scan(
		&c.Reference, &c.UserID, &c.TotalCOP, &c.TotalUSD, &c.ShippingCOP, &c.ShippingUSD,
		&c.Fulfillment, &c.Address, &c.City, &c.Phone, &items, &c.ReservationIDs, &c.Status,
		&c.SaleID, &c.BoldPaymentID, &c.CreatedAt, &c.PaidAt, &c.ExpiresAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sale.CheckoutIntent{}, sale.ErrPaymentNotFound
		}
		return sale.CheckoutIntent{}, fmt.Errorf("extender checkout %s: %w", reference, err)
	}

	if _, err := r.db.Exec(ctx,
		`UPDATE payment_checkouts SET expires_at = $2 WHERE reference = $1`, reference, expiresAt); err != nil {
		return sale.CheckoutIntent{}, fmt.Errorf("extender checkout %s: %w", reference, err)
	}
	c.ExpiresAt = expiresAt
	return c, nil
}

// FindCheckoutByReference devuelve la intención de pago de esa referencia.
func (r *SaleRepository) FindCheckoutByReference(ctx context.Context, reference string) (sale.CheckoutIntent, error) {
	var (
		c     sale.CheckoutIntent
		items []byte
	)
	err := r.db.QueryRow(ctx, checkoutSelect+` WHERE reference = $1`, reference).Scan(
		&c.Reference, &c.UserID, &c.TotalCOP, &c.TotalUSD, &c.ShippingCOP, &c.ShippingUSD,
		&c.Fulfillment, &c.Address, &c.City, &c.Phone, &items, &c.ReservationIDs, &c.Status,
		&c.SaleID, &c.BoldPaymentID, &c.CreatedAt, &c.PaidAt, &c.ExpiresAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sale.CheckoutIntent{}, sale.ErrPaymentNotFound
		}
		return sale.CheckoutIntent{}, fmt.Errorf("buscar checkout por referencia: %w", err)
	}
	if err := json.Unmarshal(items, &c.Items); err != nil {
		return sale.CheckoutIntent{}, fmt.Errorf("leer items del checkout %s: %w", reference, err)
	}
	return c, nil
}

// FindOpenCheckoutByUser devuelve el checkout abierto del usuario más reciente,
// para reutilizar su referencia si vuelve a pulsar "Pagar".
func (r *SaleRepository) FindOpenCheckoutByUser(ctx context.Context, userID string) (sale.CheckoutIntent, error) {
	var (
		c     sale.CheckoutIntent
		items []byte
	)
	err := r.db.QueryRow(ctx, checkoutSelect+`
		WHERE user_id = $1 AND status = $2
		ORDER BY created_at DESC
		LIMIT 1
	`, userID, sale.CheckoutOpen).Scan(
		&c.Reference, &c.UserID, &c.TotalCOP, &c.TotalUSD, &c.ShippingCOP, &c.ShippingUSD,
		&c.Fulfillment, &c.Address, &c.City, &c.Phone, &items, &c.ReservationIDs, &c.Status,
		&c.SaleID, &c.BoldPaymentID, &c.CreatedAt, &c.PaidAt, &c.ExpiresAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sale.CheckoutIntent{}, sale.ErrPaymentNotFound
		}
		return sale.CheckoutIntent{}, fmt.Errorf("buscar checkout abierto: %w", err)
	}
	if err := json.Unmarshal(items, &c.Items); err != nil {
		return sale.CheckoutIntent{}, fmt.Errorf("leer items del checkout %s: %w", c.Reference, err)
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
func (r *SaleRepository) ApprovePayment(ctx context.Context, reference, paymentID string) (sale.Sale, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return sale.Sale{}, err
	}
	defer tx.Rollback(ctx)

	var (
		c     sale.CheckoutIntent
		items []byte
	)
	err = tx.QueryRow(ctx, checkoutSelect+` WHERE reference = $1 FOR UPDATE`, reference).Scan(
		&c.Reference, &c.UserID, &c.TotalCOP, &c.TotalUSD, &c.ShippingCOP, &c.ShippingUSD,
		&c.Fulfillment, &c.Address, &c.City, &c.Phone, &items, &c.ReservationIDs, &c.Status,
		&c.SaleID, &c.BoldPaymentID, &c.CreatedAt, &c.PaidAt, &c.ExpiresAt,
	)
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

	if err := json.Unmarshal(items, &c.Items); err != nil {
		return sale.Sale{}, fmt.Errorf("leer items del checkout %s: %w", reference, err)
	}

	// Confirmar las reservas que sostenían el pago. Si alguna ya no estaba
	// activa (el checkout expiró y el stock se liberó antes de que llegara el
	// dinero) el pedido se crea igual, pero marcado para revisión manual: el
	// cliente pagó y hay que resolverlo a mano.
	review, err := confirmReservationsTx(ctx, tx, c.ReservationIDs)
	if err != nil {
		return sale.Sale{}, err
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
	})
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
		c              sale.CheckoutIntent
		reservationIDs []int64
		status         string
	)
	err = tx.QueryRow(ctx, `
		SELECT reference, reservation_ids, status FROM payment_checkouts WHERE reference = $1 FOR UPDATE
	`, reference).Scan(&c.Reference, &reservationIDs, &status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sale.CheckoutIntent{}, sale.ErrPaymentNotFound
		}
		return sale.CheckoutIntent{}, fmt.Errorf("buscar checkout a rechazar: %w", err)
	}

	if status == sale.CheckoutOpen {
		if err := releaseReservationsTx(ctx, tx, reservationIDs); err != nil {
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

// CloseOpenCheckouts cierra los checkouts abiertos del usuario y devuelve el
// stock que retinían. Se usa cuando el carrito cambió: el checkout anterior ya
// no corresponde a lo que el cliente quiere pagar.
func (r *SaleRepository) CloseOpenCheckouts(ctx context.Context, userID string) (int64, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		SELECT reference, reservation_ids
		FROM payment_checkouts
		WHERE user_id = $1 AND status = $2
		FOR UPDATE
	`, userID, sale.CheckoutOpen)
	if err != nil {
		return 0, fmt.Errorf("buscar checkouts abiertos: %w", err)
	}
	type open struct {
		reference      string
		reservationIDs []int64
	}
	var list []open
	for rows.Next() {
		var o open
		if err := rows.Scan(&o.reference, &o.reservationIDs); err != nil {
			rows.Close()
			return 0, fmt.Errorf("escanear checkout abierto: %w", err)
		}
		list = append(list, o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	for _, o := range list {
		if err := releaseReservationsTx(ctx, tx, o.reservationIDs); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE payment_checkouts SET status = $2 WHERE reference = $1
		`, o.reference, sale.CheckoutExpired); err != nil {
			return 0, fmt.Errorf("cerrar checkout abierto: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return int64(len(list)), nil
}

// ExpireCheckouts cierra los checkouts que el cliente no pagó dentro de la
// ventana de retención y libera el stock que retinían.
func (r *SaleRepository) ExpireCheckouts(ctx context.Context) (int64, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	type expired struct {
		reference      string
		reservationIDs []int64
	}
	rows, err := tx.Query(ctx, `
		SELECT reference, reservation_ids
		FROM payment_checkouts
		WHERE status = $1 AND expires_at <= now()
		FOR UPDATE
	`, sale.CheckoutOpen)
	if err != nil {
		return 0, fmt.Errorf("buscar checkouts vencidos: %w", err)
	}
	var list []expired
	for rows.Next() {
		var e expired
		if err := rows.Scan(&e.reference, &e.reservationIDs); err != nil {
			rows.Close()
			return 0, fmt.Errorf("escanear checkout vencido: %w", err)
		}
		list = append(list, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	for _, e := range list {
		if err := releaseReservationsTx(ctx, tx, e.reservationIDs); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE payment_checkouts SET status = $2 WHERE reference = $1
		`, e.reference, sale.CheckoutExpired); err != nil {
			return 0, fmt.Errorf("cerrar checkout vencido: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return int64(len(list)), nil
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
		 price_usd, price_cop, status)
	SELECT u.id, u.user_id,
	       TRIM(COALESCE(usr.first_name,'') || ' ' || COALESCE(usr.last_name,'')), COALESCE(usr.email,''),
	       c.id, c.name, cv.variant_name, il.language, u.quantity,
	       il.price_usd, il.price_cop, $3
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
	if _, err := tx.Exec(ctx, reserveToLogSQL, ids, "confirmed", "sold"); err != nil {
		return false, fmt.Errorf("confirmar reservas del pago: %w", err)
	}
	return active < len(ids), nil
}

// releaseReservationsTx libera las reservas (el stock vuelve al inventario) y
// registra el historial 'returned'. Es idempotente: solo toca las 'active'.
func releaseReservationsTx(ctx context.Context, tx pgx.Tx, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, reserveToLogSQL, ids, "released", "returned"); err != nil {
		return fmt.Errorf("liberar reservas: %w", err)
	}
	return nil
}

func (r *SaleRepository) ListByUser(ctx context.Context, userID string, limit, offset int) ([]sale.Sale, error) {
	rows, err := r.db.Query(ctx, saleSelect+` WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`, userID, limit, offset)
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
	return out, nil
}

func (r *SaleRepository) ListAll(ctx context.Context, limit, offset int) ([]sale.Sale, error) {
	rows, err := r.db.Query(ctx, saleSelect+` ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("listar ventas: %w", err)
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
	for i := range out {
		if err := r.loadItems(ctx, &out[i]); err != nil {
			return nil, fmt.Errorf("cargar items de venta %d: %w", out[i].ID, err)
		}
	}
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
		WHERE created_at >= $2
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
