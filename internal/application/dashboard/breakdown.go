package dashboard

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"time"

	"trample-back/internal/domain/dashboard"
	"trample-back/internal/domain/sale"
)

// ErrInvalidRange se devuelve con fechas mal escritas, invertidas o con un rango
// más largo que dashboard.MaxBreakdownDays.
var ErrInvalidRange = errors.New("rango de fechas inválido: usa YYYY-MM-DD, 'to' no anterior a 'from' y como máximo 2 años")

// colombia es la zona de todas las fechas y buckets del desglose.
var colombia = time.FixedZone("COT", -5*60*60)

// monthlyAfterDays es el largo a partir del cual la serie se agrupa por mes.
const monthlyAfterDays = 45

// defaultBreakdownDays es el rango cuando no se envían fechas.
const defaultBreakdownDays = 30

// breakdownReader es lo que el desglose necesita del repositorio.
type breakdownReader interface {
	// BreakdownSales devuelve las ventas creadas en [from, to), con las líneas
	// de las pagadas.
	BreakdownSales(ctx context.Context, from, to time.Time) ([]dashboard.BreakdownSale, error)
}

// BreakdownUseCase arma el desglose de ventas del panel "Ventas" en el
// servidor, con las mismas reglas que antes aplicaba el navegador sobre las
// últimas 500 ventas.
type BreakdownUseCase struct {
	repo breakdownReader
	now  func() time.Time
}

func NewBreakdownUseCase(repo breakdownReader) *BreakdownUseCase {
	return &BreakdownUseCase{repo: repo, now: time.Now}
}

// Execute calcula el desglose de [from, to] (YYYY-MM-DD, ambos días incluidos,
// hora de Colombia). Sin fechas, cubre los últimos 30 días.
func (uc *BreakdownUseCase) Execute(ctx context.Context, from, to string, top int) (dashboard.Breakdown, error) {
	start, end, err := uc.resolveRange(from, to)
	if err != nil {
		return dashboard.Breakdown{}, err
	}
	top = clamp(top, dashboard.DefaultBreakdownTop, 1, dashboard.MaxBreakdownTop)

	current, err := uc.repo.BreakdownSales(ctx, start, end)
	if err != nil {
		return dashboard.Breakdown{}, err
	}
	// El período anterior tiene el mismo largo y termina donde empieza este.
	length := end.Sub(start)
	previous, err := uc.repo.BreakdownSales(ctx, start.Add(-length), start)
	if err != nil {
		return dashboard.Breakdown{}, err
	}
	return buildBreakdown(current, previous, start, end, top), nil
}

// resolveRange convierte las fechas del filtro en [start, end) en hora de
// Colombia y valida el largo.
func (uc *BreakdownUseCase) resolveRange(from, to string) (time.Time, time.Time, error) {
	if from == "" && to == "" {
		today := uc.now().In(colombia)
		end := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, colombia).AddDate(0, 0, 1)
		return end.AddDate(0, 0, -defaultBreakdownDays), end, nil
	}
	if from == "" || to == "" {
		return time.Time{}, time.Time{}, ErrInvalidRange
	}
	start, end, err := sale.ParseDateRange(from, to)
	if err != nil {
		return time.Time{}, time.Time{}, ErrInvalidRange
	}
	if days(start, end) > dashboard.MaxBreakdownDays {
		return time.Time{}, time.Time{}, ErrInvalidRange
	}
	return start.In(colombia), end.In(colombia), nil
}

// days cuenta los días de [start, end).
func days(start, end time.Time) int {
	return int(end.Sub(start).Hours()/24 + 0.5)
}

