package service

import (
	"context"
	"testing"

	"github.com/alexonderia/filestore/internal/auth"
)

type bootstrapRepositoryStub struct {
	name         string
	email        string
	passwordHash string
}

func (repository *bootstrapRepositoryStub) BootstrapSuperadmin(_ context.Context, name, email, passwordHash string) (string, error) {
	repository.name = name
	repository.email = email
	repository.passwordHash = passwordHash
	return "user-id", nil
}

func TestBootstrapSuperadmin(t *testing.T) {
	repository := &bootstrapRepositoryStub{}
	hasher := auth.PasswordHasher{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	result, err := NewBootstrap(repository, hasher).Superadmin(context.Background(), " Admin ", "ADMIN@example.test", "correct horse battery staple")
	if err != nil {
		t.Fatalf("Superadmin() error = %v", err)
	}
	if result != "user-id" || repository.name != "Admin" || repository.email != "admin@example.test" {
		t.Fatalf("result = %q, name = %q, email = %q", result, repository.name, repository.email)
	}
	verified, err := auth.VerifyPassword(repository.passwordHash, "correct horse battery staple")
	if err != nil || !verified {
		t.Fatalf("stored password hash did not verify: %v, %v", verified, err)
	}
}

func TestBootstrapRejectsInvalidInput(t *testing.T) {
	repository := &bootstrapRepositoryStub{}
	hasher := auth.PasswordHasher{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	service := NewBootstrap(repository, hasher)
	if _, err := service.Superadmin(context.Background(), "", "bad", "short"); err == nil {
		t.Fatal("Superadmin() error = nil, want validation error")
	}
}
