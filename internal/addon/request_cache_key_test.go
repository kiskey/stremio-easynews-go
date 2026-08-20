package addon

import (
	"strings"
	"testing"
)

func TestBuildRequestCacheKeyScopesCredentialAndContentType(t *testing.T) {
	t.Setenv("ADDON_BASE_URL", "https://addon.example")

	cfg := AddonConfig{
		Username:             "alice",
		Password:             "top-secret",
		StrictTitleMatching:  "true",
		EnableAltTitles:      "true",
		PreferredLanguage:    "en",
		SortingPreference:    "quality_first",
		ShowQualities:        "4k,1080p",
		MaxResultsPerQuality: "5",
		MaxFileSize:          "50",
	}

	movieA := buildRequestCacheKey("movie", "tt1234567", "cred-a", cfg)
	movieB := buildRequestCacheKey("movie", "tt1234567", "cred-b", cfg)
	seriesA := buildRequestCacheKey("series", "tt1234567", "cred-a", cfg)

	if movieA == movieB {
		t.Fatal("credential change must invalidate the request cache key")
	}
	if movieA == seriesA {
		t.Fatal("content type must be part of the request cache key")
	}
	if strings.Contains(movieA, cfg.Username) || strings.Contains(movieA, cfg.Password) {
		t.Fatal("request cache key must not contain plaintext Easynews credentials")
	}
}

func TestBuildRequestCacheKeyScopesEffectiveBaseURL(t *testing.T) {
	cfg := AddonConfig{}

	t.Setenv("ADDON_BASE_URL", "https://one.example")
	one := buildRequestCacheKey("movie", "tt1234567", "cred", cfg)

	t.Setenv("ADDON_BASE_URL", "https://two.example")
	two := buildRequestCacheKey("movie", "tt1234567", "cred", cfg)

	if one == two {
		t.Fatal("effective ADDON_BASE_URL change must invalidate cached stream URLs")
	}

	cfg.BaseUrl = "https://configured.example/"
	t.Setenv("ADDON_BASE_URL", "https://ignored.example")
	configuredA := buildRequestCacheKey("movie", "tt1234567", "cred", cfg)

	t.Setenv("ADDON_BASE_URL", "https://also-ignored.example")
	configuredB := buildRequestCacheKey("movie", "tt1234567", "cred", cfg)

	if configuredA != configuredB {
		t.Fatal("explicit config BaseUrl must take precedence over ADDON_BASE_URL")
	}
}

func TestRequestCacheVersionV29(t *testing.T) {
	if requestCacheVersion != "v29" {
		t.Fatalf("requestCacheVersion = %q, want v29", requestCacheVersion)
	}
	key := buildRequestCacheKey("movie", "tt1234567", "cred", AddonConfig{})
	if !strings.HasPrefix(key, "v29:") {
		t.Fatalf("cache key = %q, want v29 prefix", key)
	}
}
