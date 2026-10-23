package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"trample-back/internal/domain/reservation"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ReservationRepository struct {
	db *pgxpool.Pool
}

func NewReservationRepository(db *pgxpool.Pool) *ReservationRepository {
	return &ReservationRepository{db: db}
}

// listingRowReusable es la proyección compartida de un inventory_listings
// enriquecido con los datos de la carta para mostrar en el carrito.
type listingRow struct {
	ListingID   int64
	CardID      int64
	CardName    string
	VariantName string
	Language    string
	Quantity    int
	PriceUSD    float64
	PriceCOP    float64
	CardImage   string
}

const listingSelect = `
	SELECT
		il.id, c.id, c.name, cv.variant_name, il.language, il.quantity,
		il.price_usd, il.price_cop,
		COALESCE(
			(SELECT vi.small_url FROM card_images vi WHERE vi.variant_id = cv.id AND vi.small_url <> '' LIMIT 1),
			(SELECT ci.small_url FROM card_images ci WHERE ci.card_id = c.id AND ci.small_url <> '' LIMIT 1),
			''
		)
 FROM inventory_listings il
	JOIN card_variants cv ON cv.id = il.variant_id
	JOIN cards c ON c.id = cv.card_id
`

func scanListingRow(row pgx.Row) (listingRow, error) {
	var l listingRow
	err := row.Scan(
		&l.ListingID, &l.CardID, &l.CardName, &l.VariantName, &l.Language,
		&l.Quantity, &l.PriceUSD, &l.PriceCOP, &l.CardImage,
	)
	return l, err
}

// FindListingForReserve devuelve el listing activo con stock más adecuado
// para reservar una carta en un idioma concreto. Si variantName no viene
// vacío, solo considera listings de ese acabado (insensible a mayúsculas).
func (r *ReservationRepository) FindListingForReserve(ctx context.Context, cardID int64, language, variantName string) (reservation.ListingInfo, error) {
	query := listingSelect + `
		AND cv.card_id = $1
		AND il.language = $2
		AND il.status = 'active'
		AND il.quantity > 0
	`
	args := []any{cardID, language}
	if variantName != "" {
		query += ` AND LOWER(cv.variant_name) = LOWER($3)`
		args = append(args, variantName)
	}
	// Primero los listings que todavía tienen unidades sin reservar y, entre ellos,
	// el de más stock (criterio estable: sumar otra unidad cae en la misma línea
	// del carrito). Si el de más stock ya está reservado por completo, se usa otro
	// de la misma carta e idioma en vez de responder "stock insuficiente".
	query += `
		ORDER BY (il.quantity - COALESCE((
		              SELECT SUM(cr.quantity) FROM cart_reservations cr
		              WHERE cr.listing_id = il.id AND cr.status = 'active'
		          ), 0)) > 0 DESC,
		         il.quantity DESC, il.id ASC
		LIMIT 1
	`
	row := r.db.QueryRow(ctx, query, args...)
	l, err := scanListingRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return reservation.ListingInfo{}, reservation.ErrInvalidListing
		}
		return reservation.ListingInfo{}, fmt.Errorf("buscar listing para reserva: %w", err)
	}
	return reservation.ListingInfo{
		ListingID:   l.ListingID,
		CardID:      l.CardID,
		CardName:    l.CardName,
		VariantName: l.VariantName,
		Language:    l.Language,
		Quantity:    l.Quantity,
		PriceUSD:    l.PriceUSD,
		PriceCOP:    l.PriceCOP,
		CardImage:   l.CardImage,
	}, nil
}

