package api

import (
	"strings"
	"testing"
)

func TestCredentialFingerprintDeterministicAndIsolated(t *testing.T) {
	t.Parallel()

	a := CredentialFingerprint("alice", "secret-one")
	b := CredentialFingerprint("alice", "secret-one")
	c := CredentialFingerprint("alice", "secret-two")
	d := CredentialFingerprint("bob", "secret-one")

	if a != b {
		t.Fatal("same credentials must produce the same fingerprint")
	}
	if a == c {
		t.Fatal("changing password must change fingerprint")
	}
	if a == d {
		t.Fatal("changing username must change fingerprint")
	}
	if len(a) != 64 {
		t.Fatalf("expected SHA-256 hex fingerprint length 64, got %d", len(a))
	}
	if strings.Contains(a, "alice") || strings.Contains(a, "secret-one") {
		t.Fatal("fingerprint must not contain plaintext credential material")
	}
}

func TestNewEasynewsAPIUsesCredentialFingerprint(t *testing.T) {
	t.Parallel()

	api, err := NewEasynewsAPI("alice", "secret-one")
	if err != nil {
		t.Fatalf("NewEasynewsAPI returned error: %v", err)
	}

	want := CredentialFingerprint("alice", "secret-one")
	if api.GetCredKey() != want {
		t.Fatalf("unexpected credential key: got %q want %q", api.GetCredKey(), want)
	}
}
