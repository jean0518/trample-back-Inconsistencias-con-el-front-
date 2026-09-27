package http

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"trample-back/internal/domain/catalog"
)

// Formato del alta manual. El mismo endpoint acepta las dos formas porque el
// formulario elige según si el staff pegó una URL o seleccionó un archivo:
//
//	application/json                     {"name": "...", "image_url": "..."}
//	multipart/form-data                  name=... & image_file=@frontal.jpg
//
// Aceptar ambas evita mantener dos endpoints ni un cliente de negociación de
// contenido, y deja el caso más simple (JSON) sin el sobrecoste de un
// multipart.
const (
	imageFieldName = "image_file"
	maxFormMemory  = 8 << 20 // 8 MB
)

// DecodeManualListing lee el alta manual del request admitiendo JSON o
// multipart/form-data, y devuelve la entrada ya normalizada, incluido el
// ImageUpload del archivo si lo hubo.
func DecodeManualListing(w http.ResponseWriter, r *http.Request) (manualForm, error) {
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		// Sin Content-Type legible se asume JSON, que es lo que manda un
		// cliente REST normal y lo que ya enviaba el panel antes de que existiera
		// la subida de archivos.
		return decodeManualJSON(r)
	}

	switch mediaType {
	case "multipart/form-data":
		return decodeManualMultipart(w, r, params["boundary"])
	case "application/json", "text/json", "":
		return decodeManualJSON(r)
	default:
		return manualForm{}, fmt.Errorf("content-type no soportado: usa application/json o multipart/form-data")
	}
}

// manualForm es el alta en la forma neutral que espera el caso de uso, ya
// con los textos recortados y los numéricos convertidos.
type manualForm struct {
	GameCode      string
	ExpansionID   int64
	ExpansionName string
	ExternalID    string
	Name          string
	Number        string
	Rarity        string
	VariantName   string
	Quantity      int
	PriceUSD      float64
	Language      string
	OwnerID       int64
	ImageURL      string
	ImageFile     catalog.ImageUpload
}

func decodeManualJSON(r *http.Request) (manualForm, error) {
	// pointer para distinguir "no vino" de "vino vacío": expansion_id ausente y
	// expansion_id: 0 significan cosas distintas al resolver la expansión.
	var body struct {
		GameCode      string  `json:"game_code"`
		ExpansionID   *int64  `json:"expansion_id"`
		ExpansionName string  `json:"expansion_name"`
		ExternalID    string  `json:"external_id"`
		Name          string  `json:"name"`
		Number        string  `json:"number"`
		Rarity        string  `json:"rarity"`
		VariantName   string  `json:"variant_name"`
		Quantity      int     `json:"quantity"`
		PriceUSD      float64 `json:"price_usd"`
		Language      string  `json:"language"`
		OwnerID       *int64  `json:"owner_id"`
		ImageURL      string  `json:"image_url"`
	}
	if err := Decode(r, &body); err != nil {
		return manualForm{}, fmt.Errorf("el cuerpo de la petición no es JSON válido: %w", err)
	}

	form := manualForm{
		GameCode:      strings.TrimSpace(body.GameCode),
		ExpansionName: strings.TrimSpace(body.ExpansionName),
		ExternalID:    strings.TrimSpace(body.ExternalID),
		Name:          strings.TrimSpace(body.Name),
		Number:        strings.TrimSpace(body.Number),
		Rarity:        strings.TrimSpace(body.Rarity),
		VariantName:   catalog.NormalizeVariantName(body.VariantName),
		Quantity:      body.Quantity,
		PriceUSD:      body.PriceUSD,
		Language:      strings.TrimSpace(body.Language),
		ImageURL:      strings.TrimSpace(body.ImageURL),
	}
	if body.ExpansionID != nil {
		form.ExpansionID = *body.ExpansionID
	}
	if body.OwnerID != nil {
		form.OwnerID = *body.OwnerID
	}
	return form, nil
}

func decodeManualMultipart(w http.ResponseWriter, r *http.Request, boundary string) (manualForm, error) {
	if boundary == "" {
		return manualForm{}, errors.New("falta el boundary del multipart")
	}

	// MaxBytesReader envuelve la petición para que un archivo enorme se corte
	// mientras se lee, en vez de acumularse entero en memoria o en el temporal
	// de disco antes de que el handler lo note.
	r.Body = http.MaxBytesReader(w, r.Body, maxFormMemory)
	if err := r.ParseMultipartForm(maxFormMemory); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return manualForm{}, fmt.Errorf("el archivo supera el máximo de %d MB", maxErr.Limit>>20)
		}
		return manualForm{}, fmt.Errorf("no se pudo leer el formulario: %w", err)
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	form := manualForm{
		GameCode:      strings.TrimSpace(r.FormValue("game_code")),
		ExpansionName: strings.TrimSpace(r.FormValue("expansion_name")),
		ExternalID:    strings.TrimSpace(r.FormValue("external_id")),
		Name:          strings.TrimSpace(r.FormValue("name")),
		Number:        strings.TrimSpace(r.FormValue("number")),
		Rarity:        strings.TrimSpace(r.FormValue("rarity")),
		VariantName:   catalog.NormalizeVariantName(r.FormValue("variant_name")),
		Language:      strings.TrimSpace(r.FormValue("language")),
		ImageURL:      strings.TrimSpace(r.FormValue("image_url")),
	}
	if raw := strings.TrimSpace(r.FormValue("quantity")); raw != "" {
		qty, err := strconv.Atoi(raw)
		if err != nil {
			return manualForm{}, errors.New("quantity no es un número válido")
		}
		form.Quantity = qty
	}
	if raw := strings.TrimSpace(r.FormValue("price_usd")); raw != "" {
		price, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return manualForm{}, errors.New("price_usd no es un número válido")
		}
		form.PriceUSD = price
	}
	if raw := strings.TrimSpace(r.FormValue("owner_id")); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return manualForm{}, errors.New("owner_id no es un número válido")
		}
		form.OwnerID = id
	}
	if raw := strings.TrimSpace(r.FormValue("expansion_id")); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return manualForm{}, fmt.Errorf("expansion_id no es un número válido")
		}
		form.ExpansionID = id
	}

	// Que no haya archivo es válido: el frontal es opcional y puede venir solo
	// la URL.
	file, header, err := r.FormFile(imageFieldName)
	if errors.Is(err, http.ErrMissingFile) {
		return form, nil
	}
	if err != nil {
		return manualForm{}, fmt.Errorf("no se pudo leer el archivo de imagen: %w", err)
	}
	defer file.Close()

	if header.Size > catalog.MaxImageBytes {
		return manualForm{}, fmt.Errorf("la imagen supera el máximo de %d MB", catalog.MaxImageBytes>>20)
	}

	// Se lee un byte por encima del tope del dominio para poder distinguir
	// "cabía justo" de "se pasó"; el dominio vuelve a validar tamaño y tipo
	// sobre los bytes reales, y ni el header ni el Content-Type que declara el
	// cliente son fuente de confianza.
	content, err := io.ReadAll(io.LimitReader(file, catalog.MaxImageBytes+1))
	if err != nil {
		return manualForm{}, fmt.Errorf("no se pudo leer el archivo de imagen: %w", err)
	}

	upload, err := catalog.NewImageUpload(content)
	if err != nil {
		return manualForm{}, err
	}
	form.ImageFile = upload
	return form, nil
}
