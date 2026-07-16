package domain

import "time"

type FileVersion struct {
	ID            string    `json:"id"`
	FileID        string    `json:"file_id"`
	VersionNumber int       `json:"version_number"`
	Size          int64     `json:"size"`
	SHA256        string    `json:"sha256"`
	MIMEType      string    `json:"mime_type"`
	OriginalName  string    `json:"original_name"`
	CreatedBy     string    `json:"created_by"`
	CreatedAt     time.Time `json:"created_at"`
	ObjectKey     string    `json:"-"`
}

type File struct {
	ID             string      `json:"id"`
	WorkspaceID    string      `json:"workspace_id"`
	Name           string      `json:"name"`
	TextEncoding   string      `json:"text_encoding"`
	CurrentVersion FileVersion `json:"current_version"`
	CreatedBy      string      `json:"created_by"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
}

type FilePage struct {
	Items      []File `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

type VersionPage struct {
	Items      []FileVersion `json:"items"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

type StoredObject struct {
	ID       string
	Key      string
	Size     int64
	SHA256   string
	MIMEType string
}
