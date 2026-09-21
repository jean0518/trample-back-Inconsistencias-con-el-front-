package admin

import (
	"context"

	"trample-back/internal/ports/out"
)

type PanelsListUseCase struct {
	panels out.PanelRepository
}

func NewPanelsListUseCase(panels out.PanelRepository) *PanelsListUseCase {
	return &PanelsListUseCase{panels: panels}
}

func (uc *PanelsListUseCase) Execute(ctx context.Context) ([]out.Panel, error) {
	return uc.panels.GetAll(ctx)
}