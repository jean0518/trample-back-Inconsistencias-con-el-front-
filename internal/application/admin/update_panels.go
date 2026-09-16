package admin

import (
	"context"

	"trample-back/internal/ports/out"
)

type UpdatePanelsInput struct {
	UserID int64
	Panels []string
}

type UpdatePanelsUseCase struct {
	staff  out.AdminUserRepository
	panels out.PanelRepository
}

func NewUpdatePanelsUseCase(staff out.AdminUserRepository, panels out.PanelRepository) *UpdatePanelsUseCase {
	return &UpdatePanelsUseCase{staff: staff, panels: panels}
}

func (uc *UpdatePanelsUseCase) Execute(ctx context.Context, in UpdatePanelsInput) error {
	all, err := uc.panels.GetAll(ctx)
	if err != nil {
		return err
	}
	known := make(map[string]struct{}, len(all))
	for _, p := range all {
		known[p.Code] = struct{}{}
	}
	for _, p := range in.Panels {
		if _, ok := known[p]; !ok {
			return ErrInvalidPanel
		}
	}

	// Recalcula la etiqueta del rol según los paneles elegidos: si coinciden
	// con el preset de un rol se usa ese rol; si no, "personalizado".
	role := ResolveRoleLabel("", in.Panels)
	return uc.staff.UpdatePanels(ctx, in.UserID, in.Panels, role)
}