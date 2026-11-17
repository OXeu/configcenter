package apikey

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

const (
	prefix   = "cc_"
	keyBytes = 32
)

// ErrMalformed is returned for headers that do not carry a Bearer credential.
var ErrMalformed = errors.New("malformed api key header")

// Generate returns a plaintext bearer token and the digest persisted with it.
func Generate() (token string, digest [32]byte, err error) {
	buf := make([]byte, keyBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", digest, fmt.Errorf("read random bytes: %w", err)
	}
	token = prefix + base64.RawURLEncoding.EncodeToString(buf)
	return token, Hash(token), nil
}

// Hash derives the stored digest of a token.
func Hash(token string) [32]byte {
	return sha256.Sum256([]byte(token))
}

// Display returns a short non-secret identifier used in listings.
func Display(token string) string {
	if len(token) <= len(prefix)+6 {
		return token
	}
	return token[:len(prefix)+6] + "…"
}

// ParseHeader extracts the token from an `Authorization: Bearer <token>` header.
func ParseHeader(header string) (string, error) {
	scheme, rest, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", ErrMalformed
	}
	token := strings.TrimSpace(rest)
	if !strings.HasPrefix(token, prefix) || len(token) == len(prefix) || strings.ContainsAny(token, " \t\r\n") {
		return "", ErrMalformed
	}
	return token, nil
}
