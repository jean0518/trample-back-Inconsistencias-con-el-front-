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
func (f *fakeReservations) UpdateQuantity(context.Context, string, int64, int) (reservation.Reservation, error) {
	return reservation.Reservation{}, errors.New("no implementado en el fake")
}
func (f *fakeReservations) ReleaseExpired(context.Context) (int64, error) {
	return 0, errors.New("no implementado en el fake")
}
func (f *fakeReservations) FindListingForReserve(context.Context, int64, string, string) (reservation.ListingInfo, error) {
	return reservation.ListingInfo{}, errors.New("no implementado en el fake")
}
func (f *fakeReservations) ListReservationLogs(context.Context, reservation.LogFilter) ([]reservation.ReservationLog, int, error) {
	return nil, 0, errors.New("no implementado en el fake")
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
	expired     []sale.CheckoutIntent
	// pendingCalls cuenta las veces que se registró un pago en proceso.
	pendingCalls int
	expireGrace  time.Duration
	flagged      []string
	forgotten    []string
	restocked    []int64 // pedidos cancelados cuyo stock volvió al inventario
	paidAmounts  []int64
	// confirmedIDs y confirmErr modelan la confirmación de reservas que Create
	// hace en la misma transacción que la venta.
	confirmedIDs [][]int64
	confirmErr   error
	// raceWinner simula que otro "Pagar" simultáneo ganó el índice único: al
	// crear el checkout, el repositorio devuelve ErrCheckoutConflict y queda
	// abierto el del ganador.
	raceWinner *sale.CheckoutIntent
	// reminded son los pedidos a los que ya se les recordó la recogida.
	reminded map[int64]bool
	// lastFilter es el último filtro recibido por Search o CountByStage.
	lastFilter sale.ListFilter
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
	// Como el repositorio: solo la llamada que crea el pedido lo marca.
	s.JustCreated = true
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

func (f *fakeSales) ExpireCheckouts(_ context.Context, grace time.Duration) ([]sale.CheckoutIntent, error) {
	f.expireGrace = grace
	return f.expired, nil
}

// MarkCheckoutPending imita al repositorio: solo la primera vez, con el
// checkout abierto, alarga la ventana a `window` desde ahora.
func (f *fakeSales) MarkCheckoutPending(_ context.Context, reference string, window, _ time.Duration) (sale.CheckoutIntent, bool, error) {
	f.pendingCalls++
	c, ok := f.checkouts[reference]
	if !ok {
		return sale.CheckoutIntent{}, false, sale.ErrPaymentNotFound
	}
	if c.Status != sale.CheckoutOpen || !c.PaymentPendingAt.IsZero() {
		return c, false, nil
	}
	c.PaymentPendingAt = time.Now()
	if until := time.Now().Add(window); until.After(c.ExpiresAt) {
		c.ExpiresAt = until
	}
	f.checkouts[reference] = c
	return c, true, nil
}

func (f *fakeSales) RequestPickupCancel(_ context.Context, id int64, userID string) (sale.Sale, error) {
	s, ok := f.sales[id]
	switch {
	case !ok:
		return sale.Sale{}, sale.ErrPaymentNotFound
	case s.UserID != userID:
		return sale.Sale{}, sale.ErrForbidden
	case s.Status != sale.StatusPendingPickup:
		return sale.Sale{}, sale.ErrNotPendingPickup
	case !s.CancelRequestedAt.IsZero():
		return sale.Sale{}, sale.ErrCancelAlreadyRequested
	}
	s.CancelRequestedAt = time.Now()
	f.sales[id] = s
	return s, nil
}

func (f *fakeSales) RejectPickupCancel(_ context.Context, id int64) (sale.Sale, error) {
	s, ok := f.sales[id]
	switch {
	case !ok:
		return sale.Sale{}, sale.ErrPaymentNotFound
	case s.Status != sale.StatusPendingPickup:
		return sale.Sale{}, sale.ErrNotPendingPickup
	case s.CancelRequestedAt.IsZero():
		return sale.Sale{}, sale.ErrNoCancelRequest
	}
	s.CancelRequestedAt = time.Time{}
	f.sales[id] = s
	return s, nil
}

// ClaimPickupReminders devuelve una sola vez los pedidos pendientes cuyo plazo
// vence en menos de 39 horas.
func (f *fakeSales) ClaimPickupReminders(_ context.Context, now time.Time) ([]sale.Sale, error) {
	if f.reminded == nil {
		f.reminded = map[int64]bool{}
	}
	var out []sale.Sale
	for id, s := range f.sales {
		due := s.Status == sale.StatusPendingPickup && s.CancelRequestedAt.IsZero() &&
			s.PickupDeadline.After(now) && !s.PickupDeadline.Add(-39*time.Hour).After(now)
		if due && !f.reminded[id] {
			f.reminded[id] = true
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeSales) MarkShipped(_ context.Context, id int64, carrier, tracking string) (sale.Sale, error) {
	s, ok := f.sales[id]
	if !ok {
		return sale.Sale{}, sale.ErrPaymentNotFound
	}
	if s.Status != sale.StatusPaid || s.Fulfillment != sale.FulfillmentShipping || s.RequiresReview || !s.ShippedAt.IsZero() {
		return sale.Sale{}, sale.ErrNotShippable
	}
	s.ShippedAt, s.Carrier, s.TrackingNumber = time.Now(), carrier, tracking
	f.sales[id] = s
	return s, nil
}

func (f *fakeSales) MarkDelivered(_ context.Context, id int64) (sale.Sale, error) {
	s, ok := f.sales[id]
	if !ok {
		return sale.Sale{}, sale.ErrPaymentNotFound
	}
	if s.ShippedAt.IsZero() || !s.DeliveredAt.IsZero() {
		return sale.Sale{}, sale.ErrNotShippable
	}
	s.DeliveredAt = time.Now()
	f.sales[id] = s
	return s, nil
}

func (f *fakeSales) ConfirmReview(_ context.Context, id int64) (sale.Sale, error) {
	s, ok := f.sales[id]
	if !ok {
		return sale.Sale{}, sale.ErrPaymentNotFound
	}
	if !s.RequiresReview {
		return sale.Sale{}, sale.ErrNotInReview
	}
	s.RequiresReview = false
	f.sales[id] = s
	return s, nil
}

// ListUnsettledCheckouts imita al repositorio: abiertos ya vencidos o con el pago
// en proceso, y cerrados por vencimiento o rechazo.
func (f *fakeSales) ListUnsettledCheckouts(_ context.Context, limit int) ([]string, error) {
	var refs []string
	for ref, c := range f.checkouts {
		openExpired := c.Status == sale.CheckoutOpen && (!c.ExpiresAt.After(time.Now()) || !c.PaymentPendingAt.IsZero())
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

func (f *fakeSales) MarkPickupPaid(_ context.Context, id int64) (sale.Sale, error) {
	s, ok := f.sales[id]
	if !ok {
		return sale.Sale{}, sale.ErrPaymentNotFound
	}
	if s.Status != sale.StatusPendingPickup {
		return sale.Sale{}, sale.ErrNotPendingPickup
	}
	s.Status = sale.StatusPaid
	f.sales[id] = s
	return s, nil
}

func (f *fakeSales) CancelPickup(_ context.Context, id int64) (sale.Sale, error) {
	s, ok := f.sales[id]
	if !ok {
		return sale.Sale{}, sale.ErrPaymentNotFound
	}
	if s.Status != sale.StatusPendingPickup {
		return sale.Sale{}, sale.ErrNotPendingPickup
	}
	s.Status = sale.StatusCancelled
	f.sales[id] = s
	f.restocked = append(f.restocked, id)
	return s, nil
}

func (f *fakeSales) ExpirePickups(_ context.Context, now time.Time) ([]sale.Sale, error) {
	var out []sale.Sale
	for id, s := range f.sales {
		if s.Status == sale.StatusPendingPickup && !s.PickupDeadline.IsZero() && !s.PickupDeadline.After(now) {
			s.Status = sale.StatusCancelled
			f.sales[id] = s
			f.restocked = append(f.restocked, id)
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeSales) ListByUser(context.Context, string, int, int) ([]sale.Sale, error) {
	return nil, errors.New("no implementado en el fake")
}

// Search guarda el último filtro recibido para que los tests verifiquen cómo
// lo normaliza el caso de uso.
func (f *fakeSales) Search(_ context.Context, filter sale.ListFilter) ([]sale.Sale, int, error) {
	f.lastFilter = filter
	return nil, 0, nil
}

func (f *fakeSales) CountByStage(_ context.Context, filter sale.ListFilter) (sale.StageCounts, error) {
	f.lastFilter = filter
	return sale.StageCounts{}, nil
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
