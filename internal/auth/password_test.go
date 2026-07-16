package auth

import "testing"

func TestPasswordHashAndVerify(t *testing.T) {
	hasher := PasswordHasher{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	encoded, err := hasher.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	verified, err := VerifyPassword(encoded, "correct horse battery staple")
	if err != nil || !verified {
		t.Fatalf("VerifyPassword(correct) = %v, %v", verified, err)
	}
	verified, err = VerifyPassword(encoded, "incorrect password")
	if err != nil {
		t.Fatalf("VerifyPassword(incorrect) error = %v", err)
	}
	if verified {
		t.Fatal("VerifyPassword(incorrect) = true")
	}
}

func TestPasswordRejectsShortValue(t *testing.T) {
	if _, err := DefaultPasswordHasher().Hash("too-short"); err == nil {
		t.Fatal("Hash() error = nil, want short password error")
	}
}

func TestTokenGeneration(t *testing.T) {
	raw, hash, err := NewToken()
	if err != nil {
		t.Fatalf("NewToken() error = %v", err)
	}
	if raw == "" || len(hash) != sha256Size {
		t.Fatalf("token lengths = raw %d, hash %d", len(raw), len(hash))
	}
	if got := HashToken(raw); string(got) != string(hash) {
		t.Fatal("HashToken(raw) does not match generated hash")
	}
}

const sha256Size = 32
