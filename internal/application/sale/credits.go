package sale

import (
	"context"
	"strings"
	"time"

	"trample-back/internal/domain/sale"
	"trample-back/internal/ports/out"
)

// StoreCreditsUseCase gestiona las cuentas por cobrar de la tienda: crear el
// crédito al entregar un pedido, registrar abonos y listar la deuda.
type StoreCreditsUseCase struct {
	sales out.SaleRepository
	now   func() time.Time
}

func NewStoreCreditsUseCase(sales out.SaleRepository) *StoreCreditsUseCase {
	return &StoreCreditsUseCase{sales: sales, now: time.Now}
}

// Create convierte un pedido pendiente de recogida en crédito. El abono inicial
// no puede superar el total y la fecha límite no puede estar en el pasado.
func (uc *StoreCreditsUseCase) Create(ctx context.Context, in sale.NewCreditInput) (sale.StoreCredit, error) {
	if in.SaleID <= 0 || in.InitialCOP < 0 || in.DueDate.IsZero() {
		return sale.StoreCredit{}, sale.ErrInvalidInput
	}
	if beforeToday(in.DueDate, uc.now()) {
		return sale.StoreCredit{}, sale.ErrInvalidInput
	}
	in.Notes = strings.TrimSpace(in.Notes)
	return uc.sales.CreateStoreCredit(ctx, in)
}

func (uc *StoreCreditsUseCase) AddPayment(ctx context.Context, creditID, amountCOP int64, note, createdBy string) (sale.StoreCredit, error) {
	if creditID <= 0 || amountCOP <= 0 {
		return sale.StoreCredit{}, sale.ErrCreditPayment
	}
	return uc.sales.RegisterCreditPayment(ctx, creditID, amountCOP, strings.TrimSpace(note), createdBy)
}

func (uc *StoreCreditsUseCase) Get(ctx context.Context, id int64) (sale.StoreCredit, error) {
	if id <= 0 {
		return sale.StoreCredit{}, sale.ErrInvalidInput
	}
	return uc.sales.FindCreditByID(ctx, id)
}

func (uc *StoreCreditsUseCase) List(ctx context.Context, f sale.CreditFilter) ([]sale.StoreCredit, int, error) {
	switch f.Status {
	case "", sale.CreditOpen, sale.CreditPaid, sale.CreditCancelled:
	default:
		return nil, 0, sale.ErrInvalidInput
	}
	if f.Limit <= 0 {
		f.Limit = defaultSearchLimit
	}
	f.Limit = min(f.Limit, maxSearchLimit)
	f.Offset = max(f.Offset, 0)
	return uc.sales.ListCredits(ctx, f)
}

// beforeToday compara solo la fecha civil en hora de Colombia: un crédito que
// vence hoy todavía se puede crear. La fecha límite no se convierte de zona
// porque representa un día completo, no un instante.
func beforeToday(due, now time.Time) bool {
	zone := time.FixedZone("COT", -5*60*60)
	dy, dm, dd := due.Date()
	n := now.In(zone)
	ny, nm, nd := n.Date()
	return time.Date(dy, dm, dd, 0, 0, 0, 0, zone).Before(time.Date(ny, nm, nd, 0, 0, 0, 0, zone))
}
