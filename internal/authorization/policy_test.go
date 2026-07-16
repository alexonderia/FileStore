package authorization

import (
	"testing"

	"github.com/alexonderia/filestore/internal/domain"
)

func TestWorkspacePolicy(t *testing.T) {
	policy := Policy{}
	user := domain.User{}
	admin := domain.User{IsSuperadmin: true}
	private := domain.Workspace{Kind: domain.WorkspacePrivate}
	base := domain.Workspace{Kind: domain.WorkspaceBase}
	owner, editor, viewer := domain.RoleOwner, domain.RoleEditor, domain.RoleViewer

	if !policy.CanReadWorkspace(user, base, nil) || policy.CanManageMembers(user, base, &owner) {
		t.Fatal("base workspace policy is invalid")
	}
	if policy.CanReadWorkspace(user, private, nil) || !policy.CanReadWorkspace(user, private, &viewer) {
		t.Fatal("private workspace read policy is invalid")
	}
	if !policy.CanManageMembers(user, private, &owner) || policy.CanManageMembers(user, private, &editor) || policy.CanManageMembers(user, private, &viewer) {
		t.Fatal("private workspace membership policy is invalid")
	}
	if !policy.CanReadWorkspace(admin, private, nil) || !policy.CanManageMembers(admin, private, nil) {
		t.Fatal("superadmin override is invalid")
	}
}
