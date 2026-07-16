package service

import (
	"context"
	"errors"
	"net/mail"
	"strings"

	"github.com/alexonderia/filestore/internal/auth"
)

type SuperadminRepository interface {
	BootstrapSuperadmin(ctx context.Context, name, email, passwordHash string) (string, error)
}

type Bootstrap struct {
	users  SuperadminRepository
	hasher auth.PasswordHasher
}

func NewBootstrap(users SuperadminRepository, hasher auth.PasswordHasher) *Bootstrap {
	return &Bootstrap{users: users, hasher: hasher}
}

func (service *Bootstrap) Superadmin(ctx context.Context, name, email, password string) (string, error) {
	name = strings.TrimSpace(name)
	email = strings.ToLower(strings.TrimSpace(email))
	if name == "" || len(name) > 200 {
		return "", errors.New("name must contain between 1 and 200 characters")
	}
	address, err := mail.ParseAddress(email)
	if err != nil || !strings.EqualFold(address.Address, email) {
		return "", errors.New("email must be valid")
	}
	passwordHash, err := service.hasher.Hash(password)
	if err != nil {
		return "", err
	}
	return service.users.BootstrapSuperadmin(ctx, name, email, passwordHash)
}
