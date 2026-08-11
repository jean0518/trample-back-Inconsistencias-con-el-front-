package http

import (
	"encoding/json"
	"errors"
	"net/http"
	appAuth "trample-back/internal/application/auth"
)

type AuthHandler struct {
	register *appAuth.RegisterUseCase
	login    *appAuth.LoginUseCase
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
//	@Param        body  body      object{first_name=string,last_name=string,email=string,password=string}  true  "Datos del nuevo usuario"
//	@Success      201   {object}  object{id=integer,first_name=string,last_name=string,email=string}
//	@Failure      409   {object}  object{error=string}  "Email ya registrado"
//	@Failure      422   {object}  object{error=string}  "Contraseña muy corta"
//	@Router       /auth/register [post]
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var body struct {
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
		Email     string `json:"email"`
		Password  string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		JSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	user, err := h.register.Execute(r.Context(), appAuth.RegisterInput{
		FirstName: body.FirstName,
		LastName:  body.LastName,
		Email:     body.Email,
		Password:  body.Password,
	})
	if err != nil {
		if errors.Is(err, appAuth.ErrPasswordTooShort) {
			JSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		JSON(w, http.StatusConflict, map[string]string{"error": "email already registered"})
		return
	}

	JSON(w, http.StatusCreated, map[string]any{
		"id":         user.ID,
		"first_name": user.FirstName,
		"last_name":  user.LastName,
		"email":      user.Email,
	})
}

// Login autentica al usuario y devuelve un JWT.
//
//	@Summary      Iniciar sesión
//	@Tags         auth
//	@Accept       json
//	@Produce      json
//	@Param        body  body      object{email=string,password=string}  true  "Credenciales"
//	@Success      200   {object}  object{token=string}
//	@Failure      401   {object}  object{error=string}  "Credenciales inválidas"
//	@Router       /auth/login [post]
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		JSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	token, err := h.login.Execute(r.Context(), appAuth.LoginInput{
		Email:    body.Email,
		Password: body.Password,
	})
	if err != nil {
		JSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}

	JSON(w, http.StatusOK, map[string]string{"token": token})
}
