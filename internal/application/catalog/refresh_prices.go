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
const PriceStaleAfter = 30 * 24 * time.Hour

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

// expansionKey identifica una expansión de forma única para agrupar.
type expansionKey struct {
	gameCode            string
	expansionExternalID string
}

// RefreshStaleBatch busca hasta `limit` cartas con precio desactualizado y
// las refresca agrupando por expansión: 1 request a Scrydex por expansión en
// lugar de 1 por carta. Esto reduce drásticamente las llamadas a la API.
func (r *PriceRefresher) RefreshStaleBatch(ctx context.Context, olderThan time.Duration, limit int) (refreshed int, err error) {
	stale, err := r.repo.ListStaleCards(ctx, olderThan, limit)
	if err != nil {
		return 0, fmt.Errorf("buscar cartas desactualizadas: %w", err)
	}
	if len(stale) == 0 {
		return 0, nil
	}

	// Agrupa las cartas stale por (gameCode, expansionExternalID).
	groups := make(map[expansionKey][]out.StaleCardRef)
	for _, ref := range stale {
		k := expansionKey{ref.GameCode, ref.ExpansionExternalID}
		groups[k] = append(groups[k], ref)
	}

	for key, refs := range groups {
		// 1 request a Scrydex por expansión completa.
		expCards, fetchErr := r.search.FetchExpansionCards(ctx, key.gameCode, key.expansionExternalID)
		if fetchErr != nil {
			r.log.Warn("no se pudo obtener expansión de Scrydex",
				slog.String("game_code", key.gameCode),
				slog.String("expansion", key.expansionExternalID),
				slog.Any("error", fetchErr))
			continue
		}

		// Índice externalID → carta para búsqueda O(1).
		byExternalID := make(map[string]catalog.Card, len(expCards))
		for _, c := range expCards {
			byExternalID[c.ExternalID] = c
		}

		for _, ref := range refs {
			card, ok := byExternalID[ref.ExternalID]
			if !ok {
				r.log.Warn("carta no encontrada en respuesta de expansión",
					slog.String("external_id", ref.ExternalID))
				continue
			}
			if !r.claim(ref.ID) {
				continue
			}
			syncErr := r.repo.SyncCard(ctx, ref.GameCode, card)
			r.release(ref.ID)
			if syncErr != nil {
				r.log.Warn("no se pudo sincronizar carta",
					slog.Int64("card_id", ref.ID),
					slog.Any("error", syncErr))
				continue
			}
			refreshed++
		}
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
