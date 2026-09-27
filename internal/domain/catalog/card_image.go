package catalog

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// ErrNotFound indica que la imagen solicitada no existe o ya no está
// asociada a ninguna carta. El handler lo traduce a un 404.
var ErrNotFound = errors.New("imagen no encontrada")

// Límites de la imagen que el staff sube al dar de alta una carta a mano.
const (
	// MaxImageBytes es el techo del archivo. Un frontal de carta ronda los
	// 200 KB, así que 5 MB deja margen para fotos de celular sin abrir la
	// puerta a archivos que solo engordan la base.
	MaxImageBytes = 5 << 20 // 5 MB
	// sniffBufferSize es el tamaño del recorte que se entrega a
	// http.DetectContentType, que solo inspecciona los primeros 512 bytes.
	sniffBufferSize = 512
)

// allowedImageTypes son los formatos que acepta el alta manual, con la
// extensión con la que se sirven. El tipo se deduce de los bytes, nunca del
// que declara el navegador.
var allowedImageTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

// ImageUpload es el frontal de una carta subido como archivo, ya validado. Se
// guarda en la base y se sirve desde /card-images/{id}.
//
// La alternativa es ImageSource.URL, para cuando el staff ya tiene la imagen
// hospeada en otro lado; las dos formas alimentan el mismo campo del catálogo.
type ImageUpload struct {
	Content     []byte
	ContentType string
}

// NewImageUpload valida un archivo subido y devuelve el ImageUpload listo para
// persistir, o un error legible para el frontend.
//
// El tipo se deduce de los bytes con http.DetectContentType en vez de usar el
// header Content-Type de la petición: un cliente puede mandar un binario
// declarándolo como image/png y acabaría guardando en la base algo que luego
// se sirve desde el catálogo.
func NewImageUpload(content []byte) (ImageUpload, error) {
	if len(content) == 0 {
		return ImageUpload{}, fmt.Errorf("el archivo de imagen está vacío")
	}
	if len(content) > MaxImageBytes {
		return ImageUpload{}, fmt.Errorf("la imagen supera el máximo de %d MB", MaxImageBytes>>20)
	}

	head := content
	if len(head) > sniffBufferSize {
		head = head[:sniffBufferSize]
	}
	contentType := http.DetectContentType(head)
	if _, ok := allowedImageTypes[contentType]; !ok {
		return ImageUpload{}, fmt.Errorf("formato no soportado: %s (usa JPG, PNG, WEBP o GIF)", friendlyImageType(contentType))
	}

	return ImageUpload{Content: content, ContentType: contentType}, nil
}

// Extension es la extensión con la que se sirve el archivo, deducida del
// contenido real y no del nombre que le puso el staff.
func (u ImageUpload) Extension() string {
	return allowedImageTypes[u.ContentType]
}

// SizeBytes es el tamaño que se persiste en la base.
func (u ImageUpload) SizeBytes() int {
	return len(u.Content)
}

// IsEmpty indica si el alta viene sin archivo.
func (u ImageUpload) IsEmpty() bool {
	return len(u.Content) == 0
}

// ImageSource es lo que el formulario de alta manual envía para el frontal: una
// URL externa, un archivo subido, o nada.
type ImageSource struct {
	URL    string
	Upload ImageUpload
}

// ValidateImageSource decide cuál de las dos formas usar y rechaza el estado
// ambiguo en que vienen las dos, que el frontend podría mandar por error al
// cambiar de pestaña en el formulario.
func ValidateImageSource(rawURL string, upload ImageUpload) (ImageSource, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL != "" && !upload.IsEmpty() {
		return ImageSource{}, fmt.Errorf("indica una URL de imagen o un archivo, no ambos")
	}
	return ImageSource{URL: rawURL, Upload: upload}, nil
}

// HasImage indica si el alta trae frontal de alguna de las dos formas.
func (s ImageSource) HasImage() bool {
	return s.URL != "" || !s.Upload.IsEmpty()
}

// friendlyImageType traduce el tipo que devolvió el sniffer a un nombre que el
// staff entienda, en vez de un "text/plain" que no explica nada.
func friendlyImageType(contentType string) string {
	// http.DetectContentType puede devolver parámetros (por ejemplo
	// "text/plain; charset=utf-8"), y compararlos contra "text/plain" a secas
	// nunca casaría. Se quitan antes de traducir.
	if base, _, found := strings.Cut(contentType, ";"); found {
		contentType = strings.TrimSpace(base)
	}
	if strings.HasPrefix(contentType, "image/") {
		return strings.TrimPrefix(contentType, "image/")
	}
	switch contentType {
	case "text/plain":
		return "archivo de texto"
	case "application/pdf":
		return "PDF"
	case "application/zip", "application/x-zip-compressed":
		return "ZIP"
	case "application/octet-stream":
		return "binario"
	}
	return contentType
}
