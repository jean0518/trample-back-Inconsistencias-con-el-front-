package catalog

import (
	"context"
	"sync"
	"time"

	"trample-back/internal/ports/out"
)

const (
	// expansionNamesTTL es cada cuánto se vuelve a pedir la lista de
	// expansiones a Scrydex. Salen pocos sets al año, así que un día basta.
	expansionNamesTTL = 24 * time.Hour
	// expansionNamesRetry evita reintentar en cada evento si Scrydex falló.
	expansionNamesRetry = time.Hour
)

// expansionNames traduce el ID de una expansión de Scrydex (ej: "sv3pt5") a su
// nombre ("151") para los avisos del webhook, incluso si la expansión nunca se
// dio de alta en la base.
//
// La lista se pide a Scrydex solo cuando hace falta y se guarda en memoria por
// juego, así que cuesta como mucho unas pocas llamadas al día por juego.
type expansionNames struct {
	scrydex out.ScrydexClient

	mu     sync.Mutex
	byGame map[string]cachedExpansionNames
}

type cachedExpansionNames struct {
	names   map[string]string
	expires time.Time
}

func newExpansionNames(scrydex out.ScrydexClient) *expansionNames {
	return &expansionNames{scrydex: scrydex, byGame: make(map[string]cachedExpansionNames)}
}

// Name devuelve el nombre de la expansión, o "" si no se conoce. Un fallo de
// Scrydex no es un error para quien llama: el aviso sale con el ID.
func (e *expansionNames) Name(ctx context.Context, gameCode, expansionID string) string {
	e.mu.Lock()
	defer e.mu.Unlock()

	cached, ok := e.byGame[gameCode]
	if !ok || time.Now().After(cached.expires) {
		cached = e.load(ctx, gameCode)
		e.byGame[gameCode] = cached
	}
	return cached.names[expansionID]
}

func (e *expansionNames) load(ctx context.Context, gameCode string) cachedExpansionNames {
	expansions, err := e.scrydex.FetchExpansions(ctx, gameCode)
	if err != nil {
		return cachedExpansionNames{expires: time.Now().Add(expansionNamesRetry)}
	}
	names := make(map[string]string, len(expansions))
	for _, exp := range expansions {
		names[exp.ExternalID] = exp.Name
	}
	return cachedExpansionNames{names: names, expires: time.Now().Add(expansionNamesTTL)}
}
