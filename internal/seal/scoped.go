package seal

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
)

const scopedPrefix = "encs."

// IsScoped reports whether tok uses the scope-separated authenticated token format.
// Scoped tokens are intentionally distinct from legacy config tokens (enc.1.*).
func IsScoped(tok string) bool {
	return strings.HasPrefix(tok, scopedPrefix)
}

func scopedAAD(scope string) ([]byte, error) {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return nil, errors.New("scope is required")
	}
	return []byte("stremio-easynews-go:" + scope + ":v1"), nil
}

// SealScoped encrypts and authenticates plaintext using the shared ADDON_CONFIG_KEY,
// while binding the ciphertext to a caller-defined scope through AEAD additional data.
// A token sealed for one scope cannot be opened under another scope.
func SealScoped(scope string, plaintext []byte) (string, error) {
	if aead == nil {
		return "", errors.New("seal not initialized")
	}
	aad, err := scopedAAD(scope)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, nonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ct := aead.Seal(nil, nonce, plaintext, aad)
	blob := append(append([]byte{}, nonce...), ct...)
	return scopedPrefix + string(keyID) + "." + base64.RawURLEncoding.EncodeToString(blob), nil
}

// OpenScoped decrypts a scope-separated token and verifies both the GCM tag and
// the caller-provided scope. Config tokens and resolver tokens are therefore not
// interchangeable even though they use the same root key.
func OpenScoped(scope, tok string) ([]byte, error) {
	if aead == nil {
		return nil, errors.New("seal not initialized")
	}
	if !IsScoped(tok) {
		return nil, errors.New("not a scoped sealed token")
	}
	aad, err := scopedAAD(scope)
	if err != nil {
		return nil, err
	}

	parts := strings.SplitN(tok, ".", 3)
	if len(parts) != 3 || parts[0] != "encs" || parts[1] != string(keyID) {
		return nil, errors.New("invalid scoped token format")
	}
	if parts[2] == "" {
		return nil, errors.New("empty scoped token body")
	}
	blob, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, err
	}
	if len(blob) < nonceLen+16 {
		return nil, errors.New("scoped token too short")
	}
	return aead.Open(nil, blob[:nonceLen], blob[nonceLen:], aad)
}
