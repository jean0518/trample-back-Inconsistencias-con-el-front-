package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"trample-back/internal/domain/reservation"
	"trample-back/internal/domain/sale"
)

// creditSelect lee una cuenta por cobrar con los datos del cliente y del admin
// que la autorizó.
const creditSelect = `
	SELECT c.id, c.sale_id, c.user_id::text,
	       COALESCE(trim(concat_ws(' ', u.first_name, u.last_name)), ''), COALESCE(u.email, ''),
	       c.total_cop, c.initial_cop, c.paid_cop, c.balance_cop, c.due_date,
	       c.notes, COALESCE(c.created_by::text, ''),
	       COALESCE(trim(concat_ws(' ', a.first_name, a.last_name)), ''),
	       c.status, c.created_at, c.updated_at
	FROM store_credits c
	JOIN users u ON u.id = c.user_id
	LEFT JOIN users a ON a.id = c.created_by
`

func scanCredit(row pgx.Row) (sale.StoreCredit, error) {
	var c sale.StoreCredit
	err := row.Scan(
		&c.ID, &c.SaleID, &c.UserID, &c.CustomerName, &c.CustomerEmail,
		&c.TotalCOP, &c.InitialCOP, &c.PaidCOP, &c.BalanceCOP, &c.DueDate,
		&c.Notes, &c.CreatedBy, &c.CreatedByName,
		&c.Status, &c.CreatedAt, &c.UpdatedAt,
	)
	return c, err
}

func (r *SaleRepository) loadCreditPayments(ctx context.Context, c *sale.StoreCredit) error {
	rows, err := r.db.Query(ctx, `
		SELECT id, credit_id, amount_cop, note, COALESCE(created_by::text, ''), created_at
		  FROM store_credit_payments
		 WHERE credit_id = $1
		 ORDER BY created_at, id
	`, c.ID)
	if err != nil {
		return err
	}
	defer rows.Close()

	var payments []sale.CreditPayment
	for rows.Next() {
		var p sale.CreditPayment
		if err := rows.Scan(&p.ID, &p.CreditID, &p.AmountCOP, &p.Note, &p.CreatedBy, &p.CreatedAt); err != nil {
			return err
		}
		payments = append(payments, p)
	}
	c.Payments = payments
	return rows.Err()
}

