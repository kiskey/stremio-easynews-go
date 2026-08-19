package server

import "testing"

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
