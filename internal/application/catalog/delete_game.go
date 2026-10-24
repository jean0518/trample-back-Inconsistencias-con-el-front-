package catalog

import (
	"context"
	"errors"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"trample-back/internal/ports/out"
)

var (
	ErrDirectAdminRequired = errors.New("solo un administrador directo puede eliminar juegos")
	ErrDeleteConfirmation  = errors.New("confirma tu correo y contraseña para eliminar el juego")
)

// DeleteGameUseCase elimina un juego y sus productos. Es una acción crítica:
// exige un administrador directo (no creado/editable desde el panel) y que
// vuelva a confirmar su correo y contraseña.
type DeleteGameUseCase struct {
	games out.GameRepository
	users out.UserRepository
}

func NewDeleteGameUseCase(games out.GameRepository, users out.UserRepository) *DeleteGameUseCase {
	return &DeleteGameUseCase{games: games, users: users}
}

func (uc *DeleteGameUseCase) Execute(ctx context.Context, actorID string, gameID int64, email, password string) (int64, error) {
	user, err := uc.users.FindByID(ctx, actorID)
	if err != nil {
		return 0, ErrDirectAdminRequired
	}
	if !user.IsDirectAdmin {
		return 0, ErrDirectAdminRequired
	}
	if !strings.EqualFold(strings.TrimSpace(email), user.Email) ||
		bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)) != nil {
		return 0, ErrDeleteConfirmation
	}
	return uc.games.DeleteWithProducts(ctx, gameID)
}