// loadCreditForSale adjunta el crédito al pedido si existe. No es un error que
// el pedido no tenga crédito.
func (r *SaleRepository) loadCreditForSale(ctx context.Context, s *sale.Sale) error {
	c, err := scanCredit(r.db.QueryRow(ctx, creditSelect+` WHERE c.sale_id = $1`, s.ID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("buscar crédito del pedido %d: %w", s.ID, err)
	}
	if err := r.loadCreditPayments(ctx, &c); err != nil {
		return fmt.Errorf("cargar abonos del crédito del pedido %d: %w", s.ID, err)
	}
	s.Credit = &c
	return nil
}

// FindCreditByID devuelve el crédito con sus abonos.
func (r *SaleRepository) FindCreditByID(ctx context.Context, id int64) (sale.StoreCredit, error) {
	c, err := scanCredit(r.db.QueryRow(ctx, creditSelect+` WHERE c.id = $1`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sale.StoreCredit{}, sale.ErrCreditNotFound
		}
		return sale.StoreCredit{}, fmt.Errorf("buscar crédito %d: %w", id, err)
	}
	if err := r.loadCreditPayments(ctx, &c); err != nil {
		return sale.StoreCredit{}, fmt.Errorf("cargar abonos del crédito %d: %w", id, err)
	}
	return c, nil
}

// FindCreditBySale devuelve el crédito de un pedido, si existe.
func (r *SaleRepository) FindCreditBySale(ctx context.Context, saleID int64) (sale.StoreCredit, error) {
	c, err := scanCredit(r.db.QueryRow(ctx, creditSelect+` WHERE c.sale_id = $1`, saleID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sale.StoreCredit{}, sale.ErrCreditNotFound
		}
		return sale.StoreCredit{}, fmt.Errorf("buscar crédito del pedido %d: %w", saleID, err)
	}
	if err := r.loadCreditPayments(ctx, &c); err != nil {
		return sale.StoreCredit{}, fmt.Errorf("cargar abonos del crédito del pedido %d: %w", saleID, err)
	}
	return c, nil
}

// loadCreditsForSales adjunta el crédito a cada pedido que tenga uno. Se hace en
// una sola consulta para no degradar el listado del panel.
func (r *SaleRepository) loadCreditsForSales(ctx context.Context, sales []sale.Sale) error {
	if len(sales) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(sales))
	index := make(map[int64]int, len(sales))
	for i, s := range sales {
		ids = append(ids, s.ID)
		index[s.ID] = i
	}
	rows, err := r.db.Query(ctx, creditSelect+` WHERE c.sale_id = ANY($1)`, ids)
	if err != nil {
		return fmt.Errorf("cargar créditos de los pedidos: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		c, err := scanCredit(rows)
		if err != nil {
			return err
		}
		if i, ok := index[c.SaleID]; ok {
			credit := c
			sales[i].Credit = &credit
		}
	}
	return rows.Err()
}

// CreateStoreCredit convierte un pedido pendiente en una cuenta por cobrar. La
// mercancía se entrega (las reservas pasan a 'sold') y el pedido queda en
// 'credit' hasta que el saldo llegue a cero.
func (r *SaleRepository) CreateStoreCredit(ctx context.Context, input sale.NewCreditInput) (sale.StoreCredit, error) {
	if input.InitialCOP < 0 || input.DueDate.IsZero() {
		return sale.StoreCredit{}, sale.ErrInvalidInput
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return sale.StoreCredit{}, err
	}
	defer tx.Rollback(ctx)

	var (
		userID string
		total  int64
		status string
	)
	err = tx.QueryRow(ctx, `
		SELECT user_id::text, total_cop, status
		  FROM sales WHERE id = $1 FOR UPDATE
	`, input.SaleID).Scan(&userID, &total, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return sale.StoreCredit{}, sale.ErrPaymentNotFound
	}
	if err != nil {
		return sale.StoreCredit{}, fmt.Errorf("buscar pedido %d para crédito: %w", input.SaleID, err)
	}
	if status != sale.StatusPendingPickup {
		return sale.StoreCredit{}, sale.ErrNotPendingPickup
	}
	if input.InitialCOP > total {
		return sale.StoreCredit{}, sale.ErrCreditPayment
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM store_credits WHERE sale_id = $1)`, input.SaleID).Scan(&exists); err != nil {
		return sale.StoreCredit{}, err
	}
	if exists {
		return sale.StoreCredit{}, sale.ErrCreditExists
	}

	balance := total - input.InitialCOP
	creditStatus := sale.CreditOpen
	saleStatus := sale.StatusCredit
	if balance == 0 {
		creditStatus = sale.CreditPaid
		saleStatus = sale.StatusPaid
	}

	var creditID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO store_credits
			(sale_id, user_id, total_cop, initial_cop, paid_cop, balance_cop, due_date, notes, created_by, status)
		VALUES ($1, $2, $3, $4, $4, $5, $6, $7, $8, $9)
		RETURNING id
	`, input.SaleID, userID, total, input.InitialCOP, balance, input.DueDate, input.Notes, nullableStr(input.CreatedBy), creditStatus).Scan(&creditID)
	if err != nil {
		return sale.StoreCredit{}, fmt.Errorf("crear crédito del pedido %d: %w", input.SaleID, err)
	}
	if input.InitialCOP > 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO store_credit_payments (credit_id, amount_cop, note, created_by)
			VALUES ($1, $2, 'Abono inicial', $3)
		`, creditID, input.InitialCOP, nullableStr(input.CreatedBy)); err != nil {
			return sale.StoreCredit{}, fmt.Errorf("registrar abono inicial: %w", err)
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE sales
		   SET status = $2, payment_method = $3, delivered_at = now(),
		       paid_at = CASE WHEN $2 = $4 THEN now() ELSE paid_at END,
		       cancel_requested_at = NULL, requires_review = false
		 WHERE id = $1
	`, input.SaleID, saleStatus, sale.PaymentCredit, sale.StatusPaid); err != nil {
		return sale.StoreCredit{}, fmt.Errorf("marcar pedido %d como crédito: %w", input.SaleID, err)
	}
	// El cliente ya se llevó la mercancía: las reservas dejan de estar apartadas
	// y pasan a vendidas, aunque el pago quede pendiente en la cuenta por cobrar.
	if err := logSaleCardReservationsTx(ctx, tx, input.SaleID, reservation.LogSold, ""); err != nil {
		return sale.StoreCredit{}, err
	}
	if err := logSaleProductReservationsTx(ctx, tx, input.SaleID, reservation.LogSold, ""); err != nil {
		return sale.StoreCredit{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sale.StoreCredit{}, err
	}
	return r.FindCreditByID(ctx, creditID)
}

