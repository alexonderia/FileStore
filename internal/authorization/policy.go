package authorization

import "github.com/alexonderia/filestore/internal/domain"

// Policy is the single role matrix used by workspace and future file services.
type Policy struct{}

func (Policy) CanReadWorkspace(actor domain.User, workspace domain.Workspace, role *domain.WorkspaceRole) bool {
	if actor.IsSuperadmin || workspace.Kind == domain.WorkspaceBase {
		return true
	}
	return role != nil
}

func (Policy) CanManageMembers(actor domain.User, workspace domain.Workspace, role *domain.WorkspaceRole) bool {
	if workspace.Kind != domain.WorkspacePrivate {
		return false
	}
	return actor.IsSuperadmin || (role != nil && *role == domain.RoleOwner)
}

func ValidRole(role domain.WorkspaceRole) bool {
	return role == domain.RoleOwner || role == domain.RoleEditor || role == domain.RoleViewer
}

func (Policy) CanCreateFile(actor domain.User, workspace domain.Workspace, role *domain.WorkspaceRole) bool {
	if actor.IsSuperadmin || workspace.Kind == domain.WorkspaceBase {
		return true
	}
	return role != nil && (*role == domain.RoleOwner || *role == domain.RoleEditor)
}

func (Policy) CanReadFile(actor domain.User, workspace domain.Workspace, role *domain.WorkspaceRole, createdBy string) bool {
	if actor.IsSuperadmin {
		return true
	}
	if workspace.Kind == domain.WorkspaceBase {
		return actor.ID == createdBy
	}
	return role != nil
}

func (Policy) CanWriteFile(actor domain.User, workspace domain.Workspace, role *domain.WorkspaceRole, createdBy string) bool {
	if actor.IsSuperadmin {
		return true
	}
	if workspace.Kind == domain.WorkspaceBase {
		return actor.ID == createdBy
	}
	return role != nil && (*role == domain.RoleOwner || *role == domain.RoleEditor)
}

func (Policy) CanUnlock(actor domain.User, workspace domain.Workspace, role *domain.WorkspaceRole, createdBy, lockedBy string) bool {
	if actor.IsSuperadmin || actor.ID == createdBy || actor.ID == lockedBy {
		return true
	}
	return workspace.Kind == domain.WorkspacePrivate && role != nil && (*role == domain.RoleOwner || *role == domain.RoleEditor)
}