// listingReserved suma las unidades de un listing concreto que están
// reservadas activamente (cualquier usuario) para calcular el stock
// realmente comprable de esa variante+idioma dentro de la transacción.
func listingReserved(ctx context.Context, tx pgx.Tx, listingID int64) (int, error) {
	var reserved int
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(cr.quantity), 0)
		FROM cart_reservations cr
		WHERE cr.listing_id = $1 AND cr.status = 'active'
	`, listingID).Scan(&reserved)
	if err != nil {
		return 0, err
	}
	return reserved, nil
}

// Reserve registra (o incrementa) la reserva activa del usuario en una
// transacción atómica de 5 minutos. NO descuenta el inventario: la carta se
// consume del listing solo al confirmarse la venta.
func (r *ReservationRepository) Reserve(ctx context.Context, input reservation.ReserveInput, durationMinutes int) (reservation.Reservation, error) {
	info, err := r.FindListingForReserve(ctx, input.CardID, input.Language, input.VariantName)
	if err != nil {
		return reservation.Reservation{}, err
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return reservation.Reservation{}, err
	}
	defer tx.Rollback(ctx)

	// Stock realmente comprable del listing específico (variante+idioma):
	// unidades del listing menos las reservas activas de cualquier usuario
	// para ese mismo listing. La cláusula FOR UPDATE bloquea la fila para
	// serializar reservas concurrentes y evitar que dos clientes reserven la
	// última unidad al mismo tiempo.
	var totalStock int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(il.quantity, 0)::int
		FROM inventory_listings il
		WHERE il.id = $1 AND il.status = 'active'
		FOR UPDATE
	`, info.ListingID).Scan(&totalStock); err != nil {
		return reservation.Reservation{}, fmt.Errorf("calcular stock del listing: %w", err)
	}

	reserved, err := listingReserved(ctx, tx, info.ListingID)
	if err != nil {
		return reservation.Reservation{}, fmt.Errorf("calcular reservado del listing: %w", err)
	}
	available := totalStock - reserved
	if input.Quantity <= 0 || available < input.Quantity {
		return reservation.Reservation{}, reservation.ErrInsufficientStock
	}

	expiresAt := time.Now().Add(time.Duration(durationMinutes) * time.Minute)

	// Upsert atómico: si el usuario ya tiene una reserva activa para este
	// listing, se incrementa la cantidad y se renueva la ventana de 5 minutos
	// minutos (gracias al índice único parcial 005). Así el carrito muestra
	// una sola línea por carta+idioma y no se puede exceder el stock real.
	var res reservation.Reservation
	err = tx.QueryRow(ctx, `
		INSERT INTO cart_reservations (user_id, listing_id, quantity, expires_at, status)
		VALUES ($1, $2, $3, $4, 'active')
		ON CONFLICT (user_id, listing_id) WHERE status = 'active'
		DO UPDATE SET quantity = cart_reservations.quantity + EXCLUDED.quantity,
		              expires_at = GREATEST(cart_reservations.expires_at, EXCLUDED.expires_at)
		RETURNING id, listing_id, quantity, expires_at
	`, input.UserID, info.ListingID, input.Quantity, expiresAt).Scan(
		&res.ID, &res.ListingID, &res.Quantity, &res.ExpiresAt,
	)
	if err != nil {
		return reservation.Reservation{}, fmt.Errorf("crear/incrementar reserva: %w", err)
	}
	// GREATEST: si la línea ya sostiene un pago en curso, su retención (30 min) es
	// más larga que la del carrito (5 min) y sumar una unidad no debe acortarla.
	// Si cambió la cantidad, el pago abierto ya no corresponde al carrito.
	if err := closeCheckoutsHoldingTx(ctx, tx, res.ID); err != nil {
		return reservation.Reservation{}, err
	}

	// El inventario NO se descuenta aquí: la carta se consume al confirmarse
	// la venta (SaleRepository.Create). Mientras la reserva esté activa, ese
	// stock queda "apartado" y no lo puede reservar otro cliente.

	// Historico: la carta pasó a reservada (active).
	if err := logReservation(ctx, tx, reservation.ReservationLog{
		ReservationID: res.ID,
		UserID:        input.UserID,
		CardID:        info.CardID,
		CardName:      info.CardName,
		VariantName:   info.VariantName,
		Language:      info.Language,
		Quantity:      input.Quantity,
		PriceUSD:      info.PriceUSD,
		PriceCOP:      info.PriceCOP,
		Status:        reservation.LogReserved,
	}); err != nil {
		return reservation.Reservation{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return reservation.Reservation{}, err
	}

	// reserved incluía lo que esta línea tenía antes del upsert.
	res.MaxQuantity = totalStock - reserved - input.Quantity + res.Quantity
	res.CardID = info.CardID
	res.CardName = info.CardName
	res.VariantName = info.VariantName
	res.Language = info.Language
	res.PriceUSD = info.PriceUSD
	res.PriceCOP = info.PriceCOP
	res.CardImage = info.CardImage
	return res, nil
}

