package sale

// OrderItemInput es una línea de carta en la edición de un pedido. Con ID = 0
// se agrega una línea nueva y ListingID es obligatorio. Con ID > 0 se edita la
// línea existente; si Quantity es 0 se elimina.
type OrderItemInput struct {
	ID        int64
	ListingID *int64
	Quantity  int
	PriceCOP  int64
	PriceUSD  float64
}

// OrderProductInput es una línea de producto sellado en la edición de un pedido.
type OrderProductInput struct {
	ID        int64
	ProductID *int64
	Quantity  int
	PriceCOP  int64
}

// UpdateOrderInput es la corrección manual de un pedido desde el panel. Los
// ítems enviados reemplazan por completo a los actuales: lo que no venga se
// elimina y el stock se ajusta. Status vacío conserva el estado actual.
type UpdateOrderInput struct {
	ID           int64
	Address      string
	City         string
	Phone        string
	Status       string
	Items        []OrderItemInput
	ProductItems []OrderProductInput
	UpdatedBy    string
}
