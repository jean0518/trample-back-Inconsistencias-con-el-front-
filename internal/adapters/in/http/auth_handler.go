package http

import (
	"encoding/json"
	"errors"
	"net/http"
	appAuth "trample-back/internal/application/auth"
	"trample-back/internal/domain/auth"
)

type AuthHandler struct {
	register *appAuth.RegisterUseCase
	login    *appAuth.LoginUseCase
}

type authUserResponse struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
	Role      string `json:"role"`
}

func newAuthUserResponse(u auth.User) authUserResponse {
	return authUserResponse{
		ID:        u.ID,
		FirstName: u.FirstName,
		LastName:  u.LastName,
		Email:     u.Email,
		Role:      u.Role,
	}
}

func NewAuthHandler(register *appAuth.RegisterUseCase, login *appAuth.LoginUseCase) *AuthHandler {
	return &AuthHandler{register: register, login: login}
}

// Register crea una nueva cuenta de usuario.
//
//	@Summary      Registrar usuario
//	@Tags         auth
//	@Accept       json
//	@Produce      json
//
// @Param        body  body      object{first_name=string,last_name=string,email=string,password=string}  true  "Datos del nuevo usuario"
// @Success      201   {object}  object{token=string,user=object{id=integer,first_name=string,last_name=string,email=string,role=string}}
// @Failure      409   {object}  object{error=string}  "Email ya registrado"
// @Failure      422   {object}  object{error=string}  "Datos de validación inválidos"
// @Router       /auth/register [post]
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var body struct {
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
		Email     string `json:"email"`
		Password  string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		JSON(w, http.StatusBadRequest, map[string]string{"error": "body JSON inválido"})
		return
	}

	user, err := h.register.Execute(r.Context(), appAuth.RegisterInput{
		FirstName: body.FirstName,
		LastName:  body.LastName,
		Email:     body.Email,
		Password:  body.Password,
	})
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrEmailTaken):
			JSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		case errors.Is(err, appAuth.ErrPasswordTooShort),
			errors.Is(err, appAuth.ErrInvalidEmail),
			errors.Is(err, appAuth.ErrInvalidName):
			JSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		default:
			JSON(w, http.StatusInternalServerError, map[string]string{"error": "error interno del servidor"})
		}
		return
	}

	token, err := h.login.IssueToken(r.Context(), user)
	if err != nil {
		JSON(w, http.StatusInternalServerError, map[string]string{"error": "no se pudo emitir el token"})
		return
	}

	JSON(w, http.StatusCreated, map[string]any{
		"token": token,
		"user":  newAuthUserResponse(user),
	})
}

// Login autentica al usuario y devuelve un JWT.
//
//	@Summary      Iniciar sesión
//	@Tags         auth
//	@Accept       json
//	@Produce      json
//	@Param        body  body      object{email=string,password=string}  true  "Credenciales"
//
// @Success      200   {object}  object{token=string,user=object{id=integer,first_name=string,last_name=string,email=string,role=string}}
// @Failure      401   {object}  object{error=string}  "Credenciales inválidas"
// @Router       /auth/login [post]
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		JSON(w, http.StatusBadRequest, map[string]string{"error": "body JSON inválido"})
		return
	}

	result, err := h.login.Execute(r.Context(), appAuth.LoginInput{
		Email:    body.Email,
		Password: body.Password,
	})
	if err != nil {
		JSON(w, http.StatusUnauthorized, map[string]string{"error": "credenciales inválidas"})
		return
	}

	JSON(w, http.StatusOK, map[string]any{
		"token": result.Token,
		"user":  newAuthUserResponse(result.User),
	})
}
