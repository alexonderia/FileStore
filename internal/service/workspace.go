package service

import (
	"context"
	"log/slog"
	"regexp"
	"strings"

	"github.com/alexonderia/filestore/internal/authorization"
	"github.com/alexonderia/filestore/internal/domain"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type WorkspaceRepository interface {
	Base(context.Context) (domain.Workspace, error)
	Access(context.Context, string, string) (domain.Workspace, *domain.WorkspaceRole, error)
	List(context.Context, string, bool) ([]domain.Workspace, error)
	Create(context.Context, string, string) (domain.Workspace, error)
	PutMember(context.Context, string, string, string, string, domain.WorkspaceRole) (domain.WorkspaceMember, error)
	RemoveMember(context.Context, string, string, string) error
}

type Workspace struct {
	repository WorkspaceRepository
	policy     authorization.Policy
	logger     *slog.Logger
}

func NewWorkspace(repository WorkspaceRepository, logger *slog.Logger) *Workspace {
	return &Workspace{repository: repository, policy: authorization.Policy{}, logger: logger}
}

func (service *Workspace) Base(ctx context.Context, _ domain.User) (domain.Workspace, error) {
	return service.repository.Base(ctx)
}

func (service *Workspace) List(ctx context.Context, actor domain.User) ([]domain.Workspace, error) {
	return service.repository.List(ctx, actor.ID, actor.IsSuperadmin)
}

func (service *Workspace) Get(ctx context.Context, actor domain.User, workspaceID string) (domain.Workspace, error) {
	if !validUUID(workspaceID) {
		return domain.Workspace{}, ErrInvalid
	}
	workspace, role, err := service.repository.Access(ctx, workspaceID, actor.ID)
	if err != nil {
		return domain.Workspace{}, err
	}
	if !service.policy.CanReadWorkspace(actor, workspace, role) {
		return domain.Workspace{}, ErrNotFound
	}
	service.auditSuperadmin(actor, "workspace_read", workspaceID, "")
	return workspace, nil
}

func (service *Workspace) Create(ctx context.Context, actor domain.User, name string) (domain.Workspace, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 200 || strings.EqualFold(name, "base") {
		return domain.Workspace{}, ErrInvalid
	}
	return service.repository.Create(ctx, name, actor.ID)
}

func (service *Workspace) PutMember(ctx context.Context, actor domain.User, workspaceID, userID, email string, role domain.WorkspaceRole) (domain.WorkspaceMember, error) {
	if !validUUID(workspaceID) || (!validUUID(userID) && email == "") || !authorization.ValidRole(role) {
		return domain.WorkspaceMember{}, ErrInvalid
	}
	if email != "" {
		var err error
		email, err = normalizeEmail(email)
		if err != nil {
			return domain.WorkspaceMember{}, ErrInvalid
		}
	}
	workspace, actorRole, err := service.repository.Access(ctx, workspaceID, actor.ID)
	if err != nil {
		return domain.WorkspaceMember{}, err
	}
	if !service.policy.CanManageMembers(actor, workspace, actorRole) {
		if actorRole == nil && !actor.IsSuperadmin {
			return domain.WorkspaceMember{}, ErrNotFound
		}
		return domain.WorkspaceMember{}, ErrForbidden
	}
	member, err := service.repository.PutMember(ctx, workspaceID, actor.ID, userID, email, role)
	if err == nil {
		service.auditSuperadmin(actor, "workspace_member_put", workspaceID, member.User.ID)
	}
	return member, err
}

func (service *Workspace) RemoveMember(ctx context.Context, actor domain.User, workspaceID, userID string) error {
	if !validUUID(workspaceID) || !validUUID(userID) {
		return ErrInvalid
	}
	workspace, actorRole, err := service.repository.Access(ctx, workspaceID, actor.ID)
	if err != nil {
		return err
	}
	if !service.policy.CanManageMembers(actor, workspace, actorRole) {
		if actorRole == nil && !actor.IsSuperadmin {
			return ErrNotFound
		}
		return ErrForbidden
	}
	if err := service.repository.RemoveMember(ctx, workspaceID, actor.ID, userID); err != nil {
		return err
	}
	service.auditSuperadmin(actor, "workspace_member_remove", workspaceID, userID)
	return nil
}

func (service *Workspace) auditSuperadmin(actor domain.User, action, workspaceID, targetUserID string) {
	if !actor.IsSuperadmin || service.logger == nil {
		return
	}
	service.logger.Info("security event", "event", action, "actor_user_id", actor.ID, "workspace_id", workspaceID, "target_user_id", targetUserID)
}

func validUUID(value string) bool {
	return uuidPattern.MatchString(value)
}
