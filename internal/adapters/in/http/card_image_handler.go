package http

import (
	"errors"
	"net/http"
	"strconv"

	"trample-back/internal/domain/catalog"
	"trample-back/internal/ports/out"
)

// CardImageHandler sirve los frontales que el staff subió como archivo.
//
// Las imágenes de Scrydex viven en su CDN y el catálogo las pide por URL. Las
// del alta manual se guardan en esta base y se sirven desde aquí, con lo que
// /card-images/{id} es para el catálogo exactamente lo que es la URL de
// Scrydex: una dirección que devolver bytes de la imagen.
//
//	@Summary      Servir una imagen de carta subida
//	@Tags         catalog
//	@Produce      image/jpeg
//	@Success      200  {file}    binary
//	@Failure      404  {object}  object{error=string}
//	@Router       /card-images/{id} [get]
type CardImageHandler struct {
	images out.CardImageRepository
}

// NewCardImageHandler construye el handler.
func NewCardImageHandler(images out.CardImageRepository) *CardImageHandler {
	return &CardImageHandler{images: images}
}

// Get devuelve los bytes de la imagen.
//
// Es público a propósito: el catálogo es público y una imagen no debe dejar de
// cargar porque la carta esté pausada o sin stock. El id es un BIGSERIAL, así
// que adivinarlo no expone datos de otro tipo: o existe una imagen o da 404.
func (h *CardImageHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		Error(w, http.StatusBadRequest, "id de imagen inválido")
		return
	}

	contentType, content, err := h.images.Get(r.Context(), id)
	if errors.Is(err, catalog.ErrNotFound) {
		Error(w, http.StatusNotFound, "imagen no encontrada")
		return
	}
	if err != nil {
		Error(w, http.StatusInternalServerError, "error interno del servidor")
		return
	}

	// Cache-Control largo: el nombre del archivo incluye el id, así que una
	// imagen reemplazada llega con otro id y no hay riesgo de servir una versión
	// vieja desde el caché del navegador.
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}
