package catalog

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"trample-back/internal/domain/catalog"
	"trample-back/internal/ports/out"
)

// PriceStaleAfter es cuánto puede pasar sin que una carta consulte su precio
// en Scrydex antes de considerarse desactualizada.
const PriceStaleAfter = 7 * 24 * time.Hour

// refreshCard vuelve a consultar una carta en Scrydex (con TRM aplicado) y
// persiste el resultado, incluyendo el precio actualizado.
func refreshCard(ctx context.Context, search *SearchScrydex, repo out.CardRepository, gameCode, externalID string) (*catalog.Card, error) {
	card, err := search.FetchOne(ctx, gameCode, externalID, nil)
	if err != nil {
		return nil, fmt.Errorf("re-fetch de scrydex: %w", err)
	}
	if err := repo.SyncCard(ctx, gameCode, *card); err != nil {
		return nil, fmt.Errorf("actualizar carta: %w", err)
	}
	return card, nil
}

// PriceRefresher mantiene los precios de Scrydex al día: refresca cartas
// desactualizadas tanto de forma perezosa (disparada al listar el catálogo)
// como en lotes (job periódico de respaldo para cartas que nadie consulta).
type PriceRefresher struct {
	search *SearchScrydex
	repo   out.CardRepository
	log    *slog.Logger

	mu       sync.Mutex
	inFlight map[int64]bool
	lastLazy time.Time
}

func NewPriceRefresher(search *SearchScrydex, repo out.CardRepository, log *slog.Logger) *PriceRefresher {
	if log == nil {
		log = slog.Default()
	}
	return &PriceRefresher{search: search, repo: repo, log: log, inFlight: make(map[int64]bool)}
}

// TriggerLazy dispara en segundo plano (sin bloquear al llamador) el
// refresco de un pequeño lote de las cartas más desactualizadas. Pensado
// para llamarse cada vez que se consulta el catálogo público, con un
// cooldown para no saturar Scrydex si hay muchos requests seguidos.
func (r *PriceRefresher) TriggerLazy(ctx context.Context) {
	const (
		cooldown  = 1 * time.Minute
		lazyBatch = 3
	)

	r.mu.Lock()
	if time.Since(r.lastLazy) < cooldown {
		r.mu.Unlock()
		return
	}
	r.lastLazy = time.Now()
	r.mu.Unlock()

	go func() {
		bgCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if _, err := r.RefreshStaleBatch(bgCtx, PriceStaleAfter, lazyBatch); err != nil {
			r.log.Warn("refresco perezoso de precios falló", slog.Any("error", err))
		}
	}()
}

// RefreshStaleBatch busca hasta `limit` cartas con precio desactualizado
// (más viejas primero) y las vuelve a consultar en Scrydex. Cartas que ya
// tienen un refresco en curso se saltan.
func (r *PriceRefresher) RefreshStaleBatch(ctx context.Context, olderThan time.Duration, limit int) (refreshed int, err error) {
	stale, err := r.repo.ListStaleCards(ctx, olderThan, limit)
	if err != nil {
		return 0, fmt.Errorf("buscar cartas desactualizadas: %w", err)
	}

	for _, ref := range stale {
		if !r.claim(ref.ID) {
			continue
		}
		_, refErr := refreshCard(ctx, r.search, r.repo, ref.GameCode, ref.ExternalID)
		r.release(ref.ID)
		if refErr != nil {
			r.log.Warn("no se pudo refrescar precio",
				slog.Int64("card_id", ref.ID),
				slog.String("game_code", ref.GameCode),
				slog.Any("error", refErr))
			continue
		}
		refreshed++
	}
	return refreshed, nil
}

func (r *PriceRefresher) claim(cardID int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.inFlight[cardID] {
		return false
	}
	r.inFlight[cardID] = true
	return true
}

func (r *PriceRefresher) release(cardID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.inFlight, cardID)
}
