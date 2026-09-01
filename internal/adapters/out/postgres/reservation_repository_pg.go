package postgres

import (
	"context"
	"errors"
	"fmt"
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
	ListingID  int64
	CardID     int64
	CardName   string
	VariantName string
	Language   string
	Quantity   int
	PriceUSD   float64
	PriceCOP   float64
	Stock      int
	CardImage  string
	Status     string
}

const listingSelect = `
	SELECT
		il.id, c.id, c.name, cv.variant_name, il.language, il.quantity,
		il.price_usd, il.price_cop,
		il.quantity, il.status,
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
		&l.Quantity, &l.PriceUSD, &l.PriceCOP, &l.Stock, &l.Status, &l.CardImage,
	)
	return l, err
}

// FindListingForReserve devuelve el listing activo con stock más adecuado
// para reservar una carta en un idioma concreto.
func (r *ReservationRepository) FindListingForReserve(ctx context.Context, cardID int64, language string) (reservation.ListingInfo, error) {
	row := r.db.QueryRow(ctx, listingSelect+`
		AND cv.card_id = $1
		AND il.language = $2
		AND il.status = 'active'
		AND il.quantity > 0
		ORDER BY il.quantity DESC, il.id ASC
		LIMIT 1
	`, cardID, language)
	l, err := scanListingRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return reservation.ListingInfo{}, reservation.ErrInvalidListing
		}
		return reservation.ListingInfo{}, fmt.Errorf("buscar listing para reserva: %w", err)
	}
	return reservation.ListingInfo{
		ListingID:  l.ListingID,
		CardID:     l.CardID,
		CardName:   l.CardName,
		VariantName: l.VariantName,
		Language:   l.Language,
		Quantity:   l.Quantity,
		PriceUSD:   l.PriceUSD,
		PriceCOP:   l.PriceCOP,
		Stock:      l.Stock,
		CardImage:  l.CardImage,
		Status:     l.Status,
	}, nil
}

// cardReserved suma las unidades de una carta+idioma que están
// reservadas activamente (cualquier usuario) para calcular el stock
// realmente comprable dentro de la transacción.
func cardReserved(ctx context.Context, tx pgx.Tx, cardID int64, language string) (int, error) {
	var reserved int
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(cr.quantity), 0)
		FROM cart_reservations cr
		JOIN inventory_listings il ON il.id = cr.listing_id
		JOIN card_variants cv ON cv.id = il.variant_id
		WHERE cv.card_id = $1 AND il.language = $2 AND cr.status = 'active'
	`, cardID, language).Scan(&reserved)
	if err != nil {
		return 0, err
	}
	return reserved, nil
}

// Reserve descuenta stock del listing y crea (o incrementa) la reserva activa
// del usuario en una transacción atómica de 5 minutos.
func (r *ReservationRepository) Reserve(ctx context.Context, input reservation.ReserveInput, durationMinutes int) (reservation.Reservation, error) {
	info, err := r.FindListingForReserve(ctx, input.CardID, input.Language)
	if err != nil {
		return reservation.Reservation{}, err
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return reservation.Reservation{}, err
	}
	defer tx.Rollback(ctx)

	// Stock realmente comprable de la carta+idioma: suma de listings activos
	// menos las reservas activas de cualquier usuario.
	var totalStock int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(il.quantity), 0)::int
		FROM inventory_listings il
		JOIN card_variants cv ON cv.id = il.variant_id
		WHERE cv.card_id = $1 AND il.language = $2 AND il.status = 'active' AND il.quantity > 0
	`, input.CardID, input.Language).Scan(&totalStock); err != nil {
		return reservation.Reservation{}, fmt.Errorf("calcular stock total: %w", err)
	}

	reserved, err := cardReserved(ctx, tx, input.CardID, input.Language)
	if err != nil {
		return reservation.Reservation{}, fmt.Errorf("calcular reservado de la carta: %w", err)
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
		              expires_at = EXCLUDED.expires_at
		RETURNING id, user_id, listing_id, quantity, expires_at, status, created_at
	`, input.UserID, info.ListingID, input.Quantity, expiresAt).Scan(
		&res.ID, &res.UserID, &res.ListingID, &res.Quantity, &res.ExpiresAt, &res.Status, &res.CreatedAt,
	)
	if err != nil {
		return reservation.Reservation{}, fmt.Errorf("crear/incrementar reserva: %w", err)
	}

	// Descuenta el stock del inventario (la venta confirmada lo deja 'inactive').
	if _, err := tx.Exec(ctx, `
		UPDATE inventory_listings
		SET quantity = quantity - $2, updated_at = now()
		WHERE id = $1
	`, info.ListingID, input.Quantity); err != nil {
		return reservation.Reservation{}, fmt.Errorf("descontar stock del listing: %w", err)
	}

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

	res.UserID = input.UserID
	res.CardID = info.CardID
	res.CardName = info.CardName
	res.VariantName = info.VariantName
	res.Language = info.Language
	res.PriceUSD = info.PriceUSD
	res.PriceCOP = info.PriceCOP
	res.CardImage = info.CardImage
	return res, nil
}

