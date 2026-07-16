package domain

import "time"

type User struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Email        string    `json:"email"`
	IsSuperadmin bool      `json:"is_superadmin"`
	CreatedAt    time.Time `json:"created_at"`
}

type Actor struct {
	User    User
	TokenID string
}

type AuthResult struct {
	Token string `json:"token"`
	User  User   `json:"user"`
}
