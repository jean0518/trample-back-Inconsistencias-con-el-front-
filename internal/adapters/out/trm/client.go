package trm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const trmURL = "https://www.datos.gov.co/resource/32sa-8pi3.json?$limit=1&$order=vigenciadesde+DESC"

type Client struct {
	http      *http.Client
	mu        sync.Mutex
	cached    float64
	cachedAt  time.Time
	cacheTTL  time.Duration
}

func NewClient() *Client {
	return &Client{
		http:     &http.Client{Timeout: 5 * time.Second},
		cacheTTL: time.Hour,
	}
}

func (c *Client) GetRate(ctx context.Context) (float64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cached > 0 && time.Since(c.cachedAt) < c.cacheTTL {
		return c.cached, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, trmURL, nil)
	if err != nil {
		return 0, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("trm: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	slog.Info("trm raw response", slog.String("body", string(body)))

	var rows []struct {
		Valor string `json:"valor"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return 0, fmt.Errorf("trm: decode: %w", err)
	}
	if len(rows) == 0 {
		return 0, fmt.Errorf("trm: respuesta vacía")
	}

	rate, err := strconv.ParseFloat(rows[0].Valor, 64)
	if err != nil {
		return 0, fmt.Errorf("trm: valor inválido %q: %w", rows[0].Valor, err)
	}

	c.cached = rate
	c.cachedAt = time.Now()
	return rate, nil
}