func (r *ReservationRepository) ListActiveByUser(ctx context.Context, userID int64) ([]reservation.Reservation, error) {
	rows, err := r.db.Query(ctx, `
		SELECT cr.id, cr.user_id, cr.listing_id, cr.quantity, cr.expires_at, cr.status, cr.created_at,
		       c.id, c.name, lcv.variant_name, cr.listing_id,
		       il.language, il.price_usd, il.price_cop,
		       COALESCE(
		           (SELECT vi.small_url FROM card_images vi WHERE vi.variant_id = lcv.id AND vi.small_url <> '' LIMIT 1),
		           (SELECT ci.small_url FROM card_images ci WHERE ci.card_id = c.id AND ci.small_url <> '' LIMIT 1),
		           ''
		       )
		FROM cart_reservations cr
		JOIN inventory_listings il ON il.id = cr.listing_id
		JOIN card_variants lcv ON lcv.id = il.variant_id
		JOIN cards c ON c.id = lcv.card_id
		WHERE cr.user_id = $1 AND cr.status = 'active'
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
			&res.ID, &res.UserID, &res.ListingID, &res.Quantity, &res.ExpiresAt, &res.Status, &res.CreatedAt,
			&res.CardID, &res.CardName, &res.VariantName, &res.ListingID,
			&res.Language, &res.PriceUSD, &res.PriceCOP,
			&res.CardImage,
		); err != nil {
			return nil, fmt.Errorf("escanear reserva: %w", err)
		}
		out = append(out, res)
	}
	return out, rows.Err()
}

func (r *ReservationRepository) ListActiveByUserAndIDs(ctx context.Context, userID int64, ids []int64) ([]reservation.Reservation, error) {
	rows, err := r.db.Query(ctx, `
		SELECT cr.id, cr.user_id, cr.listing_id, cr.quantity, cr.expires_at, cr.status, cr.created_at,
		       c.id, c.name, lcv.variant_name, cr.listing_id,
		       il.language, il.price_usd, il.price_cop,
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
			&res.ID, &res.UserID, &res.ListingID, &res.Quantity, &res.ExpiresAt, &res.Status, &res.CreatedAt,
			&res.CardID, &res.CardName, &res.VariantName, &res.ListingID,
			&res.Language, &res.PriceUSD, &res.PriceCOP,
			&res.CardImage,
		); err != nil {
			return nil, fmt.Errorf("escanear reserva por ids: %w", err)
		}
		out = append(out, res)
	}
	return out, rows.Err()
}

func (r *ReservationRepository) Confirm(ctx context.Context, userID int64, ids []int64) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

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
		id        int64
		quantity  int
		details   reservation.Reservation
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

	return tx.Commit(ctx)
}

