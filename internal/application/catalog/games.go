package catalog

import (
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"trample-back/internal/domain/catalog"
	"trample-back/internal/ports/out"
)

type GamesUseCase struct {
	repo    out.GameRepository
	storage out.ImageStorage
}

func NewGamesUseCase(repo out.GameRepository, storage out.ImageStorage) *GamesUseCase {
	return &GamesUseCase{repo: repo, storage: storage}
}

func (uc *GamesUseCase) ListGames(ctx context.Context) ([]catalog.Game, error) {
	return uc.repo.ListGames(ctx)
}

func (uc *GamesUseCase) ListGamesAdmin(ctx context.Context) ([]catalog.Game, error) {
	return uc.repo.ListGamesAdmin(ctx)
}

func (uc *GamesUseCase) GetGame(ctx context.Context, id int64) (catalog.Game, error) {
	return uc.repo.GetByID(ctx, id)
}

func (uc *GamesUseCase) CreateGame(ctx context.Context, in catalog.GameInput) (catalog.Game, error) {
	return uc.repo.Create(ctx, in)
}

func (uc *GamesUseCase) UpdateGame(ctx context.Context, id int64, in catalog.GameInput) (catalog.Game, error) {
	return uc.repo.Update(ctx, id, in)
}

func (uc *GamesUseCase) SetActive(ctx context.Context, id int64, active bool) (catalog.Game, error) {
	return uc.repo.SetActive(ctx, id, active)
}

func (uc *GamesUseCase) UploadGameIcon(ctx context.Context, id int64, fh *multipart.FileHeader) (catalog.Game, error) {
	return uc.uploadGameImage(ctx, id, fh, "icon")
}

func (uc *GamesUseCase) UploadGameLogo(ctx context.Context, id int64, fh *multipart.FileHeader) (catalog.Game, error) {
	return uc.uploadGameImage(ctx, id, fh, "logo")
}

func (uc *GamesUseCase) uploadGameImage(ctx context.Context, id int64, fh *multipart.FileHeader, kind string) (catalog.Game, error) {
	if uc.storage == nil {
		return catalog.Game{}, errors.New("subida de imágenes no configurada")
	}
	f, err := fh.Open()
	if err != nil {
		return catalog.Game{}, err
	}
	defer f.Close()
	content, err := io.ReadAll(f)
	if err != nil {
		return catalog.Game{}, err
	}
	ct := fh.Header.Get("Content-Type")
	if ct == "" {
		ct = http.DetectContentType(content)
	}
	if !strings.HasPrefix(ct, "image/") {
		return catalog.Game{}, errors.New("solo se permiten imágenes")
	}
	upload := catalog.ImageUpload{ContentType: ct, Content: content}
	url, err := uc.storage.PutGameImage(ctx, id, kind, upload)
	if err != nil {
		return catalog.Game{}, err
	}
	if kind == "icon" {
		if err := uc.repo.AttachIconImage(ctx, id, url, nil); err != nil {
			return catalog.Game{}, err
		}
	} else {
		if err := uc.repo.AttachLogoImage(ctx, id, url, nil); err != nil {
			return catalog.Game{}, err
		}
	}
	return uc.repo.GetByID(ctx, id)
}
