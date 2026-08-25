package addon

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/kiskey/stremio-easynews-go/internal/resolve"
	"github.com/kiskey/stremio-easynews-go/internal/seal"
)

func initAddonTestSeal(t *testing.T) {
	t.Helper()
	t.Setenv("ADDON_CONFIG_KEY", base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	if err := seal.Init(); err != nil {
		t.Fatalf("seal.Init: %v", err)
	}
}

func TestCreateSecureStreamURLUsesEncryptedResolverPayload(t *testing.T) {
	initAddonTestSeal(t)
	t.Setenv("ADDON_BASE_URL", "https://addon.example")
	t.Setenv("ALLOW_LEGACY_RESOLVE_PAYLOADS", "false")
	t.Setenv("ALLOW_INSECURE_CREDENTIAL_URLS", "false")

	streamURL, err := CreateSecureStreamURL(
		"https://members.easynews.com", "farm", 443,
		"alice", "super-secret", "hash.mkv/My Movie.mkv", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(streamURL, "https://addon.example/resolve/encs.1.") {
		t.Fatalf("unexpected secure stream URL: %q", streamURL)
	}
	if strings.Contains(streamURL, "alice") || strings.Contains(streamURL, "super-secret") {
		t.Fatal("secure stream URL must not expose Easynews credentials")
	}

	parts := strings.Split(streamURL, "/resolve/")
	if len(parts) != 2 {
		t.Fatalf("missing resolver route: %q", streamURL)
	}
	payload := strings.SplitN(parts[1], "/", 2)[0]
	target, err := resolve.ParseSecureResolvePayload(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(target.CleanUrl, "members.easynews.com") {
		t.Fatalf("unexpected target: %q", target.CleanUrl)
	}
}

func TestCreateSecureStreamURLFailsClosedWithoutSeal(t *testing.T) {
	// This assertion validates the helper's policy without mutating the process-wide
	// seal singleton: a missing base URL remains a hard configuration failure.
	t.Setenv("ADDON_BASE_URL", "")
	t.Setenv("ALLOW_INSECURE_CREDENTIAL_URLS", "false")
	_, err := CreateSecureStreamURL("https://members.easynews.com", "farm", 443, "alice", "secret", "file.mkv", "")
	if err == nil || !isStreamURLConfigurationError(err) {
		t.Fatalf("expected secure stream configuration error, got %v", err)
	}
}
