package server

import "testing"

func TestTMDBIntegrationStatus(t *testing.T) {
	t.Setenv("TMDB_ACCESS_TOKEN", "")
	t.Setenv("TMDB_API_KEY", "")
	if got := tmdbIntegrationStatus(); got != "Disabled" {
		t.Fatalf("status = %q, want Disabled", got)
	}

	t.Setenv("TMDB_API_KEY", "legacy")
	if got := tmdbIntegrationStatus(); got != "Enabled (legacy API key)" {
		t.Fatalf("legacy status = %q", got)
	}

	t.Setenv("TMDB_ACCESS_TOKEN", "token")
	if got := tmdbIntegrationStatus(); got != "Enabled (Bearer access token)" {
		t.Fatalf("bearer status = %q", got)
	}
}
