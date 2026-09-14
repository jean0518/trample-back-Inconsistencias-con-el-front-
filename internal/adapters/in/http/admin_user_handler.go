package http

import (
	"errors"
	"net/http"
	"strconv"

	appAdmin "trample-back/internal/application/admin"
	"trample-back/internal/domain/auth"
)

type AdminUserHandler struct {
	create     *appAdmin.CreateAdminUseCase
	list       *appAdmin.ListAdminsUseCase
	updateRole *appAdmin.UpdateRoleUseCase
	delete     *appAdmin.DeleteAdminUseCase
}

func NewAdminUserHandler(
	create *appAdmin.CreateAdminUseCase,
	list *appAdmin.ListAdminsUseCase,
	updateRole *appAdmin.UpdateRoleUseCase,
	delete *appAdmin.DeleteAdminUseCase,
) *AdminUserHandler {
	return &AdminUserHandler{create: create, list: list, updateRole: updateRole, delete: delete}
}

type adminUserResponse struct {
	ID          int64    `json:"id"`
	FirstName   string   `json:"first_name"`
	LastName    string   `json:"last_name"`
	Email       string   `json:"email"`
	Role        string   `json:"role"`
	Permissions []string `json:"permissions"`
}

func newAdminUserResponse(u auth.User) adminUserResponse {
	perms := u.Permissions
	if perms == nil {
		perms = []string{}
	}
	return adminUserResponse{
		ID:          u.ID,
		FirstName:   u.FirstName,
		LastName:    u.LastName,
		Email:       u.Email,
		Role:        u.Role,
		Permissions: perms,
	}
}

func (h *AdminUserHandler) List(w http.ResponseWriter, r *http.Request) {
	users, err := h.list.Execute(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, "error interno del servidor")
		return
	}
	resp := make([]adminUserResponse, 0, len(users))
	for _, u := range users {
		resp = append(resp, newAdminUserResponse(u))
	}
	JSON(w, http.StatusOK, resp)
}

func (h *AdminUserHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
		Email     string `json:"email"`
		Password  string `json:"password"`
		Role      string `json:"role"`
	}
	if err := Decode(r, &body); err != nil {
		Error(w, http.StatusBadRequest, "body JSON inválido")
		return
	}

	user, err := h.create.Execute(r.Context(), appAdmin.CreateAdminInput{
		FirstName: body.FirstName,
		LastName:  body.LastName,
		Email:     body.Email,
		Password:  body.Password,
		Role:      body.Role,
	})
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrEmailTaken):
			Error(w, http.StatusConflict, err.Error())
		case errors.Is(err, appAdmin.ErrInvalidName),
			errors.Is(err, appAdmin.ErrPasswordTooShort),
			errors.Is(err, appAdmin.ErrInvalidRole):
			Error(w, http.StatusUnprocessableEntity, err.Error())
		default:
			Error(w, http.StatusInternalServerError, "error interno del servidor")
		}
		return
	}
	JSON(w, http.StatusCreated, newAdminUserResponse(user))
}

func (h *AdminUserHandler) UpdateRole(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		Error(w, http.StatusBadRequest, "id inválido")
		return
	}

	var body struct {
		Role string `json:"role"`
	}
	if err := Decode(r, &body); err != nil {
		Error(w, http.StatusBadRequest, "body JSON inválido")
		return
	}

	if err := h.updateRole.Execute(r.Context(), appAdmin.UpdateRoleInput{
		UserID: id,
		Role:   body.Role,
	}); err != nil {
		switch {
		case errors.Is(err, auth.ErrUserNotFound):
			Error(w, http.StatusNotFound, "usuario no encontrado")
		case errors.Is(err, appAdmin.ErrInvalidRole):
			Error(w, http.StatusUnprocessableEntity, err.Error())
		default:
			Error(w, http.StatusInternalServerError, "error interno del servidor")
		}
		return
	}
	JSON(w, http.StatusOK, map[string]string{"updated": strconv.FormatInt(id, 10)})
}

func (h *AdminUserHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		Error(w, http.StatusBadRequest, "id inválido")
		return
	}

	if err := h.delete.Execute(r.Context(), id); err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			Error(w, http.StatusNotFound, "usuario no encontrado")
			return
		}
		Error(w, http.StatusInternalServerError, "error interno del servidor")
		return
	}
	JSON(w, http.StatusOK, map[string]string{"deleted": strconv.FormatInt(id, 10)})
}
