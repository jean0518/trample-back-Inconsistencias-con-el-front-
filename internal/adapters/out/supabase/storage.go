package supabase

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"trample-back/internal/domain/catalog"
	"trample-back/internal/ports/out"
)

// Storage sube los frontales al bucket de Supabase Storage.
//
// Habla con la API REST de Storage con net/http en vez de meter el SDK
// oficial: el proyecto no tiene ninguna dependencia de Supabase y la operación
// es un POST, así que el SDK solo añadiría peso al binario.
type Storage struct {
	baseURL string
	bucket  string
	key     string
	client  *http.Client
}

// NewStorage devuelve nil si falta configuración, para que el repositorio de
// imágenes pueda seguir escribiendo en la base en lugar de romper el arranque.
func NewStorage(baseURL, serviceKey, bucket string) out.ImageStorage {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	bucket = strings.TrimSpace(bucket)
	if baseURL == "" || serviceKey == "" || bucket == "" {
		return nil
	}
	return &Storage{
		baseURL: baseURL,
		bucket:  bucket,
		key:     serviceKey,
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

// extensionByContentType mapea el tipo real detectado sobre los bytes a la
// extensión del archivo. El Content-Type viaja aparte en la petición, así que
// un desajuste entre extensión y tipo no rompería la carga, pero sí el nombre
// que ve un humano en el panel de Supabase.
func extensionByContentType(contentType string) string {
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "image/jpeg", "image/jpg":
		return "jpg"
	case "image/png":
		return "png"
	case "image/webp":
		return "webp"
	case "image/gif":
		return "gif"
	default:
		return "bin"
	}
}

// objectPath arma la clave del objeto. El sufijo aleatorio no es decorativo:
// la URL de la imagen entra en la respuesta del catálogo y el navegador la
// cachea con agresividad, así que con una clave estable por carta una
// re-subida seguiría mostrando el frontal viejo.
func objectPath(cardID int64, extension string) string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// Si falla la entropía se usa el tiempo: es peor que el nombre tenga
		// menos aleatoriedad, pero no tanto como perder la subida entera.
		return fmt.Sprintf("cards/%d/%d.%s", cardID, time.Now().UnixMilli(), extension)
	}
	return fmt.Sprintf("cards/%d/%d-%s.%s", cardID, time.Now().UnixMilli(), hex.EncodeToString(buf[:]), extension)
}

// escapeKey escapa cada segmento de la clave por separado. La clave lleva "/"
// y sin esto el servidor la interpretaría como una sola parte mal formada.
func escapeKey(key string) string {
	parts := strings.Split(strings.TrimLeft(key, "/"), "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

// PublicURL devuelve la dirección pública de una clave dentro del bucket.
func (s *Storage) PublicURL(objectKey string) string {
	return fmt.Sprintf("%s/storage/v1/object/public/%s/%s", s.baseURL, s.bucket, escapeKey(objectKey))
}

// Put sube el frontal y devuelve la URL pública.
func (s *Storage) Put(ctx context.Context, cardID int64, upload catalog.ImageUpload) (string, error) {
	if len(upload.Content) == 0 {
		return "", fmt.Errorf("la imagen está vacía")
	}

	objectKey := objectPath(cardID, extensionByContentType(upload.ContentType))
	endpoint := fmt.Sprintf("%s/storage/v1/object/%s/%s",
		s.baseURL, url.PathEscape(s.bucket), escapeKey(objectKey))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(upload.Content))
	if err != nil {
		return "", fmt.Errorf("no se pudo preparar la subida de la imagen: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.key)
	req.Header.Set("apikey", s.key)
	req.Header.Set("Content-Type", upload.ContentType)
	// x-upsert deja que la clave se sobrescriba en vez de fallar si por
	// algún motivo ya existe.
	req.Header.Set("x-upsert", "true")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("no se pudo subir la imagen: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return "", fmt.Errorf("Supabase rechazó la imagen (HTTP %d): %s",
			resp.StatusCode, strings.TrimSpace(string(body)))
	}
	_, _ = io.Copy(io.Discard, resp.Body)

	return s.PublicURL(objectKey), nil
}
