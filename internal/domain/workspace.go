package domain

import "time"

type WorkspaceKind string

const (
	WorkspaceBase    WorkspaceKind = "base"
	WorkspacePrivate WorkspaceKind = "private"
)

type WorkspaceRole string

const (
	RoleOwner  WorkspaceRole = "owner"
	RoleEditor WorkspaceRole = "editor"
	RoleViewer WorkspaceRole = "viewer"
)

type Workspace struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Kind      WorkspaceKind `json:"kind"`
	CreatedAt time.Time     `json:"created_at"`
}

type WorkspaceMember struct {
	WorkspaceID string        `json:"workspace_id"`
	User        User          `json:"user"`
	Role        WorkspaceRole `json:"role"`
}
