package catalog

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"trample-back/internal/domain/catalog"
	"trample-back/internal/domain/listing"
	"trample-back/internal/ports/out"
)

// gameCodeFromEvent convierte el prefijo del evento de Scrydex al código de
// juego interno. Ej: "magicthegathering.expansions.prices.raw_updated" → "mtg"
func gameCodeFromEvent(eventName string) string {
	prefix := strings.SplitN(eventName, ".", 2)[0]
	switch prefix {
	case "magicthegathering":
		return "mtg"
	default:
		return prefix // "pokemon", "riftbound"
	}
}

// WebhookEvent representa el payload que envía Scrydex al disparar un evento.
type WebhookEvent struct {
	ID   string           `json:"id"`
	Name string           `json:"name"`
	Data WebhookEventData `json:"data"`
}

type WebhookEventData struct {
	ExpansionIDs []string `json:"expansion_ids"`
}

// ProcessWebhookUseCase recibe eventos de precio de Scrydex y actualiza el
// precio de las cartas que tienen listing en el inventario.
//
// Las expansiones sin nada listado se saltan sin consultar Scrydex, y de las
// que sí tienen, solo se refrescan las cartas listadas.
//
// No inserta cartas. Los eventos de precio traen la expansión completa, así que
// antes de esto se sincronizaban las ~100 cartas de cada set informado y la tabla
// `cards`crecía con el catálogo completo de Scrydex sin que nadie hubiera dado de
// alta un listing. Ahora una carta que no está en la base se ignora: el alta
// sigue siendo responsabilidad del flujo de listings.
type ProcessWebhookUseCase struct {
	search *SearchScrydex
	repo   out.CardPriceRefresher
	// notifier es opcional: sin él los cambios solo quedan en los logs.
	notifier out.PriceUpdateNotifier
	log      *slog.Logger
}

func NewProcessWebhookUseCase(search *SearchScrydex, repo out.CardPriceRefresher, notifier out.PriceUpdateNotifier, log *slog.Logger) *ProcessWebhookUseCase {
	if log == nil {
		log = slog.Default()
	}
	return &ProcessWebhookUseCase{search: search, repo: repo, notifier: notifier, log: log}
}

func (uc *ProcessWebhookUseCase) Execute(ctx context.Context, event WebhookEvent) error {
	gameCode := gameCodeFromEvent(event.Name)
	if gameCode == "" {
		return fmt.Errorf("evento desconocido: %s", event.Name)
	}

	uc.log.Info("webhook scrydex recibido",
		slog.String("event_id", event.ID),
		slog.String("event_name", event.Name),
		slog.Int("expansions", len(event.Data.ExpansionIDs)),
	)

	var firstErr error
	refreshed, skipped, skippedExpansions := 0, 0, 0
	var changes []catalog.CardPriceChange
	for _, expansionID := range event.Data.ExpansionIDs {
		// Primero se mira qué hay listado de la expansión: Scrydex avisa por set
		// completo y la mayoría de sets no tienen nada en el inventario.
		listed, err := uc.repo.ListedExternalIDs(ctx, gameCode, expansionID)
		if err != nil {
			uc.log.Warn("webhook: no se pudo consultar el inventario de la expansión",
				slog.String("expansion_id", expansionID),
				slog.Any("error", err),
			)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if len(listed) == 0 {
			skippedExpansions++
			uc.log.Info("webhook: expansión sin listings activos, se salta",
				slog.String("event_id", event.ID),
				slog.String("game_code", gameCode),
				slog.String("expansion_id", expansionID),
			)
			continue
		}

		cards, err := uc.search.FetchExpansionCards(ctx, gameCode, expansionID)
		if err != nil {
			uc.log.Warn("webhook: no se pudo obtener expansión",
				slog.String("expansion_id", expansionID),
				slog.Any("error", err),
			)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}

		uc.log.Info("webhook: expansión con listings, revisando precios",
			slog.String("event_id", event.ID),
			slog.String("game_code", gameCode),
			slog.String("expansion_id", expansionID),
			slog.Int("cartas_en_scrydex", len(cards)),
			slog.Int("cartas_listadas", len(listed)),
		)

		for _, card := range cards {
			if !listed[card.ExternalID] {
				skipped++
				continue
			}
			updates, err := uc.repo.RefreshCardPrices(ctx, gameCode, card)
			if err != nil {
				uc.log.Warn("webhook: no se pudo actualizar el precio",
					slog.String("external_id", card.ExternalID),
					slog.Any("error", err),
				)
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			if len(updates) == 0 {
				skipped++
				continue
			}
			refreshed++
			for _, u := range updates {
				uc.logPriceUpdate(event.ID, gameCode, expansionID, card, u)
				change := catalog.CardPriceChange{
					ExpansionID: expansionID,
					ExternalID:  card.ExternalID,
					CardName:    card.Name,
					Update:      u,
				}
				if change.Changed() {
					changes = append(changes, change)
				}
			}
		}
	}

	uc.log.Info("webhook: precios actualizados",
		slog.String("event_name", event.Name),
		slog.Int("cartas_actualizadas", refreshed),
		slog.Int("cartas_ignoradas_sin_listing", skipped),
		slog.Int("expansiones_sin_listing", skippedExpansions),
		slog.Int("variantes_con_precio_distinto", len(changes)),
	)

	// Solo se avisa si algún precio cambió de verdad: Scrydex manda el set
	// completo y la mayoría de eventos no mueven nada del inventario.
	if uc.notifier != nil && len(changes) > 0 {
		if err := uc.notifier.NotifyPriceUpdates(ctx, gameCode, changes); err != nil {
			uc.log.Warn("webhook: no se pudo enviar el aviso de precios",
				slog.String("event_id", event.ID),
				slog.Any("error", err),
			)
		}
	}

	return firstErr
}

// logPriceUpdate deja una línea por variante actualizada con el precio anterior
// y el nuevo, para poder rastrear en los logs qué cambió en cada evento.
func (uc *ProcessWebhookUseCase) logPriceUpdate(eventID, gameCode, expansionID string, card catalog.Card, u catalog.PriceUpdate) {
	attrs := []any{
		slog.String("event_id", eventID),
		slog.String("game_code", gameCode),
		slog.String("expansion_id", expansionID),
		slog.String("external_id", card.ExternalID),
		slog.String("carta", card.Name),
		slog.String("variante", u.VariantName),
		slog.Float64("precio_nuevo_usd", u.NewUSD),
		slog.Int64("precio_nuevo_cop", u.NewCOP),
		slog.Float64("precio_espanol_usd", listing.SpanishPriceUSD(u.NewUSD)),
		slog.Int64("listings_actualizados", u.ListingsRepriced),
	}
	if u.HadPrice {
		attrs = append(attrs, slog.Float64("precio_anterior_usd", u.OldUSD))
	} else {
		attrs = append(attrs, slog.String("precio_anterior_usd", "sin precio"))
	}
	uc.log.Info("webhook: precio actualizado", attrs...)
}