// buildBreakdown aplica las reglas del panel:
//   - ingresos, unidades, top y desgloses: solo ventas pagadas;
//   - el ingreso total incluye el envío; cartas y sellado no;
//   - cards.orders / sealed.orders: pedidos con al menos una línea de la categoría;
//   - top por unidades y luego por ingreso; un producto borrado se agrupa por título;
//   - fechas y buckets en hora de Colombia.
func buildBreakdown(current, previous []dashboard.BreakdownSale, start, end time.Time, top int) dashboard.Breakdown {
	b := dashboard.Breakdown{
		From:        start.Format("2006-01-02"),
		To:          end.AddDate(0, 0, -1).Format("2006-01-02"),
		Granularity: "day",
	}
	if days(start, end) > monthlyAfterDays {
		b.Granularity = "month"
	}

	series, bucketOf := newSeries(start, end, b.Granularity)
	cards := map[string]*dashboard.TopCard{}
	sealed := map[string]*dashboard.TopSealed{}
	byPayment := map[string]*dashboard.KeyOrders{}
	byFulfillment := map[string]*dashboard.KeyOrders{}
	byLanguage := map[string]*dashboard.KeyUnits{}

	for _, s := range current {
		switch s.Status {
		case sale.StatusPendingPickup:
			b.Pending.Orders++
			b.Pending.TotalCOP += s.TotalCOP
			continue
		case sale.StatusCancelled:
			b.Cancelled.Orders++
			b.Cancelled.TotalCOP += s.TotalCOP
			continue
		case sale.StatusPaid:
		default:
			continue
		}

		created := s.CreatedAt.In(colombia)
		b.Totals.Orders++
		b.Totals.RevenueCOP += s.TotalCOP
		b.Totals.ShippingCOP += s.ShippingCOP
		b.Heatmap[(int(created.Weekday())+6)%7][created.Hour()]++

		bucket := series[bucketOf(created)]
		if bucket != nil {
			bucket.Orders++
		}
		addKeyOrders(byPayment, s.PaymentMethod, s.TotalCOP)
		addKeyOrders(byFulfillment, s.Fulfillment, s.TotalCOP)

		var hasCard, hasSealed bool
		for _, l := range s.Lines {
			revenue := l.PriceCOP * int64(l.Quantity)
			b.Totals.Units += l.Quantity
			if l.Sealed {
				hasSealed = true
				b.Sealed.Units += l.Quantity
				b.Sealed.RevenueCOP += revenue
				if bucket != nil {
					bucket.SealedCOP += revenue
				}
				key := "t:" + l.Name
				if l.ProductID != nil {
					key = "p:" + strconv.FormatInt(*l.ProductID, 10)
				}
				t := sealed[key]
				if t == nil {
					t = &dashboard.TopSealed{ProductID: l.ProductID, Title: l.Name}
					sealed[key] = t
				}
				t.Units += l.Quantity
				t.RevenueCOP += revenue
				continue
			}
			hasCard = true
			b.Cards.Units += l.Quantity
			b.Cards.RevenueCOP += revenue
			if bucket != nil {
				bucket.CardsCOP += revenue
			}
			key := strconv.FormatInt(l.CardID, 10) + "|" + l.Language
			t := cards[key]
			if t == nil {
				t = &dashboard.TopCard{CardID: l.CardID, Name: l.Name, Language: l.Language}
				cards[key] = t
			}
			t.Units += l.Quantity
			t.RevenueCOP += revenue
			lang := byLanguage[l.Language]
			if lang == nil {
				lang = &dashboard.KeyUnits{Key: l.Language}
				byLanguage[l.Language] = lang
			}
			lang.Units += l.Quantity
			lang.RevenueCOP += revenue
		}
		if hasCard {
			b.Cards.Orders++
		}
		if hasSealed {
			b.Sealed.Orders++
		}
	}
	if b.Totals.Orders > 0 {
		b.Totals.AvgTicketCOP = b.Totals.RevenueCOP / int64(b.Totals.Orders)
	}

	for _, s := range previous {
		if s.Status != sale.StatusPaid {
			continue
		}
		b.Previous.Orders++
		b.Previous.RevenueCOP += s.TotalCOP
		for _, l := range s.Lines {
			b.Previous.Units += l.Quantity
		}
	}

	b.Series = make([]dashboard.SeriesBucket, 0, len(series))
	for _, key := range sortedKeys(series) {
		b.Series = append(b.Series, *series[key])
	}
	b.TopCards = topCards(cards, top)
	b.TopSealed = topSealed(sealed, top)
	b.ByPayment = sortKeyOrders(byPayment)
	b.ByFulfillment = sortKeyOrders(byFulfillment)
	b.ByLanguage = sortKeyUnits(byLanguage)
	return b
}

// newSeries crea un bucket vacío por día o por mes del rango y devuelve cómo
// encontrar el bucket de una fecha. Las claves son la fecha de inicio
// (YYYY-MM-DD), que ordenan bien como texto.
func newSeries(start, end time.Time, granularity string) (map[string]*dashboard.SeriesBucket, func(time.Time) string) {
	series := map[string]*dashboard.SeriesBucket{}
	if granularity == "month" {
		key := func(t time.Time) string { return t.Format("2006-01") + "-01" }
		for m := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, colombia); m.Before(end); m = m.AddDate(0, 1, 0) {
			series[key(m)] = &dashboard.SeriesBucket{Start: key(m)}
		}
		return series, key
	}
	key := func(t time.Time) string { return t.Format("2006-01-02") }
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		series[key(d)] = &dashboard.SeriesBucket{Start: key(d)}
	}
	return series, key
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func addKeyOrders(m map[string]*dashboard.KeyOrders, key string, revenue int64) {
	k := m[key]
	if k == nil {
		k = &dashboard.KeyOrders{Key: key}
		m[key] = k
	}
	k.Orders++
	k.RevenueCOP += revenue
}

func topCards(m map[string]*dashboard.TopCard, top int) []dashboard.TopCard {
	out := make([]dashboard.TopCard, 0, len(m))
	for _, t := range m {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Units != out[j].Units {
			return out[i].Units > out[j].Units
		}
		if out[i].RevenueCOP != out[j].RevenueCOP {
			return out[i].RevenueCOP > out[j].RevenueCOP
		}
		return out[i].Name < out[j].Name
	})
	return out[:min(top, len(out))]
}

func topSealed(m map[string]*dashboard.TopSealed, top int) []dashboard.TopSealed {
	out := make([]dashboard.TopSealed, 0, len(m))
	for _, t := range m {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Units != out[j].Units {
			return out[i].Units > out[j].Units
		}
		if out[i].RevenueCOP != out[j].RevenueCOP {
			return out[i].RevenueCOP > out[j].RevenueCOP
		}
		return out[i].Title < out[j].Title
	})
	return out[:min(top, len(out))]
}

// sortKeyOrders ordena por ingreso y luego por clave, para que la respuesta sea
// estable.
func sortKeyOrders(m map[string]*dashboard.KeyOrders) []dashboard.KeyOrders {
	out := make([]dashboard.KeyOrders, 0, len(m))
	for _, k := range m {
		out = append(out, *k)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].RevenueCOP != out[j].RevenueCOP {
			return out[i].RevenueCOP > out[j].RevenueCOP
		}
		return out[i].Key < out[j].Key
	})
	return out
}

func sortKeyUnits(m map[string]*dashboard.KeyUnits) []dashboard.KeyUnits {
	out := make([]dashboard.KeyUnits, 0, len(m))
	for _, k := range m {
		out = append(out, *k)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Units != out[j].Units {
			return out[i].Units > out[j].Units
		}
		return out[i].Key < out[j].Key
	})
	return out
}
