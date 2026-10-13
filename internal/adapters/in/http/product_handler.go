package http

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	appProduct "trample-back/internal/application/product"
	"trample-back/internal/domain/catalog"
	"trample-back/internal/domain/product"
	"trample-back/internal/ports/out"
)

// ProductHandler expone los productos propios de la tienda: lectura pública y
// administración exclusiva del rol admin.
type ProductHandler struct {
	manage *appProduct.ManageUseCase
	query  *appProduct.QueryUseCase
	images out.ProductRepository
}

// NewProductHandler construye el handler. images solo se usa para servir los
// bytes de las fotos guardadas en la base.
func NewProductHandler(manage *appProduct.ManageUseCase, query *appProduct.QueryUseCase, images out.ProductRepository) *ProductHandler {
	return &ProductHandler{manage: manage, query: query, images: images}
}

type productImageResponse struct {
	ID       int64  `json:"id"`
	Position int    `json:"position"`
	URL      string `json:"url"`
}

type productResponse struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	PriceCOP    int64  `json:"price_cop"`
	// Stock y Reserved son solo para el panel admin: el público ve available y
	// sold_out, no cuántas unidades hay ni cuántas tienen otros en su carrito.
	Stock     *int                   `json:"stock,omitempty"`
	Reserved  *int                   `json:"reserved,omitempty"`
	Available int                    `json:"available"`
	SoldOut   bool                   `json:"sold_out"`
	Status    string                 `json:"status"`
	Images    []productImageResponse `json:"images"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
}

func toProductResponse(p product.Product, admin bool) productResponse {
	imgs := make([]productImageResponse, 0, len(p.Images))
	for _, i := range p.Images {
		imgs = append(imgs, productImageResponse{ID: i.ID, Position: i.Position, URL: i.URL})
	}
	resp := productResponse{
		ID: p.ID, Title: p.Title, Description: p.Description, PriceCOP: p.PriceCOP,
		Available: p.Available(), SoldOut: p.SoldOut(),
		Status: p.Status, Images: imgs, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
	if admin {
		resp.Stock, resp.Reserved = &p.Stock, &p.Reserved
	}
	return resp
}

type productListResponse struct {
	Items  []productResponse `json:"items"`
	Total  int               `json:"total"`
	Limit  int               `json:"limit"`
	Offset int               `json:"offset"`
}

// writeProductError traduce los errores del dominio. Los de validación ya son
// texto pensado para el admin y se muestran tal cual; lo demás es un 500 que
// no filtra detalles.
func writeProductError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, product.ErrNotFound), errors.Is(err, product.ErrImageNotFound):
		Error(w, http.StatusNotFound, err.Error())
	case errors.Is(err, product.ErrHasSales):
		Error(w, http.StatusConflict, err.Error())
	case errors.Is(err, product.ErrTooManyImages):
		Error(w, http.StatusUnprocessableEntity, err.Error())
	case isProductValidation(err):
		Error(w, http.StatusBadRequest, err.Error())
	default:
		internalError(w, err)
	}
}

// isProductValidation reconoce los errores de entrada. El dominio los devuelve
// como errores simples (sin tipo propio), así que se distinguen de los internos
// porque ningún error de base de datos contiene estos prefijos.
func isProductValidation(err error) bool {
	msg := err.Error()
	for _, prefix := range []string{
		"el título", "la descripción", "el precio", "el stock", "estado inválido",
		"la URL de la imagen", "la imagen", "el archivo", "formato no soportado",
		"no se pudo identificar",
	} {
		if strings.HasPrefix(msg, prefix) {
			return true
		}
	}
	return false
}

func parseProductID(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id, err == nil && id > 0
}

func listFilterFromQuery(r *http.Request) product.ListFilter {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	return product.ListFilter{Query: q.Get("q"), Limit: limit, Offset: offset}
}

func (h *ProductHandler) list(w http.ResponseWriter, r *http.Request, admin bool) {
	f := listFilterFromQuery(r)
	f.IncludeHidden = admin
	items, total, err := h.query.List(r.Context(), f, admin)
	if err != nil {
		internalError(w, err)
		return
	}
	f = f.Normalize()
	resp := productListResponse{Items: make([]productResponse, 0, len(items)), Total: total, Limit: f.Limit, Offset: f.Offset}
	for _, p := range items {
		resp.Items = append(resp.Items, toProductResponse(p, admin))
	}
	JSON(w, http.StatusOK, resp)
}

// List devuelve los productos publicados de la tienda.
//
//	@Summary  Listar productos publicados
//	@Tags     products
//	@Router   /products [get]
func (h *ProductHandler) List(w http.ResponseWriter, r *http.Request) { h.list(w, r, false) }

// ListAdmin devuelve todos los productos, incluidos los ocultos.
//
//	@Summary  Listar todos los productos (admin)
//	@Tags     products
//	@Router   /admin/products [get]
func (h *ProductHandler) ListAdmin(w http.ResponseWriter, r *http.Request) { h.list(w, r, true) }

func (h *ProductHandler) get(w http.ResponseWriter, r *http.Request, admin bool) {
	id, ok := parseProductID(r, "id")
	if !ok {
		Error(w, http.StatusBadRequest, "id de producto inválido")
		return
	}
	p, err := h.query.Get(r.Context(), id, admin)
	if err != nil {
		writeProductError(w, err)
		return
	}
	JSON(w, http.StatusOK, toProductResponse(p, admin))
}

// Get devuelve un producto publicado.
//
//	@Router /products/{id} [get]
func (h *ProductHandler) Get(w http.ResponseWriter, r *http.Request) { h.get(w, r, false) }

// GetAdmin devuelve un producto aunque esté oculto.
//
//	@Router /admin/products/{id} [get]
func (h *ProductHandler) GetAdmin(w http.ResponseWriter, r *http.Request) { h.get(w, r, true) }

type productBody struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	PriceCOP    int64    `json:"price_cop"`
	Stock       int      `json:"stock"`
	Status      string   `json:"status"`
	ImageURLs   []string `json:"image_urls"`
}

// Create da de alta un producto.
//
// Acepta JSON (fotos por URL en image_urls) o multipart/form-data (archivos en
// el campo repetido "images" y/o URLs en "image_urls"). Si el producto se
// guarda pero una foto falla responde 422 con el producto ya creado, porque
// reenviar el formulario lo duplicaría.
//
//	@Router /admin/products [post]
func (h *ProductHandler) Create(w http.ResponseWriter, r *http.Request) {
	body, uploads, err := decodeProductForm(w, r)
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	user, ok := AuthFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "no autenticado")
		return
	}

	p, err := h.manage.Create(r.Context(), user.ID, appProduct.CreateInput{
		Title: body.Title, Description: body.Description, PriceCOP: body.PriceCOP,
		Stock: body.Stock, Status: body.Status, ImageURLs: body.ImageURLs, Uploads: uploads,
	})
	if errors.Is(err, product.ErrImageAttachment) {
		JSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error":   err.Error(),
			"product": toProductResponse(p, true),
		})
		return
	}
	if err != nil {
		writeProductError(w, err)
		return
	}
	JSON(w, http.StatusCreated, toProductResponse(p, true))
}

// Update reemplaza título, descripción, precio y stock.
//
//	@Router /admin/products/{id} [put]
func (h *ProductHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := parseProductID(r, "id")
	if !ok {
		Error(w, http.StatusBadRequest, "id de producto inválido")
		return
	}
	var body productBody
	if err := Decode(r, &body); err != nil {
		Error(w, http.StatusBadRequest, "el cuerpo de la petición no es JSON válido")
		return
	}
	p, err := h.manage.Update(r.Context(), id, product.Input{
		Title: body.Title, Description: body.Description, PriceCOP: body.PriceCOP, Stock: body.Stock,
	})
	if err != nil {
		writeProductError(w, err)
		return
	}
	JSON(w, http.StatusOK, toProductResponse(p, true))
}

// SetStatus oculta o publica el producto.
//
//	@Router /admin/products/{id}/status [patch]
func (h *ProductHandler) SetStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := parseProductID(r, "id")
	if !ok {
		Error(w, http.StatusBadRequest, "id de producto inválido")
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := Decode(r, &body); err != nil {
		Error(w, http.StatusBadRequest, "el cuerpo de la petición no es JSON válido")
		return
	}
	p, err := h.manage.SetStatus(r.Context(), id, body.Status)
	if err != nil {
		writeProductError(w, err)
		return
	}
	JSON(w, http.StatusOK, toProductResponse(p, true))
}

// Delete borra un producto sin ventas.
//
//	@Router /admin/products/{id} [delete]
func (h *ProductHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseProductID(r, "id")
	if !ok {
		Error(w, http.StatusBadRequest, "id de producto inválido")
		return
	}
	if err := h.manage.Delete(r.Context(), id); err != nil {
		writeProductError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// AddImage añade una foto: archivo en el campo "image" (multipart) o
// {"url": "..."} en JSON.
//
//	@Router /admin/products/{id}/images [post]
func (h *ProductHandler) AddImage(w http.ResponseWriter, r *http.Request) {
	id, ok := parseProductID(r, "id")
	if !ok {
		Error(w, http.StatusBadRequest, "id de producto inválido")
		return
	}

	var (
		img product.Image
		err error
	)
	if isMultipart(r) {
		var uploads []catalog.ImageUpload
		_, uploads, err = decodeProductForm(w, r)
		if err == nil && len(uploads) != 1 {
			err = errors.New("envía exactamente un archivo en el campo images")
		}
		if err != nil {
			Error(w, http.StatusBadRequest, err.Error())
			return
		}
		img, err = h.manage.AddImageUpload(r.Context(), id, uploads[0])
	} else {
		var body struct {
			URL string `json:"url"`
		}
		if derr := Decode(r, &body); derr != nil {
			Error(w, http.StatusBadRequest, "el cuerpo de la petición no es JSON válido")
			return
		}
		img, err = h.manage.AddImageURL(r.Context(), id, body.URL)
	}
	if err != nil {
		writeProductError(w, err)
		return
	}
	JSON(w, http.StatusCreated, productImageResponse{ID: img.ID, Position: img.Position, URL: img.URL})
}

// RemoveImage quita una foto del producto.
//
//	@Router /admin/products/{id}/images/{imageId} [delete]
func (h *ProductHandler) RemoveImage(w http.ResponseWriter, r *http.Request) {
	id, ok := parseProductID(r, "id")
	imageID, ok2 := parseProductID(r, "imageId")
	if !ok || !ok2 {
		Error(w, http.StatusBadRequest, "id inválido")
		return
	}
	if err := h.manage.RemoveImage(r.Context(), id, imageID); err != nil {
		writeProductError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ServeImage devuelve los bytes de una foto guardada en la base. Es público:
// el catálogo lo es, y la foto no debe dejar de cargar porque el producto esté
// agotado.
//
//	@Router /product-images/{id} [get]
func (h *ProductHandler) ServeImage(w http.ResponseWriter, r *http.Request) {
	id, ok := parseProductID(r, "id")
	if !ok {
		Error(w, http.StatusBadRequest, "id de imagen inválido")
		return
	}
	contentType, content, err := h.images.GetImage(r.Context(), id)
	if errors.Is(err, product.ErrImageNotFound) {
		Error(w, http.StatusNotFound, "imagen no encontrada")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	// Cache largo: el id cambia con cada foto nueva, no hay versión vieja que servir.
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func isMultipart(r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && mediaType == "multipart/form-data"
}

const (
	productImagesField = "images"
	// productFormMemory cubre las 5 fotos de MaxImageBytes más los campos de texto.
	productFormMemory = product.MaxImages*catalog.MaxImageBytes + (1 << 20)
)

// decodeProductForm lee el alta en JSON o multipart. Los archivos se validan
// por sus bytes (catalog.NewImageUpload), no por el Content-Type que declara el
// cliente.
func decodeProductForm(w http.ResponseWriter, r *http.Request) (productBody, []catalog.ImageUpload, error) {
	if !isMultipart(r) {
		var body productBody
		if err := Decode(r, &body); err != nil {
			return body, nil, errors.New("el cuerpo de la petición no es JSON válido")
		}
		return body, nil, nil
	}

	r.Body = http.MaxBytesReader(w, r.Body, productFormMemory)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return productBody{}, nil, fmt.Errorf("el formulario supera el máximo permitido")
		}
		return productBody{}, nil, fmt.Errorf("no se pudo leer el formulario: %w", err)
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	body := productBody{
		Title:       strings.TrimSpace(r.FormValue("title")),
		Description: strings.TrimSpace(r.FormValue("description")),
		Status:      strings.TrimSpace(r.FormValue("status")),
	}
	if raw := strings.TrimSpace(r.FormValue("price_cop")); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return body, nil, errors.New("price_cop no es un número válido")
		}
		body.PriceCOP = n
	}
	if raw := strings.TrimSpace(r.FormValue("stock")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return body, nil, errors.New("stock no es un número válido")
		}
		body.Stock = n
	}
	for _, u := range r.MultipartForm.Value["image_urls"] {
		if u = strings.TrimSpace(u); u != "" {
			body.ImageURLs = append(body.ImageURLs, u)
		}
	}

	files := r.MultipartForm.File[productImagesField]
	if len(files) > product.MaxImages {
		return body, nil, product.ErrTooManyImages
	}
	uploads := make([]catalog.ImageUpload, 0, len(files))
	for _, fh := range files {
		if fh.Size > catalog.MaxImageBytes {
			return body, nil, fmt.Errorf("la imagen %q supera el máximo de %d MB", fh.Filename, catalog.MaxImageBytes>>20)
		}
		f, err := fh.Open()
		if err != nil {
			return body, nil, fmt.Errorf("no se pudo leer el archivo de imagen: %w", err)
		}
		// Un byte por encima del tope para distinguir "cabía justo" de "se pasó".
		content, err := io.ReadAll(io.LimitReader(f, catalog.MaxImageBytes+1))
		f.Close()
		if err != nil {
			return body, nil, fmt.Errorf("no se pudo leer el archivo de imagen: %w", err)
		}
		up, err := catalog.NewImageUpload(content)
		if err != nil {
			return body, nil, err
		}
		uploads = append(uploads, up)
	}
	return body, uploads, nil
}
