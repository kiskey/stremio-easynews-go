package server

import (
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
