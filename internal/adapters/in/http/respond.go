package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
)

func JSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func Error(w http.ResponseWriter, status int, msg string) {
	JSON(w, status, map[string]string{"error": msg})
}

// internalError responde 500 sin filtrar el error al cliente (puede traer nombres
// de tablas o consultas) y lo deja en el log para diagnosticarlo.
func internalError(w http.ResponseWriter, err error) {
	slog.Error("error interno", slog.Any("error", err))
	Error(w, http.StatusInternalServerError, "error interno, inténtalo de nuevo")
}

func Decode(r *http.Request, dst any) error {
	return json.NewDecoder(r.Body).Decode(dst)
}

func DecodeBytes(data []byte, dst any) error {
	return json.Unmarshal(data, dst)
}

// friendlyErr convierte errores internos en mensajes legibles para el usuario.
// Detecta el origen (Scrydex, TRM, búsqueda expirada) y devuelve un texto
// claro; cualquier otro error pasa tal cual para no perder contexto útil.
func friendlyErr(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "scrydex:"):
		return "Hubo un error al consultar Scrydex. Por favor intentalo de nuevo."
	case strings.Contains(msg, "obtener TRM") || strings.Contains(msg, "trm:"):
		return "No pudimos obtener la tasa de cambio (TRM) del Banco de la República. Por favor intentalo de nuevo en unos minutos."
	case strings.Contains(msg, "expiró"):
		return msg // ya tiene instrucción de acción ("repetila e intentá de nuevo")
	default:
		return msg
	}
}
