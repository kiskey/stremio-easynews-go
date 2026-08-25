package server

import (
	"strings"
	"testing"

	"github.com/kiskey/stremio-easynews-go/internal/addon"
)

func TestRedactFallbackPathConfiguredRoutes(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"/enc.1.super-secret/manifest.json":               "/<config>/manifest.json",
		"/enc.1.super-secret/stream/movie/tt1234567.json": "/<config>/stream/movie/tt1234567.json",
		"/resolve/dXNlcjpwYXNzd29yZA/movie.mkv":           "/resolve/<payload>/movie.mkv",
		"/enc.1.super-secret":                             "/<config>",
		"/encs.1.encrypted-resolver-token":                "/<config>",
		"/username=alice&password=hunter2":                "/<config>",
		"/manifest.json":                                  "/manifest.json",
		"/configure":                                      "/configure",
	}

	for input, want := range tests {
		input, want := input, want
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			if got := redactFallbackPath(input); got != want {
				t.Fatalf("redactFallbackPath(%q) = %q, want %q", input, got, want)
			}
		})
	}
}

func TestStreamCacheControlValue(t *testing.T) {
	t.Parallel()

	result := addon.StreamHandlerResult{
		CacheMaxAge:     600,
		StaleRevalidate: 3600,
		StaleError:      86400,
	}
	got := streamCacheControlValue(result)
	want := "max-age=600, public, stale-while-revalidate=3600, stale-if-error=86400"
	if got != want {
		t.Fatalf("streamCacheControlValue() = %q, want %q", got, want)
	}

	if got := streamCacheControlValue(addon.StreamHandlerResult{}); got != "" {
		t.Fatalf("zero cache policy should not emit Cache-Control, got %q", got)
	}
}

func TestConfigurationTokenPolicyRejectsPlaintextByDefault(t *testing.T) {
	t.Setenv("ALLOW_LEGACY_PLAINTEXT_CONFIG", "false")
	if !configurationTokenAllowed("") {
		t.Fatal("empty/default configuration must remain allowed")
	}
	if !configurationTokenAllowed("enc.1.opaque-token") {
		t.Fatal("sealed configuration token must be allowed")
	}
	if configurationTokenAllowed("username=alice&password=secret") {
		t.Fatal("plaintext credential configuration must be rejected by default")
	}

	t.Setenv("ALLOW_LEGACY_PLAINTEXT_CONFIG", "true")
	if !configurationTokenAllowed("username=alice&password=secret") {
		t.Fatal("explicit legacy compatibility mode should allow plaintext config")
	}
}

func TestHardenConfigureHTMLRemovesAutomaticPlaintextFallback(t *testing.T) {
	t.Setenv("ALLOW_LEGACY_PLAINTEXT_CONFIG", "false")
	html := "<script>\n" +
		"if (res.ok) {} else { return `${secureHost}/${getLegacyConfigString()}/manifest.json`; }\n" +
		"try {} catch (err) { return `${secureHost}/${getLegacyConfigString()}/manifest.json`; }\n" +
		"</script>"

	got, err := hardenConfigureHTML(html)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, legacyFallbackStatement) {
		t.Fatal("hardened configuration page still contains plaintext fallback")
	}
	if strings.Count(got, "secure configuration sealing unavailable") != 2 {
		t.Fatalf("expected both fallback branches to fail closed: %q", got)
	}
}

func TestHardenConfigureHTMLFailsClosedOnUnexpectedTemplate(t *testing.T) {
	t.Setenv("ALLOW_LEGACY_PLAINTEXT_CONFIG", "false")
	if _, err := hardenConfigureHTML("<html>changed template</html>"); err == nil {
		t.Fatal("unexpected template must fail closed rather than bypass the guard")
	}
}
