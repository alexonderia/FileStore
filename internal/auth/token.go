package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

const tokenBytes = 32

func NewToken() (string, []byte, error) {
	random := make([]byte, tokenBytes)
	if _, err := rand.Read(random); err != nil {
		return "", nil, fmt.Errorf("generate token: %w", err)
	}
	raw := base64.RawURLEncoding.EncodeToString(random)
	return raw, HashToken(raw), nil
}

func HashToken(raw string) []byte {
	hash := sha256.Sum256([]byte(raw))
	return hash[:]
}
