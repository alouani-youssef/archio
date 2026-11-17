// Package auth holds API key generation and request-scoped identity.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

const (
	keyPrefix    = "ak_"
	prefixLength = 11
)

// GenerateKey returns a new plaintext API key, its display prefix and its
// SHA-256 hex hash. Only the prefix and hash are meant to be stored.
func GenerateKey() (plaintext, prefix, hash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", "", fmt.Errorf("generate api key: %w", err)
	}
	plaintext = keyPrefix + base64.RawURLEncoding.EncodeToString(raw)
	return plaintext, plaintext[:prefixLength], HashKey(plaintext), nil
}

// HashKey returns the SHA-256 hex digest of a plaintext key.
func HashKey(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}
