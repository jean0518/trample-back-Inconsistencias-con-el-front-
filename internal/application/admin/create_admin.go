package admin

import (
	"context"
	"errors"
	"strings"

	appAuth "trample-back/internal/application/auth"
	"trample-back/internal/domain/auth"
	"trample-back/internal/ports/out"

	"golang.org/x/crypto/bcrypt"
)

// Los errores de validación se reutilizan de application/auth para no duplicar
// mensajes ni lógica (misma semántica en registro y creación de staff).
var (
	ErrInvalidName      = appAuth.ErrInvalidName
	ErrInvalidEmail     = appAuth.ErrInvalidEmail
	ErrPasswordTooShort = appAuth.ErrPasswordTooShort
	ErrInvalidRole      = errors.New("rol inválido: debe ser 'colaborador', 'sup_colaborador' o 'personalizado'")
	ErrInvalidPanel     = errors.New("panel inválido")
)

type CreateAdminInput struct {
	FirstName   string
	LastName    string
	Email       string
	Password    string
	Role        string
	Permissions []string
	CreatedBy   int64
}

type CreateAdminUseCase struct {
	users  out.UserRepository
	staff  out.AdminUserRepository
	panels out.PanelRepository
}

func NewCreateAdminUseCase(users out.UserRepository, staff out.AdminUserRepository, panels out.PanelRepository) *CreateAdminUseCase {
	return &CreateAdminUseCase{users: users, staff: staff, panels: panels}
}

func (uc *CreateAdminUseCase) Execute(ctx context.Context, in CreateAdminInput) (auth.User, error) {
	firstName := strings.TrimSpace(in.FirstName)
	lastName := strings.TrimSpace(in.LastName)
	email := strings.ToLower(strings.TrimSpace(in.Email))

	if firstName == "" || lastName == "" {
		return auth.User{}, ErrInvalidName
	}
	if email == "" {
		return auth.User{}, errors.New("email es requerido")
	}
	if len(in.Password) < 6 {
		return auth.User{}, ErrPasswordTooShort
	}

	role := strings.TrimSpace(in.Role)
	if role != "" {
		if _, ok := validAdminRoles[role]; !ok {
			return auth.User{}, ErrInvalidRole
		}
	}

	var perms []string
	if len(in.Permissions) == 0 {
		if role == "" {
			return auth.User{}, ErrInvalidRole
		}
		defaults, ok := DefaultPermissions(role)
		if !ok {
			return auth.User{}, ErrInvalidRole
		}
		perms = defaults
	} else {
		perms = append([]string{}, in.Permissions...)
		valid, err := uc.validatePanels(ctx, perms)
		if err != nil {
			return auth.User{}, err
		}
		if !valid {
			return auth.User{}, ErrInvalidPanel
		}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return auth.User{}, err
	}

	user, err := uc.users.Create(ctx, auth.User{
		FirstName: firstName,
		LastName:  lastName,
		Email:     email,
		Password:  string(hash),
		Role:      auth.RoleCustomer,
	})
	if err != nil {
		return auth.User{}, err
	}

	// El rol es una etiqueta descriptiva: se queda con el preset si coincide
	// con los permisos elegidos y, si no, se marca como "personalizado".
	user.Role = ResolveRoleLabel(role, perms)
	user.Permissions = perms
	return uc.staff.CreateStaff(ctx, user, in.CreatedBy)
}

func (uc *CreateAdminUseCase) validatePanels(ctx context.Context, panels []string) (bool, error) {
	all, err := uc.panels.GetAll(ctx)
	if err != nil {
		return false, err
	}
	known := make(map[string]struct{}, len(all))
	for _, p := range all {
		known[p.Code] = struct{}{}
	}
	for _, p := range panels {
		if _, ok := known[p]; !ok {
			return false, nil
		}
	}
	return true, nil
}