package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alexonderia/filestore/internal/domain"
	"github.com/alexonderia/filestore/internal/service"
)

type identityStub struct{}

func (identityStub) Register(_ context.Context, name, email, _ string) (domain.AuthResult, error) {
	return domain.AuthResult{Token: "secret", User: domain.User{ID: "user", Name: name, Email: email}}, nil
}
func (identityStub) Login(context.Context, string, string) (domain.AuthResult, error) {
	return domain.AuthResult{}, service.ErrUnauthorized
}
func (identityStub) Authenticate(_ context.Context, token string) (domain.Actor, error) {
	if token != "valid" {
		return domain.Actor{}, service.ErrUnauthorized
	}
	return domain.Actor{User: domain.User{ID: "user"}}, nil
}
func (identityStub) Logout(context.Context, string) error { return nil }

type workspaceStub struct{}

func (workspaceStub) Base(context.Context, domain.User) (domain.Workspace, error) {
	return domain.Workspace{}, nil
}
func (workspaceStub) List(context.Context, domain.User) ([]domain.Workspace, error) {
	return []domain.Workspace{}, nil
}
func (workspaceStub) Get(context.Context, domain.User, string) (domain.Workspace, error) {
	return domain.Workspace{}, nil
}
func (workspaceStub) Create(context.Context, domain.User, string) (domain.Workspace, error) {
	return domain.Workspace{}, nil
}
func (workspaceStub) PutMember(context.Context, domain.User, string, string, string, domain.WorkspaceRole) (domain.WorkspaceMember, error) {
	return domain.WorkspaceMember{}, nil
}
func (workspaceStub) RemoveMember(context.Context, domain.User, string, string) error { return nil }

func TestHealthEndpoints(t *testing.T) {
	for _, path := range []string{"/health/live", "/health/ready"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			response := httptest.NewRecorder()
			NewHandler().ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
			}
		})
	}
}

func TestUnknownRouteUsesProblemFormat(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/missing", nil)
	response := httptest.NewRecorder()
	NewHandler().ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
	if got := response.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Fatalf("Content-Type = %q", got)
	}
}

func TestProductRoutesAreUnavailableWithoutDatabase(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{}`))
	response := httptest.NewRecorder()
	NewHandler().ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || response.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("status = %d, content type = %q", response.Code, response.Header().Get("Content-Type"))
	}
}

func TestHealthRejectsUnsupportedMethod(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/health/live", nil)
	response := httptest.NewRecorder()
	NewHandler().ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}

func TestProductAuthenticationAndStrictJSON(t *testing.T) {
	handler := NewProductHandler(identityStub{}, workspaceStub{})

	request := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || response.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("anonymous me status = %d, auth header = %q", response.Code, response.Header().Get("WWW-Authenticate"))
	}

	request = httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(`{"name":"User","email":"user@example.test","password":"long enough password","extra":true}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || response.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("strict JSON status = %d, content type = %q", response.Code, response.Header().Get("Content-Type"))
	}
}

func TestLinkTokenIsRedactedFromProblemInstance(t *testing.T) {
	token := strings.Repeat("a", 43)
	request := httptest.NewRequest(http.MethodGet, "/links/"+token+"/content", nil)
	response := httptest.NewRecorder()
	writeServiceError(response, request, service.ErrNotFound)
	if strings.Contains(response.Body.String(), token) {
		t.Fatalf("problem response leaked link token: %s", response.Body.String())
	}
}
