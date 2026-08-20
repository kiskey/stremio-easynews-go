package addon

import (
	"testing"
	"time"

	"github.com/kiskey/stremio-easynews-go/internal/api"
)

func TestStructuredSizePrefersRawBytes(t *testing.T) {
	bytes, unit, value := structuredSize(3*bytesPerGiB, "999 MB")
	if bytes != 3*bytesPerGiB {
		t.Fatalf("raw byte size not preserved: got %d", bytes)
	}
	if unit != "GB" || value != 3 {
		t.Fatalf("unexpected structured size: unit=%s value=%v", unit, value)
	}
}

func TestStructuredSizeFallsBackToSourceSize(t *testing.T) {
	bytes, unit, value := structuredSize(0, "1.5 GB")
	want := int64(float64(bytesPerGiB) * 1.5)
	if bytes != want || unit != "GB" || value != 1.5 {
		t.Fatalf("unexpected fallback structured size: bytes=%d unit=%s value=%v", bytes, unit, value)
	}
}

func TestStructuredDateFallsBackToTimestamp(t *testing.T) {
	file := api.FileData{Ts: 1_700_000_000}
	if got := structuredDateMs(file); got != file.Ts*1000 {
		t.Fatalf("timestamp fallback = %d, want %d", got, file.Ts*1000)
	}

	file.Five = "2024-01-02T03:04:05Z"
	want, _ := time.Parse(time.RFC3339, file.Five)
	if got := structuredDateMs(file); got != want.UnixMilli() {
		t.Fatalf("RFC3339 date = %d, want %d", got, want.UnixMilli())
	}
}

func TestNormalizedAudioLanguagesDeduplicatesCaseInsensitively(t *testing.T) {
	got := normalizedAudioLanguages([]string{"en", "EN", " ja ", "", "fr"})
	if len(got) != 3 {
		t.Fatalf("expected 3 normalized languages, got %#v", got)
	}
	if !hasPreferredAudioLanguage(got, "JA") {
		t.Fatalf("preferred language lookup should be case-insensitive: %#v", got)
	}
}

func TestStructuredReleaseTraitsUseRawTitleClassifiers(t *testing.T) {
	sourceScore, sourceLabel, hdrScore, hdrLabel, codecScore, codecLabel :=
		structuredReleaseTraits("Movie.2160p.REMUX.DV.HEVC")

	if sourceScore != 8 || sourceLabel != "Remux" {
		t.Fatalf("unexpected source traits: %d %q", sourceScore, sourceLabel)
	}
	if hdrScore != 4 || hdrLabel != "DV" {
		t.Fatalf("unexpected HDR traits: %d %q", hdrScore, hdrLabel)
	}
	if codecScore != 2 || codecLabel != "H265 HEVC" {
		t.Fatalf("unexpected codec traits: %d %q", codecScore, codecLabel)
	}
}
