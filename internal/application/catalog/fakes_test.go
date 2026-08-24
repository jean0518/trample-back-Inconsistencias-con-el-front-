package catalog

import (
	"context"

	"trample-back/internal/domain/catalog"
	"trample-back/internal/ports/out"
)

// Fixtures compartidos por los tests de los casos de uso de catálogo.

type fakeCardRepo struct {
	out.CardRepository
	synced map[string]bool
}

func newFakeCardRepo() *fakeCardRepo {
	return &fakeCardRepo{synced: make(map[string]bool)}
}

func (f *fakeCardRepo) SyncCard(_ context.Context, gameCode string, card catalog.Card) error {
	f.synced[gameCode+":"+card.ExternalID] = true
	return nil
}

func cardWithID(externalID string) catalog.Card {
	return catalog.Card{ExternalID: externalID, Name: externalID}
}
