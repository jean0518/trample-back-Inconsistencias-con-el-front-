package sale

import "time"

// Estados de un crédito en tienda (cuenta por cobrar).
const (
	CreditOpen      = "open"
	CreditPaid      = "paid"
	CreditCancelled = "cancelled"
)

// StoreCredit es una cuenta por cobrar: el cliente recibió el pedido y quedó
// debiendo el saldo. `PaidCOP` acumula los abonos (incluido el inicial) y
// `BalanceCOP` siempre equivale a TotalCOP - PaidCOP.
type StoreCredit struct {
	ID     int64
	SaleID int64
	UserID string
	// Datos de lectura: se resuelven al consultar, no se guardan aquí.
	CustomerName  string
	CustomerEmail string
	TotalCOP      int64
	InitialCOP    int64
	PaidCOP       int64
	BalanceCOP    int64
	// DueDate es el día límite de pago (fecha civil de Colombia).
	DueDate   time.Time
	Notes     string
	CreatedBy string
	// CreatedByName es el nombre del admin que autorizó el crédito.
	CreatedByName string
	Status        string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Payments      []CreditPayment
}

// Overdue indica si el crédito sigue abierto y ya pasó su fecha límite. Se
// comparan fechas civiles en hora de Colombia: `DueDate` viene de una columna
// DATE, sin hora, y no debe desplazarse por la zona horaria del servidor.
func (c StoreCredit) Overdue(now time.Time) bool {
	if c.Status != CreditOpen || c.BalanceCOP <= 0 {
		return false
	}
	dy, dm, dd := c.DueDate.Date()
	due := time.Date(dy, dm, dd, 0, 0, 0, 0, storeZone)
	n := now.In(storeZone)
	ny, nm, nd := n.Date()
	today := time.Date(ny, nm, nd, 0, 0, 0, 0, storeZone)
	return due.Before(today)
}

// CreditPayment es un abono registrado a una cuenta por cobrar.
type CreditPayment struct {
	ID        int64
	CreditID  int64
	AmountCOP int64
	Note      string
	CreatedBy string
	CreatedAt time.Time
}

// CreditFilter son los filtros del listado de créditos. Los campos vacíos no
// filtran.
type CreditFilter struct {
	Status  string
	Overdue bool
	Query   string
	Limit   int
	Offset  int
}

// NewCreditInput son los datos para convertir un pedido pendiente en crédito.
// TotalCOP no se acepta del cliente: se toma del pedido para que la deuda y el
// pedido no puedan contradecirse.
type NewCreditInput struct {
	SaleID     int64
	InitialCOP int64
	DueDate    time.Time
	Notes      string
	CreatedBy  string
}