// maxQuantitySQL calcula, para la reserva `cr`, hasta cuántas unidades puede
// llegar: el stock del listing (0 si no está activo) menos lo que reservan los
// demás. Es el max_quantity del ítem del carrito.
const maxQuantitySQL = `GREATEST(
		       CASE WHEN il.status = 'active' THEN il.quantity ELSE 0 END
		       - COALESCE((SELECT SUM(o.quantity) FROM cart_reservations o
		                   WHERE o.listing_id = il.id AND o.status = 'active' AND o.id <> cr.id), 0),
		       0)::int`

func (r *ReservationRepository) ListActiveByUser(ctx context.Context, userID string) ([]reservation.Reservation, error) {
	rows, err := r.db.Query(ctx, `
		SELECT cr.id, cr.listing_id, cr.quantity, cr.expires_at,
		       c.id, c.name, lcv.variant_name, cr.listing_id,
		       il.language, il.price_usd, il.price_cop, `+maxQuantitySQL+`,
		       COALESCE(
		           (SELECT vi.small_url FROM card_images vi WHERE vi.variant_id = lcv.id AND vi.small_url <> '' LIMIT 1),
		           (SELECT ci.small_url FROM card_images ci WHERE ci.card_id = c.id AND ci.small_url <> '' LIMIT 1),
		           ''
		       )
		FROM cart_reservations cr
		JOIN inventory_listings il ON il.id = cr.listing_id
		JOIN card_variants lcv ON lcv.id = il.variant_id
		JOIN cards c ON c.id = lcv.card_id
		WHERE cr.user_id = $1 AND cr.status = 'active' AND cr.expires_at > now()
		ORDER BY cr.created_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("listar reservas: %w", err)
	}
	defer rows.Close()

	var out []reservation.Reservation
	for rows.Next() {
		var res reservation.Reservation
		if err := rows.Scan(
			&res.ID, &res.ListingID, &res.Quantity, &res.ExpiresAt,
			&res.CardID, &res.CardName, &res.VariantName, &res.ListingID,
			&res.Language, &res.PriceUSD, &res.PriceCOP, &res.MaxQuantity,
			&res.CardImage,
		); err != nil {
			return nil, fmt.Errorf("escanear reserva: %w", err)
		}
		out = append(out, res)
	}
	return out, rows.Err()
}

func (r *ReservationRepository) ListActiveByUserAndIDs(ctx context.Context, userID string, ids []int64) ([]reservation.Reservation, error) {
	rows, err := r.db.Query(ctx, `
		SELECT cr.id, cr.listing_id, cr.quantity, cr.expires_at,
		       c.id, c.name, lcv.variant_name, cr.listing_id,
		       il.language, il.price_usd, il.price_cop, `+maxQuantitySQL+`,
		       COALESCE(
		           (SELECT vi.small_url FROM card_images vi WHERE vi.variant_id = lcv.id AND vi.small_url <> '' LIMIT 1),
		           (SELECT ci.small_url FROM card_images ci WHERE ci.card_id = c.id AND ci.small_url <> '' LIMIT 1),
		           ''
		       )
		FROM cart_reservations cr
		JOIN inventory_listings il ON il.id = cr.listing_id
		JOIN card_variants lcv ON lcv.id = il.variant_id
		JOIN cards c ON c.id = lcv.card_id
		WHERE cr.user_id = $1 AND cr.status = 'active' AND cr.expires_at > now() AND cr.id = ANY($2)
		ORDER BY cr.created_at DESC
	`, userID, ids)
	if err != nil {
		return nil, fmt.Errorf("listar reservas por ids: %w", err)
	}
	defer rows.Close()

	var out []reservation.Reservation
	for rows.Next() {
		var res reservation.Reservation
		if err := rows.Scan(
			&res.ID, &res.ListingID, &res.Quantity, &res.ExpiresAt,
			&res.CardID, &res.CardName, &res.VariantName, &res.ListingID,
			&res.Language, &res.PriceUSD, &res.PriceCOP, &res.MaxQuantity,
			&res.CardImage,
		); err != nil {
			return nil, fmt.Errorf("escanear reserva por ids: %w", err)
		}
		out = append(out, res)
	}
	return out, rows.Err()
}

func (r *ReservationRepository) Confirm(ctx context.Context, userID string, ids []int64) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := confirmReservationsStrictTx(ctx, tx, userID, ids); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// confirmReservationsStrictTx confirma TODAS las reservas del usuario indicadas y
// registra el historial 'sold'. Si alguna ya no está activa devuelve
// reservation.ErrNotFound sin tocar ninguna. Lo comparten Confirm y el registro de
// ventas en efectivo/transferencia, para que la confirmación y la venta ocurran en
// la misma transacción.
func confirmReservationsStrictTx(ctx context.Context, tx pgx.Tx, userID string, ids []int64) error {
	rows, err := tx.Query(ctx, `
		SELECT cr.id, cr.quantity,
		       c.id, c.name, cv.variant_name, il.language, il.price_usd, il.price_cop
		FROM cart_reservations cr
		JOIN inventory_listings il ON il.id = cr.listing_id
		JOIN card_variants cv ON cv.id = il.variant_id
		JOIN cards c ON c.id = cv.card_id
		WHERE cr.user_id = $1 AND cr.id = ANY($2) AND cr.status = 'active'
		FOR UPDATE
	`, userID, ids)
	if err != nil {
		return fmt.Errorf("buscar reservas a confirmar: %w", err)
	}

	type confirmed struct {
		id       int64
		quantity int
		details  reservation.Reservation
	}
	var items []confirmed
	for rows.Next() {
		var it confirmed
		if err := rows.Scan(&it.id, &it.quantity,
			&it.details.CardID, &it.details.CardName, &it.details.VariantName,
			&it.details.Language, &it.details.PriceUSD, &it.details.PriceCOP); err != nil {
			rows.Close()
			return fmt.Errorf("escanear reserva a confirmar: %w", err)
		}
		items = append(items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// Debemos confirmar TODAS las reservas solicitadas; si alguna expiró o
	// ya no está activa en este instante, no se devuelve en la consulta y el
	// recuento no coincide -> se considera un fallo de confirmación.
	if len(items) != len(ids) {
		return reservation.ErrNotFound
	}

	for _, it := range items {
		if _, err := tx.Exec(ctx, `
			UPDATE cart_reservations SET status = 'confirmed' WHERE id = $1
		`, it.id); err != nil {
			return fmt.Errorf("confirmar reserva: %w", err)
		}
		// Historico: la reserva se vendió (pedido confirmado).
		if err := logReservation(ctx, tx, reservation.ReservationLog{
			ReservationID: it.id,
			UserID:        userID,
			CardID:        it.details.CardID,
			CardName:      it.details.CardName,
			VariantName:   it.details.VariantName,
			Language:      it.details.Language,
			Quantity:      it.quantity,
			PriceUSD:      it.details.PriceUSD,
			PriceCOP:      it.details.PriceCOP,
			Status:        reservation.LogSold,
		}); err != nil {
			return err
		}
	}

	return nil
}

// ExtendExpiry renueva la ventana de las reservas activas indicadas a
// `minutes` a partir de ahora. Es el mecanismo con el que la retención de 5
// minutos del carrito se convierte en la de 30 minutos cuando el cliente pasa
// al pago con Bold: si alguna de las reservas ya no está activa devuelve
// reservation.ErrNotFound.
func (r *ReservationRepository) ExtendExpiry(ctx context.Context, userID string, ids []int64, minutes int) error {
	if len(ids) == 0 {
		return nil
	}
	tag, err := r.db.Exec(ctx, `
		UPDATE cart_reservations
		SET expires_at = now() + make_interval(mins => $3)
		WHERE user_id = $1 AND id = ANY($2) AND status = 'active'
	`, userID, ids, minutes)
	if err != nil {
		return fmt.Errorf("extender reservas: %w", err)
	}
	if int(tag.RowsAffected()) != len(ids) {
		return reservation.ErrNotFound
	}
	return nil
}

func (r *ReservationRepository) Remove(ctx context.Context, userID string, id int64) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var quantity int
	var details reservation.Reservation
	err = tx.QueryRow(ctx, `
		SELECT cr.quantity,
		       c.id, c.name, cv.variant_name, il.language, il.price_usd, il.price_cop
		FROM cart_reservations cr
		JOIN inventory_listings il ON il.id = cr.listing_id
		JOIN card_variants cv ON cv.id = il.variant_id
		JOIN cards c ON c.id = cv.card_id
		WHERE cr.id = $1 AND cr.user_id = $2 AND cr.status = 'active'
		FOR UPDATE
	`, id, userID).Scan(
		&quantity,
		&details.CardID, &details.CardName, &details.VariantName, &details.Language,
		&details.PriceUSD, &details.PriceCOP,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return reservation.ErrNotFound
		}
		return fmt.Errorf("buscar reserva para liberar: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE cart_reservations SET status = 'released' WHERE id = $1
	`, id); err != nil {
		return fmt.Errorf("liberar reserva: %w", err)
	}
	// Quitar una carta cambia el total: el pago abierto que la incluía ya no
	// corresponde al carrito y no debe poder cobrarse con el monto anterior.
	if err := closeCheckoutsHoldingTx(ctx, tx, id); err != nil {
		return err
	}

	// El inventario no se restaura: nunca se descontó al reservar (la carta
	// se consume solo al confirmarse la venta).

	// Historico: la reserva se devolvió (quedó disponible para otros clientes).
	if err := logReservation(ctx, tx, reservation.ReservationLog{
		ReservationID: id,
		UserID:        userID,
		CardID:        details.CardID,
		CardName:      details.CardName,
		VariantName:   details.VariantName,
		Language:      details.Language,
		Quantity:      quantity,
		PriceUSD:      details.PriceUSD,
		PriceCOP:      details.PriceCOP,
		Status:        reservation.LogReturned,
		ReturnReason:  reservation.ReturnRemoved,
	}); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// ReleaseActiveByID libera una reserva activa sin importar el usuario. Es la
// acción del panel admin para quitar una reserva que quedó atascada en el
// carrito. Registrar el evento en el historial deja rastro de quién la tenía y
// cuándo se liberó; también cierra cualquier pago abierto que la retuviera.
func (r *ReservationRepository) ReleaseActiveByID(ctx context.Context, id int64) error {
	return pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		var (
			quantity int
			details  reservation.Reservation
		)
		err := tx.QueryRow(ctx, `
			SELECT cr.quantity, cr.user_id,
			       c.id, c.name, cv.variant_name, il.language, il.price_usd, il.price_cop
			FROM cart_reservations cr
			JOIN inventory_listings il ON il.id = cr.listing_id
			JOIN card_variants cv ON cv.id = il.variant_id
			JOIN cards c ON c.id = cv.card_id
			WHERE cr.id = $1 AND cr.status = 'active'
			FOR UPDATE
		`, id).Scan(
			&quantity, &details.UserID,
			&details.CardID, &details.CardName, &details.VariantName, &details.Language,
			&details.PriceUSD, &details.PriceCOP,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return reservation.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("buscar reserva a liberar: %w", err)
		}

		if _, err := tx.Exec(ctx,
			`UPDATE cart_reservations SET status = 'released' WHERE id = $1`,
			id,
		); err != nil {
			return fmt.Errorf("liberar reserva: %w", err)
		}
		if err := closeCheckoutsHoldingTx(ctx, tx, id); err != nil {
			return err
		}
		return logReservation(ctx, tx, reservation.ReservationLog{
			ReservationID: id,
			UserID:        details.UserID,
			CardID:        details.CardID,
			CardName:      details.CardName,
			VariantName:   details.VariantName,
			Language:      details.Language,
			Quantity:      quantity,
			PriceUSD:      details.PriceUSD,
			PriceCOP:      details.PriceCOP,
			Status:        reservation.LogReturned,
			ReturnReason:  reservation.ReturnRemoved,
		})
	})
}