// RegisterCreditPayment registra un abono al crédito. Cuando el saldo llega a
// cero, el crédito y el pedido pasan a 'paid'.
func (r *SaleRepository) RegisterCreditPayment(ctx context.Context, creditID, amount int64, note, createdBy string) (sale.StoreCredit, error) {
	if amount <= 0 {
		return sale.StoreCredit{}, sale.ErrCreditPayment
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return sale.StoreCredit{}, err
	}
	defer tx.Rollback(ctx)

	var (
		saleID  int64
		status  string
		paid    int64
		balance int64
	)
	err = tx.QueryRow(ctx, `
		SELECT sale_id, status, paid_cop, balance_cop
		  FROM store_credits WHERE id = $1 FOR UPDATE
	`, creditID).Scan(&saleID, &status, &paid, &balance)
	if errors.Is(err, pgx.ErrNoRows) {
		return sale.StoreCredit{}, sale.ErrCreditNotFound
	}
	if err != nil {
		return sale.StoreCredit{}, fmt.Errorf("buscar crédito %d: %w", creditID, err)
	}
	if status != sale.CreditOpen {
		return sale.StoreCredit{}, sale.ErrCreditNotOpen
	}
	if amount > balance {
		return sale.StoreCredit{}, sale.ErrCreditPayment
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO store_credit_payments (credit_id, amount_cop, note, created_by)
		VALUES ($1, $2, $3, $4)
	`, creditID, amount, note, nullableStr(createdBy)); err != nil {
		return sale.StoreCredit{}, fmt.Errorf("registrar abono: %w", err)
	}
	newPaid := paid + amount
	newBalance := balance - amount
	newStatus := sale.CreditOpen
	if newBalance == 0 {
		newStatus = sale.CreditPaid
	}
	if _, err := tx.Exec(ctx, `
		UPDATE store_credits
		   SET paid_cop = $2, balance_cop = $3, status = $4, updated_at = now()
		 WHERE id = $1
	`, creditID, newPaid, newBalance, newStatus); err != nil {
		return sale.StoreCredit{}, fmt.Errorf("actualizar crédito %d: %w", creditID, err)
	}
	if newBalance == 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE sales
			   SET status = $2, paid_at = now(), delivered_at = COALESCE(delivered_at, now())
			 WHERE id = $1 AND status = $3
		`, saleID, sale.StatusPaid, sale.StatusCredit); err != nil {
			return sale.StoreCredit{}, fmt.Errorf("cerrar pedido %d al pagar el crédito: %w", saleID, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return sale.StoreCredit{}, err
	}
	return r.FindCreditByID(ctx, creditID)
}

// ListCredits devuelve las cuentas por cobrar con filtros y el total sin paginar.
func (r *SaleRepository) ListCredits(ctx context.Context, f sale.CreditFilter) ([]sale.StoreCredit, int, error) {
	var (
		conds []string
		args  []any
	)
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if f.Status != "" {
		conds = append(conds, "c.status = "+arg(f.Status))
	}
	if f.Overdue {
		conds = append(conds, "c.status = 'open' AND c.balance_cop > 0 AND c.due_date < (now() AT TIME ZONE 'America/Bogota')::date")
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		like := arg("%" + escapeLike(q) + "%")
		parts := []string{
			"trim(concat_ws(' ', u.first_name, u.last_name)) ILIKE " + like,
			"u.email ILIKE " + like,
			"c.notes ILIKE " + like,
		}
		if id, err := strconv.ParseInt(strings.TrimPrefix(q, "#"), 10, 64); err == nil {
			parts = append(parts, "c.sale_id = "+arg(id), "c.id = "+arg(id))
		}
		conds = append(conds, "("+strings.Join(parts, " OR ")+")")
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	from := ` FROM store_credits c JOIN users u ON u.id = c.user_id LEFT JOIN users a ON a.id = c.created_by`

	var total int
	if err := r.db.QueryRow(ctx, `SELECT count(*)`+from+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("contar créditos: %w", err)
	}

	n := len(args)
	rows, err := r.db.Query(ctx, creditSelect+where+
		fmt.Sprintf(` ORDER BY c.due_date, c.id DESC LIMIT $%d OFFSET $%d`, n+1, n+2),
		append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("listar créditos: %w", err)
	}
	defer rows.Close()

	var out []sale.StoreCredit
	for rows.Next() {
		c, err := scanCredit(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("escanear crédito: %w", err)
		}
		out = append(out, c)
	}
	return out, total, rows.Err()
}
