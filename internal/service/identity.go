package service

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"

	"github.com/alexonderia/filestore/internal/auth"
	"github.com/alexonderia/filestore/internal/domain"
)

type IdentityRepository interface {
	Register(context.Context, string, string, string, []byte, time.Time) (domain.User, error)
	CredentialsByEmail(context.Context, string) (domain.User, string, error)
	CreateToken(context.Context, string, []byte, time.Time) error
	Authenticate(context.Context, []byte, time.Time) (domain.Actor, error)
	RevokeToken(context.Context, []byte, time.Time) error
}

type Identity struct {
	users    IdentityRepository
	hasher   auth.PasswordHasher
	tokenTTL time.Duration
	now      func() time.Time
}

func NewIdentity(users IdentityRepository, hasher auth.PasswordHasher, tokenTTL time.Duration) *Identity {
	return &Identity{users: users, hasher: hasher, tokenTTL: tokenTTL, now: time.Now}
}

func (service *Identity) Register(ctx context.Context, name, email, password string) (domain.AuthResult, error) {
	name = strings.TrimSpace(name)
	email, err := normalizeEmail(email)
	if err != nil || name == "" || len(name) > 200 {
		return domain.AuthResult{}, ErrInvalid
	}
	passwordHash, err := service.hasher.Hash(password)
	if err != nil {
		return domain.AuthResult{}, errors.Join(ErrInvalid, err)
	}
	rawToken, tokenHash, err := auth.NewToken()
	if err != nil {
		return domain.AuthResult{}, err
	}
	user, err := service.users.Register(ctx, name, email, passwordHash, tokenHash, service.now().Add(service.tokenTTL))
	if err != nil {
		return domain.AuthResult{}, err
	}
	return domain.AuthResult{Token: rawToken, User: user}, nil
}

func (service *Identity) Login(ctx context.Context, email, password string) (domain.AuthResult, error) {
	email, err := normalizeEmail(email)
	if err != nil || password == "" {
		return domain.AuthResult{}, ErrUnauthorized
	}
	user, passwordHash, err := service.users.CredentialsByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return domain.AuthResult{}, ErrUnauthorized
		}
		return domain.AuthResult{}, err
	}
	valid, err := auth.VerifyPassword(passwordHash, password)
	if err != nil || !valid {
		return domain.AuthResult{}, ErrUnauthorized
	}
	rawToken, tokenHash, err := auth.NewToken()
	if err != nil {
		return domain.AuthResult{}, err
	}
	if err := service.users.CreateToken(ctx, user.ID, tokenHash, service.now().Add(service.tokenTTL)); err != nil {
		return domain.AuthResult{}, err
	}
	return domain.AuthResult{Token: rawToken, User: user}, nil
}

func (service *Identity) Authenticate(ctx context.Context, rawToken string) (domain.Actor, error) {
	if strings.TrimSpace(rawToken) == "" {
		return domain.Actor{}, ErrUnauthorized
	}
	return service.users.Authenticate(ctx, auth.HashToken(rawToken), service.now())
}

func (service *Identity) Logout(ctx context.Context, rawToken string) error {
	if strings.TrimSpace(rawToken) == "" {
		return ErrUnauthorized
	}
	return service.users.RevokeToken(ctx, auth.HashToken(rawToken), service.now())
}

func normalizeEmail(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	address, err := mail.ParseAddress(value)
	if err != nil || !strings.EqualFold(address.Address, value) || len(value) > 320 {
		return "", ErrInvalid
	}
	return value, nil
}
