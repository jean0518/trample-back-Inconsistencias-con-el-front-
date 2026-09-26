package catalog

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

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

// ProcessWebhookUseCase recibe eventos de precio de Scrydex y sincroniza
// todas las cartas de las expansiones afectadas en la base de datos.
type ProcessWebhookUseCase struct {
	search *SearchScrydex
	repo   out.CardRepository
	log    *slog.Logger
}

func NewProcessWebhookUseCase(search *SearchScrydex, repo out.CardRepository, log *slog.Logger) *ProcessWebhookUseCase {
	if log == nil {
		log = slog.Default()
	}
	return &ProcessWebhookUseCase{search: search, repo: repo, log: log}
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
	for _, expansionID := range event.Data.ExpansionIDs {
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

		for _, card := range cards {
			if err := uc.repo.SyncCard(ctx, gameCode, card); err != nil {
				uc.log.Warn("webhook: no se pudo sincronizar carta",
					slog.String("external_id", card.ExternalID),
					slog.Any("error", err),
				)
				if firstErr == nil {
					firstErr = err
				}
			}
		}

		uc.log.Info("webhook: expansión sincronizada",
			slog.String("expansion_id", expansionID),
			slog.Int("cards", len(cards)),
		)
	}

	return firstErr
}
