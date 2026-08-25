package resolve

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/kiskey/stremio-easynews-go/internal/seal"
)

func initResolveTestSeal(t *testing.T) {
	t.Helper()
	t.Setenv("ADDON_CONFIG_KEY", base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	if err := seal.Init(); err != nil {
		t.Fatalf("seal.Init: %v", err)
	}
}

func TestSecureResolvePayloadRoundTrip(t *testing.T) {
	initResolveTestSeal(t)
	t.Setenv("ALLOW_LEGACY_RESOLVE_PAYLOADS", "false")

	token, err := SealResolveTarget("https://members.easynews.com/path/file.mkv?x=1", "alice", "super-secret")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, "encs.1.") {
		t.Fatalf("unexpected token format: %q", token)
	}
	if strings.Contains(token, "alice") || strings.Contains(token, "super-secret") {
		t.Fatal("secure resolver token exposed plaintext credentials")
	}

	target, err := ParseSecureResolvePayload(token)
	if err != nil {
		t.Fatal(err)
	}
	if target.CleanUrl != "https://members.easynews.com/path/file.mkv?x=1" {
		t.Fatalf("unexpected clean URL: %q", target.CleanUrl)
	}
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:super-secret"))
	if target.AuthHeader != wantAuth {
		t.Fatalf("unexpected auth header: %q", target.AuthHeader)
	}
}

func TestSecureResolvePayloadRejectsTamperAndForeignHost(t *testing.T) {
	initResolveTestSeal(t)

	token, err := SealResolveTarget("https://members.easynews.com/path/file.mkv", "alice", "secret")
	if err != nil {
		t.Fatal(err)
	}
	i := len(token) / 2
	replacement := byte('A')
	if token[i] == replacement {
		replacement = 'B'
	}
	tampered := token[:i] + string(replacement) + token[i+1:]
	if _, err := ParseSecureResolvePayload(tampered); err == nil {
		t.Fatal("tampered resolver token must fail")
	}

	if _, err := SealResolveTarget("https://evil.example/file.mkv", "alice", "secret"); err == nil {
		t.Fatal("foreign host must not be sealable as an Easynews resolver target")
	}
}

func TestLegacyResolvePayloadRequiresExplicitOptIn(t *testing.T) {
	legacyURL := "https://members.easynews.com/path/file.mkv?u=alice&p=secret"
	legacy := base64.RawURLEncoding.EncodeToString([]byte(legacyURL))

	t.Setenv("ALLOW_LEGACY_RESOLVE_PAYLOADS", "false")
	if _, err := ParseSecureResolvePayload(legacy); err == nil {
		t.Fatal("legacy Base64 payload must be rejected by default")
	}

	t.Setenv("ALLOW_LEGACY_RESOLVE_PAYLOADS", "true")
	target, err := ParseSecureResolvePayload(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(target.CleanUrl, "alice") || strings.Contains(target.CleanUrl, "secret") || strings.Contains(target.CleanUrl, "?u=") {
		t.Fatalf("legacy parser must strip credentials from upstream URL: %q", target.CleanUrl)
	}
}

func TestSecureResolverCacheUsesSharedTTLCache(t *testing.T) {
	old := secureResolverCache
	secureResolverCache = nil
	defer func() { secureResolverCache = old }()

	// Construct through the same shared cache implementation with tiny capacity.
	secureResolverCache = newTestResolverCache(2)
	SetSecureCachedResolvedURL("a", "https://cdn.example/a")
	SetSecureCachedResolvedURL("b", "https://cdn.example/b")
	if _, ok := GetSecureCachedResolvedURL("a"); !ok {
		t.Fatal("expected cache hit")
	}
	SetSecureCachedResolvedURL("c", "https://cdn.example/c")
	stats := SecureResolverCacheStats()
	if stats.Capacity != 2 || stats.Evictions != 1 {
		t.Fatalf("unexpected resolver cache stats: %+v", stats)
	}
}
