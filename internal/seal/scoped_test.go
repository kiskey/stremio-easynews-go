package seal

import (
	"crypto/aes"
	"crypto/cipher"
	"strings"
	"testing"
)

func installTestAEAD(t *testing.T) {
	t.Helper()
	old := aead
	key := []byte("0123456789abcdef0123456789abcdef")
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	testAEAD, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	aead = testAEAD
	t.Cleanup(func() { aead = old })
}

func TestScopedSealRoundTripAndDomainSeparation(t *testing.T) {
	installTestAEAD(t)

	plaintext := []byte(`{"username":"alice","password":"secret"}`)
	tok, err := SealScoped("resolver", plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if !IsScoped(tok) || !strings.HasPrefix(tok, "encs.1.") {
		t.Fatalf("unexpected scoped token format: %q", tok)
	}
	if strings.Contains(tok, "alice") || strings.Contains(tok, "secret") {
		t.Fatal("scoped token must not expose plaintext")
	}

	got, err := OpenScoped("resolver", tok)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("round trip mismatch: got %q want %q", got, plaintext)
	}
	if _, err := OpenScoped("config", tok); err == nil {
		t.Fatal("scope separation must reject a resolver token under config scope")
	}
}

func TestScopedSealUsesFreshNonceAndRejectsTampering(t *testing.T) {
	installTestAEAD(t)

	a, err := SealScoped("resolver", []byte("same payload"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := SealScoped("resolver", []byte("same payload"))
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("fresh nonces must produce different ciphertext tokens")
	}

	i := len(a) / 2
	replacement := byte('A')
	if a[i] == replacement {
		replacement = 'B'
	}
	tampered := a[:i] + string(replacement) + a[i+1:]
	if _, err := OpenScoped("resolver", tampered); err == nil {
		t.Fatal("tampered scoped token must fail authentication")
	}
}
