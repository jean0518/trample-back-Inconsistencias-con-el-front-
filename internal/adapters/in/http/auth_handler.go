package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	appAuth "trample-back/internal/application/auth"
	"trample-back/internal/domain/auth"
)

type AuthHandler struct {
	register    *appAuth.RegisterUseCase
	login       *appAuth.LoginUseCase
	secureCookie bool
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

func NewAuthHandler(register *appAuth.RegisterUseCase, login *appAuth.LoginUseCase, secureCookie bool) *AuthHandler {
	return &AuthHandler{register: register, login: login, secureCookie: secureCookie}
}

func (h *AuthHandler) setAuthCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     AuthCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.secureCookie,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int((24 * time.Hour).Seconds()),
	})
}

func clearAuthCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     AuthCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

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

	h.setAuthCookie(w, token)
	JSON(w, http.StatusCreated, map[string]any{
		"token": token,
		"user":  newAuthUserResponse(user),
	})
}

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

	h.setAuthCookie(w, result.Token)
	JSON(w, http.StatusOK, map[string]any{
		"token": result.Token,
		"user":  newAuthUserResponse(result.User),
	})
}

// Me devuelve los datos del usuario autenticado (leyendo del contexto del JWT en cookie).
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	authUser, ok := AuthFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	JSON(w, http.StatusOK, authUserResponse{
		ID:        authUser.ID,
		FirstName: authUser.FirstName,
		LastName:  authUser.LastName,
		Email:     authUser.Email,
		Role:      authUser.Role,
	})
}

// Logout borra la cookie de sesión.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	clearAuthCookie(w)
	w.WriteHeader(http.StatusNoContent)
}
