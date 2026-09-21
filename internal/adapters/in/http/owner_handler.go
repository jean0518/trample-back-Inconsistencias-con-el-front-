package http

import (
	"errors"
	"net/http"
	"strconv"

	appOwner "trample-back/internal/application/owner"
	"trample-back/internal/domain/owner"
)

type OwnerHandler struct {
	create *appOwner.CreateOwnerUseCase
	update *appOwner.UpdateOwnerUseCase
	list   *appOwner.ListOwnersUseCase
	delete *appOwner.DeleteOwnerUseCase
}

func NewOwnerHandler(
	create *appOwner.CreateOwnerUseCase,
	update *appOwner.UpdateOwnerUseCase,
	list *appOwner.ListOwnersUseCase,
	delete *appOwner.DeleteOwnerUseCase,
) *OwnerHandler {
	return &OwnerHandler{create: create, update: update, list: list, delete: delete}
}

type ownerResponse struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Phone     string `json:"phone"`
	Email     string `json:"email"`
	IsDefault bool   `json:"is_default"`
}

type createOwnerRequest struct {
	Name      string `json:"name"`
	Phone     string `json:"phone"`
	Email     string `json:"email"`
	IsDefault *bool  `json:"is_default,omitempty"`
}

type updateOwnerRequest struct {
	Name  string `json:"name"`
	Phone string `json:"phone"`
	Email string `json:"email"`
}

func newOwnerResponse(o owner.Owner) ownerResponse {
	return ownerResponse{ID: o.ID, Name: o.Name, Phone: o.Phone, Email: o.Email, IsDefault: o.IsDefault}
}

// List devuelve todos los propietarios.
//
//	@Summary      Listar propietarios
//	@Tags         admin
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200   {array}   ownerResponse
//	@Failure      401   {object}  object{error=string}
//	@Failure      500   {object}  object{error=string}
//	@Router       /admin/owners [get]
func (h *OwnerHandler) List(w http.ResponseWriter, r *http.Request) {
	owners, err := h.list.Execute(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, "error interno del servidor")
		return
	}
	resp := make([]ownerResponse, 0, len(owners))
	for _, o := range owners {
		resp = append(resp, newOwnerResponse(o))
	}
	JSON(w, http.StatusOK, resp)
}

// Create agrega un propietario nuevo.
//
//	@Summary      Crear propietario
//	@Tags         admin
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      createOwnerRequest  true  "Datos del propietario"
//	@Success      201   {object}  ownerResponse
//	@Failure      400   {object}  object{error=string}
//	@Failure      401   {object}  object{error=string}
//	@Failure      500   {object}  object{error=string}
//	@Router       /admin/owners [post]
func (h *OwnerHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body createOwnerRequest
	if err := Decode(r, &body); err != nil {
		Error(w, http.StatusBadRequest, "body JSON inválido")
		return
	}
	if body.Name == "" {
		Error(w, http.StatusBadRequest, "name es requerido")
		return
	}

	isDefault := false
	if body.IsDefault != nil {
		isDefault = *body.IsDefault
	}

	o, err := h.create.Execute(r.Context(), owner.CreateInput{Name: body.Name, Phone: body.Phone, Email: body.Email, IsDefault: isDefault})
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	JSON(w, http.StatusCreated, newOwnerResponse(o))
}

// Update actualiza la información de un propietario (incluido el predeterminado).
//
//	@Summary      Actualizar propietario
//	@Tags         admin
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id    path  int                true  "ID del propietario"
//	@Param        body  body  updateOwnerRequest true  "Datos a actualizar"
//	@Success      200   {object}  ownerResponse
//	@Failure      400   {object}  object{error=string}
//	@Failure      401   {object}  object{error=string}
//	@Failure      404   {object}  object{error=string}
//	@Router       /admin/owners/{id} [patch]
func (h *OwnerHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		Error(w, http.StatusBadRequest, "id inválido")
		return
	}

	var body updateOwnerRequest
	if err := Decode(r, &body); err != nil {
		Error(w, http.StatusBadRequest, "body JSON inválido")
		return
	}

	o, err := h.update.Execute(r.Context(), id, owner.UpdateInput{Name: body.Name, Phone: body.Phone, Email: body.Email})
	if err != nil {
		if errors.Is(err, owner.ErrNotFound) {
			Error(w, http.StatusNotFound, err.Error())
			return
		}
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	JSON(w, http.StatusOK, newOwnerResponse(o))
}

// Delete elimina un propietario (no el predeterminado).
//
//	@Summary      Eliminar propietario
//	@Tags         admin
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id   path  int  true  "ID del propietario"
//	@Success      200  {object}  object{deleted=string}
//	@Failure      400  {object}  object{error=string}
//	@Failure      401  {object}  object{error=string}
//	@Failure      404  {object}  object{error=string}
//	@Router       /admin/owners/{id} [delete]
func (h *OwnerHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		Error(w, http.StatusBadRequest, "id inválido")
		return
	}
	if err := h.delete.Execute(r.Context(), id); err != nil {
		if errors.Is(err, owner.ErrNotFound) {
			Error(w, http.StatusNotFound, err.Error())
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"deleted": strconv.FormatInt(id, 10)})
}
