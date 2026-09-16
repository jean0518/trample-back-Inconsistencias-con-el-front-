package admin

import (
	"context"
	"net/mail"
	"strings"

	"trample-back/internal/domain/auth"
	"trample-back/internal/ports/out"

	"golang.org/x/crypto/bcrypt"
)

type UpdateAdminUserInput struct {
	UserID      int64
	FirstName   string
	LastName    string
	Email       string
	Password    string
	Role        string
	Permissions []string
}

type UpdateAdminUserUseCase struct {
	users  out.UserRepository
	staff  out.AdminUserRepository
	panels out.PanelRepository
}

func NewUpdateAdminUserUseCase(users out.UserRepository, staff out.AdminUserRepository, panels out.PanelRepository) *UpdateAdminUserUseCase {
	return &UpdateAdminUserUseCase{users: users, staff: staff, panels: panels}
}

func (uc *UpdateAdminUserUseCase) Execute(ctx context.Context, in UpdateAdminUserInput) error {
	firstName := strings.TrimSpace(in.FirstName)
	lastName := strings.TrimSpace(in.LastName)
	email := strings.ToLower(strings.TrimSpace(in.Email))

	if firstName == "" || lastName == "" {
		return ErrInvalidName
	}
	if email == "" {
		return ErrInvalidName
	}
	if addr, err := mail.ParseAddress(email); err != nil || addr.Address != email {
		return ErrInvalidEmail
	}
	if in.Password != "" && len(in.Password) < 6 {
		return ErrPasswordTooShort
	}

	role := strings.TrimSpace(in.Role)
	if role != "" && role != auth.RolePersonalizado {
		if _, ok := validAdminRoles[role]; !ok {
			return ErrInvalidRole
		}
	}

	perms := in.Permissions
	if perms == nil {
		perms = []string{}
	}
	valid, err := uc.validatePanels(ctx, perms)
	if err != nil {
		return err
	}
	if !valid {
		return ErrInvalidPanel
	}

	user := auth.User{
		FirstName:   firstName,
		LastName:    lastName,
		Email:       email,
		Permissions: perms,
	}
	if in.Password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		user.Password = string(hash)
	}

	// La etiqueta del rol siempre se recalculó a partir de los paneles.
	return uc.staff.UpdateProfile(ctx, in.UserID, user, ResolveRoleLabel(role, perms))
}

func (uc *UpdateAdminUserUseCase) validatePanels(ctx context.Context, panels []string) (bool, error) {
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