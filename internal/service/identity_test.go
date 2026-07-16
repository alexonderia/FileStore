package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alexonderia/filestore/internal/auth"
	"github.com/alexonderia/filestore/internal/domain"
)

type identityRepositoryStub struct {
	user         domain.User
	passwordHash string
	tokenHash    []byte
	revokedHash  []byte
}

func (repository *identityRepositoryStub) Register(_ context.Context, name, email, passwordHash string, tokenHash []byte, _ time.Time) (domain.User, error) {
	repository.user = domain.User{ID: "user-id", Name: name, Email: email}
	repository.passwordHash = passwordHash
	repository.tokenHash = append([]byte(nil), tokenHash...)
	return repository.user, nil
}

func (repository *identityRepositoryStub) CredentialsByEmail(_ context.Context, email string) (domain.User, string, error) {
	if repository.user.Email != email {
		return domain.User{}, "", domain.ErrNotFound
	}
	return repository.user, repository.passwordHash, nil
}

func (repository *identityRepositoryStub) CreateToken(_ context.Context, _ string, tokenHash []byte, _ time.Time) error {
	repository.tokenHash = append([]byte(nil), tokenHash...)
	return nil
}

func (repository *identityRepositoryStub) Authenticate(_ context.Context, tokenHash []byte, _ time.Time) (domain.Actor, error) {
	if string(tokenHash) != string(repository.tokenHash) || string(tokenHash) == string(repository.revokedHash) {
		return domain.Actor{}, domain.ErrUnauthorized
	}
	return domain.Actor{User: repository.user}, nil
}

func (repository *identityRepositoryStub) RevokeToken(_ context.Context, tokenHash []byte, _ time.Time) error {
	repository.revokedHash = append([]byte(nil), tokenHash...)
	return nil
}

func TestIdentityLifecycleStoresOnlyTokenHash(t *testing.T) {
	repository := &identityRepositoryStub{}
	hasher := auth.PasswordHasher{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	identity := NewIdentity(repository, hasher, time.Hour)
	result, err := identity.Register(context.Background(), " User ", "USER@example.test", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if result.User.Email != "user@example.test" || result.User.Name != "User" || result.User.IsSuperadmin {
		t.Fatalf("registered user = %#v", result.User)
	}
	if result.Token == "" || string(repository.tokenHash) == result.Token || string(repository.tokenHash) != string(auth.HashToken(result.Token)) {
		t.Fatal("repository did not receive only the token hash")
	}
	if _, err := identity.Authenticate(context.Background(), result.Token); err != nil {
		t.Fatal(err)
	}
	if err := identity.Logout(context.Background(), result.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Authenticate(context.Background(), result.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("authenticate revoked token error = %v", err)
	}
}

func TestIdentityRejectsInvalidInputAndPassword(t *testing.T) {
	repository := &identityRepositoryStub{}
	hasher := auth.PasswordHasher{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	identity := NewIdentity(repository, hasher, time.Hour)
	if _, err := identity.Register(context.Background(), "", "bad", "short"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("register error = %v, want invalid", err)
	}
	if _, err := identity.Login(context.Background(), "missing@example.test", "wrong"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("login error = %v, want unauthorized", err)
	}
}