func (r *ReservationRepository) Remove(ctx context.Context, userID, id int64) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var listingID int64
	var quantity int
	var details reservation.Reservation
	err = tx.QueryRow(ctx, `
		SELECT cr.listing_id, cr.quantity,
		       c.id, c.name, cv.variant_name, il.language, il.price_usd, il.price_cop
		FROM cart_reservations cr
		JOIN inventory_listings il ON il.id = cr.listing_id
		JOIN card_variants cv ON cv.id = il.variant_id
		JOIN cards c ON c.id = cv.card_id
		WHERE cr.id = $1 AND cr.user_id = $2 AND cr.status = 'active'
		FOR UPDATE
	`, id, userID).Scan(
		&listingID, &quantity,
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

	if _, err := tx.Exec(ctx, `
		UPDATE inventory_listings SET quantity = quantity + $2, updated_at = now() WHERE id = $1
	`, listingID, quantity); err != nil {
		return fmt.Errorf("restaurar stock del listing: %w", err)
	}

	// Historico: la reserva se devolvió al stock.
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
	}); err != nil {
		return err
	}

	return tx.Commit(ctx)
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
		if _, err := tx.Exec(ctx, `
			UPDATE inventory_listings SET quantity = quantity + $2, updated_at = now() WHERE id = $1
		`, e.listingID, e.quantity); err != nil {
			return 0, fmt.Errorf("restaurar stock expirado: %w", err)
		}
		// Historico: por vencimiento, la reserva se devuelve al stock.
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
		}); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return int64(len(exps)), nil
}

func (r *ReservationRepository) AvailableStock(ctx context.Context, cardID int64, language string) (int, error) {
	var stock int
	err := r.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(il.quantity) - COALESCE((
			SELECT SUM(cr.quantity)
			FROM cart_reservations cr
			JOIN inventory_listings crl ON crl.id = cr.listing_id
			JOIN card_variants crv ON crv.id = crl.variant_id
			WHERE crv.card_id = $1 AND crl.language = $2 AND cr.status = 'active'
		), 0), 0)::int
		FROM inventory_listings il
		JOIN card_variants lcv ON lcv.id = il.variant_id
		WHERE lcv.card_id = $1 AND il.language = $2 AND il.status = 'active' AND il.quantity > 0
	`, cardID, language).Scan(&stock)
	if err != nil {
		return 0, fmt.Errorf("calcular stock disponible: %w", err)
	}
	if stock < 0 {
		stock = 0
	}
	return stock, nil
}

// logReservation registra un cambio de estado de una reserva en el historial.
// Resuelve el nombre y correo del cliente desde la tabla `users` en el momento
// del evento, para que queden guardados aunque la cuenta cambie después.
func logReservation(ctx context.Context, tx pgx.Tx, l reservation.ReservationLog) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO cart_reservation_logs
			(reservation_id, user_id, customer_name, customer_email,
			 card_id, card_name, variant_name, language, quantity,
			 price_usd, price_cop, status)
		SELECT $1, $2,
		       TRIM(COALESCE(u.first_name,'') || ' ' || COALESCE(u.last_name,'')), COALESCE(u.email,''),
		       $3, $4, $5, $6, $7, $8, $9, $10
		FROM users u
		WHERE u.id = $2
	`, l.ReservationID, l.UserID, l.CardID, l.CardName, l.VariantName, l.Language,
		l.Quantity, l.PriceUSD, l.PriceCOP, l.Status); err != nil {
		return fmt.Errorf("registrar log de reserva: %w", err)
	}
	return nil
}

// ListReservationLogs devuelve el historial de reservas, más recientes
// primero, con paginación y filtro opcional por estado.
func (r *ReservationRepository) ListReservationLogs(ctx context.Context, status string, limit, offset int) ([]reservation.ReservationLog, error) {
	q := `
		SELECT id, reservation_id, user_id, customer_name, customer_email,
		       card_id, card_name, variant_name, language, quantity,
		       price_usd, price_cop, status, created_at
		FROM cart_reservation_logs
	`
	var args []any
	if status != "" {
		q += ` WHERE status = $1`
		args = append(args, status)
	}
	q += ` ORDER BY created_at DESC, id DESC`
	if limit > 0 {
		args = append(args, limit, offset)
		q += fmt.Sprintf(` LIMIT $%d OFFSET $%d`, len(args)-1, len(args))
	}

	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("listar logs de reserva: %w", err)
	}
	defer rows.Close()

	var out []reservation.ReservationLog
	for rows.Next() {
		var l reservation.ReservationLog
		if err := rows.Scan(
			&l.ID, &l.ReservationID, &l.UserID, &l.CustomerName, &l.CustomerEmail,
			&l.CardID, &l.CardName, &l.VariantName, &l.Language, &l.Quantity,
			&l.PriceUSD, &l.PriceCOP, &l.Status, &l.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("escanear log de reserva: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