// UpdateQuantity fija la cantidad de una reserva activa del usuario en una
// transacción. Bloquea primero el listing y después la reserva, el mismo orden
// que Reserve, para serializar contra otras reservas del listing sin
// interbloquearse. expires_at no se toca.
func (r *ReservationRepository) UpdateQuantity(ctx context.Context, userID string, id int64, quantity int) (reservation.Reservation, error) {
	if quantity < 1 {
		return reservation.Reservation{}, reservation.ErrInvalidQuantity
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return reservation.Reservation{}, err
	}
	defer tx.Rollback(ctx)

	var listingID int64
	err = tx.QueryRow(ctx, `
		SELECT listing_id FROM cart_reservations
		WHERE id = $1 AND user_id = $2 AND status = 'active' AND expires_at > now()
	`, id, userID).Scan(&listingID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return reservation.Reservation{}, reservation.ErrNotFound
		}
		return reservation.Reservation{}, fmt.Errorf("buscar reserva a editar: %w", err)
	}

	// Un listing pausado no tiene stock comprable: se puede bajar, no subir.
	var stock int
	if err := tx.QueryRow(ctx, `
		SELECT CASE WHEN status = 'active' THEN quantity ELSE 0 END
		FROM inventory_listings WHERE id = $1
		FOR UPDATE
	`, listingID).Scan(&stock); err != nil {
		return reservation.Reservation{}, fmt.Errorf("bloquear listing de la reserva: %w", err)
	}

	// Se relee con la reserva bloqueada: entre la primera lectura y el bloqueo
	// del listing pudo venderse, quitarse o vencer.
	var res reservation.Reservation
	err = tx.QueryRow(ctx, `
		SELECT cr.id, cr.listing_id, cr.quantity, cr.expires_at,
		       c.id, c.name, cv.variant_name, il.language, il.price_usd, il.price_cop,
		       COALESCE(
		           (SELECT vi.small_url FROM card_images vi WHERE vi.variant_id = cv.id AND vi.small_url <> '' LIMIT 1),
		           (SELECT ci.small_url FROM card_images ci WHERE ci.card_id = c.id AND ci.small_url <> '' LIMIT 1),
		           ''
		       )
		FROM cart_reservations cr
		JOIN inventory_listings il ON il.id = cr.listing_id
		JOIN card_variants cv ON cv.id = il.variant_id
		JOIN cards c ON c.id = cv.card_id
		WHERE cr.id = $1 AND cr.user_id = $2 AND cr.status = 'active' AND cr.expires_at > now()
		FOR UPDATE OF cr
	`, id, userID).Scan(
		&res.ID, &res.ListingID, &res.Quantity, &res.ExpiresAt,
		&res.CardID, &res.CardName, &res.VariantName, &res.Language,
		&res.PriceUSD, &res.PriceCOP, &res.CardImage,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return reservation.Reservation{}, reservation.ErrNotFound
		}
		return reservation.Reservation{}, fmt.Errorf("bloquear reserva a editar: %w", err)
	}

	// Con el pago en curso el monto ya se le mostró al cliente en Bold.
	var inCheckout bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM payment_checkouts
			WHERE status = 'open' AND expires_at > now() AND $1 = ANY(reservation_ids)
		)
	`, id).Scan(&inCheckout); err != nil {
		return reservation.Reservation{}, fmt.Errorf("buscar pago en curso de la reserva: %w", err)
	}
	if inCheckout {
		return reservation.Reservation{}, reservation.ErrCheckoutInProgress
	}

	// Lo que esta línea ya aparta cuenta como disponible para ella: pasar de 2
	// a 3 necesita 1 libre, no 3.
	var reservedByOthers int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(quantity), 0)::int FROM cart_reservations
		WHERE listing_id = $1 AND status = 'active' AND id <> $2
	`, listingID, id).Scan(&reservedByOthers); err != nil {
		return reservation.Reservation{}, fmt.Errorf("calcular reservado del listing: %w", err)
	}
	res.MaxQuantity = max(stock-reservedByOthers, 0)
	// Bajar siempre se permite, aunque el stock haya quedado por debajo.
	if quantity > res.Quantity && quantity > res.MaxQuantity {
		return reservation.Reservation{}, reservation.ErrInsufficientStock
	}

	previous := res.Quantity
	if quantity == previous {
		return res, tx.Commit(ctx)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE cart_reservations SET quantity = $2 WHERE id = $1
	`, id, quantity); err != nil {
		return reservation.Reservation{}, fmt.Errorf("actualizar cantidad de la reserva: %w", err)
	}
	res.Quantity = quantity
	// Un checkout abierto ya vencido sigue apuntando a la línea con el monto
	// viejo; se cierra igual que al agregar o quitar.
	if err := closeCheckoutsHoldingTx(ctx, tx, id); err != nil {
		return reservation.Reservation{}, err
	}

	// Historico: la diferencia se reservó (subió) o se devolvió (bajó).
	status, delta, reason := reservation.LogReserved, quantity-previous, ""
	if delta < 0 {
		status, delta, reason = reservation.LogReturned, -delta, reservation.ReturnRemoved
	}
	if err := logReservation(ctx, tx, reservation.ReservationLog{
		ReservationID: id,
		UserID:        userID,
		CardID:        res.CardID,
		CardName:      res.CardName,
		VariantName:   res.VariantName,
		Language:      res.Language,
		Quantity:      delta,
		PriceUSD:      res.PriceUSD,
		PriceCOP:      res.PriceCOP,
		Status:        status,
		ReturnReason:  reason,
	}); err != nil {
		return reservation.Reservation{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return reservation.Reservation{}, err
	}
	return res, nil
}

func (r *ReservationRepository) ReleaseExpired(ctx context.Context) (int64, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		SELECT cr.id, cr.listing_id, cr.quantity, cr.user_id,
		       c.id, c.name, cv.variant_name, il.language, il.price_usd, il.price_cop
		FROM cart_reservations cr
		JOIN inventory_listings il ON il.id = cr.listing_id
		JOIN card_variants cv ON cv.id = il.variant_id
		JOIN cards c ON c.id = cv.card_id
		WHERE cr.status = 'active' AND cr.expires_at <= now()
		FOR UPDATE
	`)
	if err != nil {
		return 0, fmt.Errorf("buscar reservas expiradas: %w", err)
	}

	type expired struct {
		id        int64
		listingID int64
		quantity  int
		details   reservation.Reservation
	}
	var exps []expired
	for rows.Next() {
		var e expired
		if err := rows.Scan(&e.id, &e.listingID, &e.quantity, &e.details.UserID,
			&e.details.CardID, &e.details.CardName, &e.details.VariantName, &e.details.Language,
			&e.details.PriceUSD, &e.details.PriceCOP); err != nil {
			rows.Close()
			return 0, fmt.Errorf("escanear reserva expirada: %w", err)
		}
		exps = append(exps, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	for _, e := range exps {
		if _, err := tx.Exec(ctx, `UPDATE cart_reservations SET status = 'released' WHERE id = $1`, e.id); err != nil {
			return 0, fmt.Errorf("marcar reserva liberada: %w", err)
		}
		// El inventario no se restaura: nunca se descontó al reservar.
		// Historico: por vencimiento, la reserva vuelve a estar disponible.
		if err := logReservation(ctx, tx, reservation.ReservationLog{
			ReservationID: e.id,
			UserID:        e.details.UserID,
			CardID:        e.details.CardID,
			CardName:      e.details.CardName,
			VariantName:   e.details.VariantName,
			Language:      e.details.Language,
			Quantity:      e.quantity,
			PriceUSD:      e.details.PriceUSD,
			PriceCOP:      e.details.PriceCOP,
			Status:        reservation.LogReturned,
			ReturnReason:  reservation.ReturnExpired,
		}); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return int64(len(exps)), nil
}

// closeCheckoutsHoldingTx cierra los checkouts abiertos que incluyen la reserva
// indicada. Se llama cuando el cliente modifica esa línea del carrito (cambia su
// cantidad o la quita): el monto que Bold iba a cobrar quedó desactualizado.
// No libera reservas; el siguiente "Pagar" crea un checkout con el carrito nuevo.
func closeCheckoutsHoldingTx(ctx context.Context, tx pgx.Tx, reservationID int64) error {
	if _, err := tx.Exec(ctx, `
		UPDATE payment_checkouts SET status = 'expired'
		WHERE status = 'open' AND $1 = ANY(reservation_ids)
	`, reservationID); err != nil {
		return fmt.Errorf("cerrar checkouts de la reserva %d: %w", reservationID, err)
	}
	return nil
}

// logReservation registra un cambio de estado de una reserva en el historial.
// Resuelve el nombre y correo del cliente desde la tabla `users` en el momento
// del evento, para que queden guardados aunque la cuenta cambie después.
func logReservation(ctx context.Context, tx pgx.Tx, l reservation.ReservationLog) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO cart_reservation_logs
			(reservation_id, user_id, customer_name, customer_email,
			 card_id, card_name, variant_name, language, quantity,
			 price_usd, price_cop, status, return_reason)
		SELECT $1, $2,
		       TRIM(COALESCE(u.first_name,'') || ' ' || COALESCE(u.last_name,'')), COALESCE(u.email,''),
		       $3, $4, $5, $6, $7, $8, $9, $10, NULLIF($11, '')
		FROM users u
		WHERE u.id = $2
	`, l.ReservationID, l.UserID, l.CardID, l.CardName, l.VariantName, l.Language,
		l.Quantity, l.PriceUSD, l.PriceCOP, l.Status, l.ReturnReason); err != nil {
		return fmt.Errorf("registrar log de reserva: %w", err)
	}
	return nil
}

// reservationLogsSQL une el historial de reservas de cartas y de productos con
// el estado actual de cada reserva: cuándo vence (solo si sigue activa) y si la
// retiene un pago en línea abierto.
const reservationLogsSQL = `
	SELECT 'card' AS kind, l.id, l.reservation_id, COALESCE(l.user_id::text, '') AS user_id,
	       l.customer_name, l.customer_email,
	       l.card_id, l.card_name, l.variant_name, l.language,
	       0::bigint AS product_id, ''::text AS title,
	       l.quantity, l.price_usd::float8 AS price_usd, l.price_cop::float8 AS price_cop,
	       l.status, COALESCE(l.return_reason, '') AS return_reason, l.created_at,
	       CASE WHEN cr.status = 'active' THEN cr.expires_at END AS expires_at,
	       COALESCE(cr.status = 'active' AND EXISTS (
	           SELECT 1 FROM payment_checkouts pc
	           WHERE pc.status = 'open' AND pc.expires_at > now() AND cr.id = ANY(pc.reservation_ids)), false) AS in_checkout
	FROM cart_reservation_logs l
	LEFT JOIN cart_reservations cr ON cr.id = l.reservation_id
	UNION ALL
	SELECT 'product', l.id, l.reservation_id, COALESCE(l.user_id::text, ''),
	       l.customer_name, l.customer_email,
	       0::bigint, ''::text, ''::text, ''::text,
	       COALESCE(l.product_id, 0), l.title,
	       l.quantity, 0::float8, l.price_cop::float8,
	       l.status, COALESCE(l.return_reason, ''), l.created_at,
	       CASE WHEN pr.status = 'active' THEN pr.expires_at END,
	       COALESCE(pr.status = 'active' AND EXISTS (
	           SELECT 1 FROM payment_checkouts pc
	           WHERE pc.status = 'open' AND pc.expires_at > now() AND pr.id = ANY(pc.product_reservation_ids)), false)
	FROM product_reservation_logs l
	LEFT JOIN product_reservations pr ON pr.id = l.reservation_id
`

// ListReservationLogs devuelve el historial de reservas de cartas y productos,
// más recientes primero, con filtros y el total de resultados sin paginar.
func (r *ReservationRepository) ListReservationLogs(ctx context.Context, f reservation.LogFilter) ([]reservation.ReservationLog, int, error) {
	var (
		conds []string
		args  []any
	)
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if !f.From.IsZero() {
		conds = append(conds, "created_at >= "+arg(f.From))
	}
	if !f.To.IsZero() {
		conds = append(conds, "created_at < "+arg(f.To))
	}
	if f.Status != "" {
		conds = append(conds, "status = "+arg(f.Status))
	}
	if f.Kind != "" {
		conds = append(conds, "kind = "+arg(f.Kind))
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		like := arg("%" + escapeLike(q) + "%")
		conds = append(conds, "(customer_name ILIKE "+like+" OR customer_email ILIKE "+like+
			" OR card_name ILIKE "+like+" OR title ILIKE "+like+")")
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}

	var total int
	if err := r.db.QueryRow(ctx, `SELECT count(*) FROM (`+reservationLogsSQL+`) logs`+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("contar logs de reserva: %w", err)
	}

	n := len(args)
	rows, err := r.db.Query(ctx, `
		SELECT kind, id, reservation_id, user_id, customer_name, customer_email,
		       card_id, card_name, variant_name, language, product_id, title,
		       quantity, price_usd, price_cop, status, return_reason, created_at,
		       expires_at, in_checkout
		FROM (`+reservationLogsSQL+`) logs`+where+
		fmt.Sprintf(` ORDER BY created_at DESC, kind, id DESC LIMIT $%d OFFSET $%d`, n+1, n+2),
		append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("listar logs de reserva: %w", err)
	}
	defer rows.Close()

	out := []reservation.ReservationLog{}
	for rows.Next() {
		var (
			l         reservation.ReservationLog
			expiresAt *time.Time
		)
		if err := rows.Scan(
			&l.Kind, &l.ID, &l.ReservationID, &l.UserID, &l.CustomerName, &l.CustomerEmail,
			&l.CardID, &l.CardName, &l.VariantName, &l.Language, &l.ProductID, &l.Title,
			&l.Quantity, &l.PriceUSD, &l.PriceCOP, &l.Status, &l.ReturnReason, &l.CreatedAt,
			&expiresAt, &l.InCheckout,
		); err != nil {
			return nil, 0, fmt.Errorf("escanear log de reserva: %w", err)
		}
		if expiresAt != nil {
			l.ExpiresAt = *expiresAt
		}
		out = append(out, l)
	}
	return out, total, rows.Err()
}
