package http

import (
	"errors"
	"net/http"
	"strconv"

	appReservation "trample-back/internal/application/reservation"
	"trample-back/internal/domain/product"
	"trample-back/internal/domain/reservation"
)

// AdminReservationHandler expone acciones de administración sobre las reservas
// de cartas y de productos.
type AdminReservationHandler struct {
	reservations *appReservation.AdminUseCase
}

func NewAdminReservationHandler(reservations *appReservation.AdminUseCase) *AdminReservationHandler {
	return &AdminReservationHandler{reservations: reservations}
}

// Release libera una reserva activa para que el stock vuelva a estar
// disponible. El panel la usa para destrabar reservas que quedaron abiertas.
//
//	@Summary      Liberar reserva
//	@Tags         admin
//	@Security     BearerAuth
//	@Param        kind  path  string  true  "card | product"
//	@Param        id    path  int     true  "ID de la reserva"
//	@Success      204
//	@Failure      400  {object}  object{error=string}
//	@Failure      404  {object}  object{error=string}
//	@Router       /admin/reservations/{kind}/{id} [delete]
func (h *AdminReservationHandler) Release(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		Error(w, http.StatusBadRequest, "id inválido")
		return
	}

	err = h.reservations.Release(r.Context(), kind, id)
	switch {
	case errors.Is(err, reservation.ErrInvalidKind):
		Error(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, reservation.ErrNotFound), errors.Is(err, product.ErrNotFound):
		Error(w, http.StatusNotFound, "reserva no encontrada o ya liberada")
	case err != nil:
		internalError(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
