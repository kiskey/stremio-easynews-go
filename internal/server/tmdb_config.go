package server

import (
	"os"
	"strings"
)

func tmdbIntegrationStatus() string {
	if strings.TrimSpace(os.Getenv("TMDB_ACCESS_TOKEN")) != "" {
		return "Enabled (Bearer access token)"
	}
	if strings.TrimSpace(os.Getenv("TMDB_API_KEY")) != "" {
		return "Enabled (legacy API key)"
	}
	return "Disabled"
}
