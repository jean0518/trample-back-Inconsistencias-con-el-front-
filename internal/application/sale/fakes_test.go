package sale

import (
	"context"
	"errors"
	"time"

	"trample-back/internal/domain/payment"
	"trample-back/internal/domain/reservation"
	"trample-back/internal/domain/sale"
)

// fakes son dobles en memoria de los puertos que usa el checkout. Permiten
// comprobar lo que NO se hace (por ejemplo, que abrir el checkout no cree
// ninguna venta), que un doble en memoria no puede.

// fakeReservations devuelve las reservas que se le configureen y registra las
// llamadas que alteran el stock.
type fakeReservations struct {
	list      []reservation.Reservation
	listErr   error
	activeErr error

	confirmed  [][]int64
	extended   []extendCall
	confirmErr error
	extendErr  error
}

type extendCall struct {
	userID  string
	ids     []int64
	minutes int
}

func (f *fakeReservations) ListActiveByUserAndIDs(_ context.Context, userID string, ids []int64) ([]reservation.Reservation, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	want := map[int64]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var out []reservation.Reservation
	for _, r := range f.list {
		if r.UserID == userID && want[r.ID] {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeReservations) Confirm(_ context.Context, userID string, ids []int64) error {
	f.confirmed = append(f.confirmed, ids)
	return f.confirmErr
}

func (f *fakeReservations) ExtendExpiry(_ context.Context, userID string, ids []int64, minutes int) error {
	f.extended = append(f.extended, extendCall{userID: userID, ids: ids, minutes: minutes})
	return f.extendErr
}

func (f *fakeReservations) Reserve(context.Context, reservation.ReserveInput, int) (reservation.Reservation, error) {
	return reservation.Reservation{}, errors.New("no implementado en el fake")
}
func (f *fakeReservations) ListActiveByUser(context.Context, string) ([]reservation.Reservation, error) {
	return nil, errors.New("no implementado en el fake")
}
func (f *fakeReservations) Remove(context.Context, string, int64) error {
	return errors.New("no implementado en el fake")
}
func (f *fakeReservations) ReleaseExpired(context.Context) (int64, error) {
	return 0, errors.New("no implementado en el fake")
}
func (f *fakeReservations) FindListingForReserve(context.Context, int64, string, string) (reservation.ListingInfo, error) {
	return reservation.ListingInfo{}, errors.New("no implementado en el fake")
}
func (f *fakeReservations) ListReservationLogs(context.Context, string, int, int) ([]reservation.ReservationLog, error) {
	return nil, errors.New("no implementado en el fake")
}

// fakeSales lleva la cuenta de qué se persistió. Las ventas creadas quedan en
// `sales` y los checkouts en `checkouts`, de modo que los tests puedan afirmar
// que pulsar "Pagar" no registró ningún pedido.
type fakeSales struct {
	checkouts   map[string]sale.CheckoutIntent
	open        map[string]string // userID -> reference
	sales       map[int64]sale.Sale
	nextSaleID  int64
	eventIDs    map[string]bool
	approveErr  error
	rejectErr   error
	createCalls int
	closeCalls  int
	approveRefs []string
	rejectRefs  []string
	expired     int64
	expireGrace time.Duration
	flagged     []string
	forgotten   []string
	paidAmounts []int64
	// confirmedIDs y confirmErr modelan la confirmación de reservas que Create
	// hace en la misma transacción que la venta.
	confirmedIDs [][]int64
	confirmErr   error
	// raceWinner simula que otro "Pagar" simultáneo ganó el índice único: al
	// crear el checkout, el repositorio devuelve ErrCheckoutConflict y queda
	// abierto el del ganador.
	raceWinner *sale.CheckoutIntent
}

func newFakeSales() *fakeSales {
	return &fakeSales{
		checkouts: map[string]sale.CheckoutIntent{},
		open:      map[string]string{},
		sales:     map[int64]sale.Sale{},
		eventIDs:  map[string]bool{},
	}
}

func (f *fakeSales) Create(_ context.Context, s sale.Sale) (sale.Sale, error) {
	if f.confirmErr != nil {
		return sale.Sale{}, f.confirmErr
	}
	f.confirmedIDs = append(f.confirmedIDs, s.ReservationIDs)
	f.nextSaleID++
	s.ID = f.nextSaleID
	f.sales[s.ID] = s
	return s, nil
}

func (f *fakeSales) CreateCheckout(_ context.Context, c sale.CheckoutIntent) (sale.CheckoutIntent, error) {
	f.createCalls++
	if f.raceWinner != nil {
		w := *f.raceWinner
		w.Status = sale.CheckoutOpen
		f.checkouts[w.Reference] = w
		f.open[w.UserID] = w.Reference
		f.raceWinner = nil
		return sale.CheckoutIntent{}, sale.ErrCheckoutConflict
	}
	// El repositorio real es quien fija el estado inicial.
	c.Status = sale.CheckoutOpen
	f.checkouts[c.Reference] = c
	f.open[c.UserID] = c.Reference
	return c, nil
}

func (f *fakeSales) FindOpenCheckoutByUser(_ context.Context, userID string) (sale.CheckoutIntent, error) {
	ref, ok := f.open[userID]
	if !ok {
		return sale.CheckoutIntent{}, sale.ErrPaymentNotFound
	}
	c, ok := f.checkouts[ref]
	if !ok || c.Status != sale.CheckoutOpen {
		return sale.CheckoutIntent{}, sale.ErrPaymentNotFound
	}
	return c, nil
}

func (f *fakeSales) ExtendCheckout(_ context.Context, reference string, expiresAt time.Time) (sale.CheckoutIntent, error) {
	c, ok := f.checkouts[reference]
	if !ok || c.Status != sale.CheckoutOpen {
		return sale.CheckoutIntent{}, sale.ErrPaymentNotFound
	}
	c.ExpiresAt = expiresAt
	f.checkouts[reference] = c
	return c, nil
}

func (f *fakeSales) FindCheckoutByReference(_ context.Context, reference string) (sale.CheckoutIntent, error) {
	c, ok := f.checkouts[reference]
	if !ok {
		return sale.CheckoutIntent{}, sale.ErrPaymentNotFound
	}
	return c, nil
}

func (f *fakeSales) CloseOpenCheckouts(_ context.Context, userID string) (int64, error) {
	f.closeCalls++
	var n int64
	for ref, c := range f.checkouts {
		if c.UserID == userID && c.Status == sale.CheckoutOpen {
			c.Status = sale.CheckoutExpired
			f.checkouts[ref] = c
			n++
		}
	}
	return n, nil
}

func (f *fakeSales) ApprovePayment(_ context.Context, reference, paymentID string, paidCOP int64) (sale.Sale, error) {
	f.approveRefs = append(f.approveRefs, reference)
	f.paidAmounts = append(f.paidAmounts, paidCOP)
	if f.approveErr != nil {
		return sale.Sale{}, f.approveErr
	}
	c, ok := f.checkouts[reference]
	if !ok {
		return sale.Sale{}, sale.ErrPaymentNotFound
	}
	if c.Status == sale.CheckoutPaid && c.SaleID != 0 {
		return f.sales[c.SaleID], nil
	}
	f.nextSaleID++
	s := sale.Sale{
		ID:               f.nextSaleID,
		UserID:           c.UserID,
		TotalCOP:         c.TotalCOP,
		Fulfillment:      c.Fulfillment,
		PaymentMethod:    sale.PaymentBold,
		Status:           sale.StatusPaid,
		PaymentReference: c.Reference,
		BoldPaymentID:    paymentID,
		Items:            c.Items,
		// Igual que el repositorio real: un pago que llega a un checkout que ya no
		// estaba abierto, o por otro monto, se revisa a mano.
		RequiresReview: c.Status != sale.CheckoutOpen || (paidCOP > 0 && paidCOP != c.TotalCOP),
	}
	f.sales[s.ID] = s
	c.Status = sale.CheckoutPaid
	c.SaleID = s.ID
	c.BoldPaymentID = paymentID
	f.checkouts[reference] = c
	return s, nil
}

func (f *fakeSales) RejectPayment(_ context.Context, reference string) (sale.CheckoutIntent, error) {
	f.rejectRefs = append(f.rejectRefs, reference)
	if f.rejectErr != nil {
		return sale.CheckoutIntent{}, f.rejectErr
	}
	c, ok := f.checkouts[reference]
	if !ok {
		return sale.CheckoutIntent{}, sale.ErrPaymentNotFound
	}
	if c.Status == sale.CheckoutOpen {
		c.Status = sale.CheckoutFailed
		f.checkouts[reference] = c
		delete(f.open, c.UserID)
	}
	return c, nil
}

func (f *fakeSales) ExpireCheckouts(_ context.Context, grace time.Duration) (int64, error) {
	f.expireGrace = grace
	return f.expired, nil
}

// ListUnsettledCheckouts imita al repositorio: abiertos ya vencidos y cerrados
// por vencimiento o rechazo.
func (f *fakeSales) ListUnsettledCheckouts(_ context.Context, limit int) ([]string, error) {
	var refs []string
	for ref, c := range f.checkouts {
		openExpired := c.Status == sale.CheckoutOpen && !c.ExpiresAt.After(time.Now())
		if openExpired || c.Status == sale.CheckoutExpired || c.Status == sale.CheckoutFailed {
			refs = append(refs, ref)
		}
	}
	if len(refs) > limit {
		refs = refs[:limit]
	}
	return refs, nil
}

func (f *fakeSales) ForgetPaymentEvent(_ context.Context, eventID string) error {
	f.forgotten = append(f.forgotten, eventID)
	delete(f.eventIDs, eventID)
	return nil
}

func (f *fakeSales) FlagSaleForReview(_ context.Context, reference string) error {
	f.flagged = append(f.flagged, reference)
	return nil
}

func (f *fakeSales) RecordPaymentEvent(_ context.Context, evt payment.Event) (bool, error) {
	if f.eventIDs[evt.ID] {
		return false, nil
	}
	f.eventIDs[evt.ID] = true
	return true, nil
}

func (f *fakeSales) FindByID(_ context.Context, id int64) (sale.Sale, error) {
	s, ok := f.sales[id]
	if !ok {
		return sale.Sale{}, sale.ErrPaymentNotFound
	}
	return s, nil
}

func (f *fakeSales) ListByUser(context.Context, string, int, int) ([]sale.Sale, error) {
	return nil, errors.New("no implementado en el fake")
}
func (f *fakeSales) ListAll(context.Context, int, int) ([]sale.Sale, error) {
	return nil, errors.New("no implementado en el fake")
}
func (f *fakeSales) Stats(context.Context, string, time.Time) ([]sale.StatBucket, error) {
	return nil, errors.New("no implementado en el fake")
}

// fakeGateway simula la pasarela: firma el checkout y devuelve el estado que se
// le programe.
type fakeGateway struct {
	configured bool
	status     payment.Status
	statusErr  error
	requests   []payment.CheckoutRequest
}

func (f *fakeGateway) Configured() bool { return f.configured }

func (f *fakeGateway) BuildCheckout(req payment.CheckoutRequest) payment.Checkout {
	f.requests = append(f.requests, req)
	return payment.Checkout{
		Reference:          req.Reference,
		AmountCOP:          req.AmountCOP,
		Currency:           payment.CurrencyCOP,
		IntegritySignature: "firma",
		IdentityKey:        "llave-publica",
		RedirectionURL:     req.RedirectionURL,
		OriginURL:          req.OriginURL,
		Description:        req.Description,
		ExpirationNS:       req.ExpiresAt.UnixNano(),
	}
}

func (f *fakeGateway) GetStatus(context.Context, string) (payment.Status, error) {
	if !f.configured {
		return payment.Status{}, payment.ErrNotConfigured
	}
	return f.status, f.statusErr
}

// cartReservation construye una reserva de carrito para los tests.
func cartReservation(id int64, userID string, priceCOP float64, priceUSD float64, qty int) reservation.Reservation {
	return reservation.Reservation{
		ID:        id,
		UserID:    userID,
		ListingID: id * 10,
		CardID:    id,
		CardName:  "Carta",
		Language:  "Inglés",
		Quantity:  qty,
		PriceCOP:  priceCOP,
		PriceUSD:  priceUSD,
	}
}
