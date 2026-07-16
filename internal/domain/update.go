package domain

import "time"

type UpdateSession struct {
	ID                string       `json:"id"`
	FileID            string       `json:"file_id"`
	BaseVersionID     string       `json:"base_version_id"`
	ResolvedVersionID string       `json:"resolved_version_id,omitempty"`
	Status            string       `json:"status"`
	ExpiresAt         time.Time    `json:"expires_at"`
	CreatedBy         string       `json:"created_by"`
	CreatedAt         time.Time    `json:"created_at"`
	CompletedAt       time.Time    `json:"completed_at,omitempty"`
	CandidateObjectID string       `json:"-"`
	CandidateKey      string       `json:"-"`
	BaseKey           string       `json:"-"`
	Candidate         DiffMetadata `json:"-"`
	Base              DiffMetadata `json:"-"`
}

type DiffMetadata struct {
	OriginalName string `json:"original_name"`
	MIMEType     string `json:"mime_type"`
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256"`
}

type DiffResult struct {
	Kind            string       `json:"kind"`
	Reason          string       `json:"reason"`
	UnifiedDiff     string       `json:"unified_diff,omitempty"`
	Base            DiffMetadata `json:"base"`
	Candidate       DiffMetadata `json:"candidate"`
	RollbackWarning bool         `json:"rollback_warning"`
}

type FileLock struct {
	ID         string    `json:"id"`
	FileID     string    `json:"file_id"`
	Status     string    `json:"status"`
	LockedBy   string    `json:"locked_by"`
	CreatedAt  time.Time `json:"created_at"`
	ReleasedAt time.Time `json:"released_at,omitempty"`
	ReleasedBy string    `json:"released_by,omitempty"`
}

type FileLink struct {
	ID        string    `json:"id"`
	FileID    string    `json:"file_id"`
	VersionID string    `json:"version_id,omitempty"`
	Kind      string    `json:"kind"`
	Token     string    `json:"token"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	RevokedAt time.Time `json:"revoked_at,omitempty"`
}

type LinkPage struct {
	Items      []FileLink `json:"items"`
	NextCursor string     `json:"next_cursor,omitempty"`
}

type LinkTarget struct {
	Link          FileLink
	WorkspaceKind WorkspaceKind
	Version       FileVersion
}
