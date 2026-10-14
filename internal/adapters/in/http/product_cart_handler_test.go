package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	appProduct "trample-back/internal/application/product"
	"trample-back/internal/domain/product"
)

type stubReservations struct {
	res product.Reservation
	err error
}

func (s stubReservations) Reserve(context.Context, product.ReserveInput, int) (product.Reservation, error) {
	return s.res, s.err
}
func (s stubReservations) ListActiveByUser(context.Context, string) ([]product.Reservation, error) {
	return nil, s.err
}
func (s stubReservations) ListActiveByUserAndIDs(context.Context, string, []int64) ([]product.Reservation, error) {
	return nil, s.err
}
func (s stubReservations) ExtendExpiry(context.Context, string, []int64, int) error { return s.err }
func (s stubReservations) Remove(context.Context, string, int64) error              { return s.err }
func (s stubReservations) ReleaseExpired(context.Context) (int64, error)            { return 0, nil }

func cartRequest(method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), authCtxKey, AuthUser{ID: "u1"}))
	return req
}

func TestProductCartAddStatusCodes(t *testing.T) {
	ok := product.Reservation{ID: 7, ProductID: 3, Title: "Caja", Quantity: 2, PriceCOP: 1000, ExpiresAt: time.Unix(0, 0)}
	cases := []struct {
		name string
		repo stubReservations
		body string
		want int
	}{
		{"ok", stubReservations{res: ok}, `{"product_id":3,"quantity":2}`, http.StatusCreated},
		{"agotado", stubReservations{err: product.ErrInsufficientStock}, `{"product_id":3,"quantity":1}`, http.StatusConflict},
		{"no existe u oculto", stubReservations{err: product.ErrNotFound}, `{"product_id":3,"quantity":1}`, http.StatusNotFound},
		{"sin cantidad", stubReservations{}, `{"product_id":3}`, http.StatusBadRequest},
		{"json inválido", stubReservations{}, `nope`, http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := NewProductCartHandler(appProduct.NewCartUseCase(c.repo))
			rec := httptest.NewRecorder()
			h.Add(rec, cartRequest(http.MethodPost, "/cart/products", c.body))
			if rec.Code != c.want {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, c.want, rec.Body)
			}
		})
	}
}

func TestProductCartAddResponseShapeAndAuth(t *testing.T) {
	h := NewProductCartHandler(appProduct.NewCartUseCase(stubReservations{
		res: product.Reservation{ID: 7, ProductID: 3, Title: "Caja", Quantity: 2, PriceCOP: 1500, ExpiresAt: time.Unix(0, 0)},
	}))

	rec := httptest.NewRecorder()
	h.Add(rec, cartRequest(http.MethodPost, "/cart/products", `{"product_id":3,"quantity":2}`))
	var got productCartItemResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.LineTotalCOP != 3000 || got.ProductID != 3 || got.ExpiresAt != "1970-01-01T00:00:00Z" {
		t.Errorf("respuesta: %+v", got)
	}

	// sin sesión
	rec = httptest.NewRecorder()
	h.Add(rec, httptest.NewRequest(http.MethodPost, "/cart/products", strings.NewReader(`{"product_id":3,"quantity":1}`)))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("sin sesión debía ser 401, fue %d", rec.Code)
	}
}

func TestProductResponseHidesStockFromPublic(t *testing.T) {
	p := product.Product{ID: 1, Title: "Caja", PriceCOP: 1000, Stock: 9, Reserved: 4, Status: product.StatusActive}

	pub, _ := json.Marshal(toProductResponse(p, false))
	var m map[string]any
	_ = json.Unmarshal(pub, &m)
	if _, ok := m["stock"]; ok {
		t.Error("el público no debe ver stock")
	}
	if _, ok := m["reserved"]; ok {
		t.Error("el público no debe ver reserved")
	}
	if m["available"] != float64(5) || m["sold_out"] != false {
		t.Errorf("available/sold_out: %v", m)
	}

	adm, _ := json.Marshal(toProductResponse(p, true))
	_ = json.Unmarshal(adm, &m)
	if m["stock"] != float64(9) || m["reserved"] != float64(4) {
		t.Errorf("el admin debe ver stock y reserved: %v", m)
	}
}
