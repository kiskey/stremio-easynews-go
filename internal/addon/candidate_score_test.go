package addon

import (
	"strings"
	"testing"

	"github.com/kiskey/stremio-easynews-go/internal/api"
)

func testVideo(title string, size int64) api.FileData {
	return api.FileData{
		Zero:     title,
		Two:      ".mkv",
		Ten:      title,
		Eleven:   ".mkv",
		Fourteen: "45m",
		Type:     "VIDEO",
		RawSize:  size,
	}
}

func testMetaMovie(name string, year int) MetaProviderResponse {
	return MetaProviderResponse{
		Name:               name,
		OriginalName:       name,
		Year:               year,
		MetadataSource:     "tmdb",
		MetadataConfidence: 0.97,
		TitleVariants: []TitleVariant{
			{Title: name, Source: "tmdb.primary", Kind: "primary", Confidence: 0.99},
		},
	}
}

func TestCandidateContextUsesCanonicalTitleProvenance(t *testing.T) {
	meta := testMetaMovie("Money Heist", 2017)
	meta.AlternativeNames = []string{"La Casa de Papel"}
	meta.TitleVariants = append(meta.TitleVariants, TitleVariant{
		Title:      "La Casa de Papel",
		Source:     "tmdb.original",
		Kind:       "original",
		Language:   "es",
		Confidence: 0.96,
	})

	ctx := newCandidateMatchContext("movie", meta, []string{"Money Heist", "La Casa de Papel"}, true, 300*1024*1024)
	if len(ctx.Titles) != 2 {
		t.Fatalf("expected 2 candidate titles, got %d", len(ctx.Titles))
	}

	found := false
	for _, title := range ctx.Titles {
		if title.Title == "La Casa de Papel" {
			found = true
			if title.Source != "tmdb.original" {
				t.Fatalf("unexpected source %q", title.Source)
			}
			if title.Confidence < 0.95 {
				t.Fatalf("unexpected confidence %.3f", title.Confidence)
			}
		}
	}
	if !found {
		t.Fatal("canonical alternative title was not retained")
	}
}

func TestExactMovieCandidatePrevalidates(t *testing.T) {
	meta := testMetaMovie("Dune", 2021)
	ctx := newCandidateMatchContext("movie", meta, []string{"Dune"}, true, 300*1024*1024)
	file := testVideo("Dune.2021.1080p.WEB-DL.mkv", 2*1024*1024*1024)

	evaluation := evaluateCandidate(file, ctx)
	if !evaluation.Accepted {
		t.Fatalf("expected candidate to be accepted, reason=%s", evaluation.Reason)
	}
	if !evaluation.Prevalidated {
		t.Fatalf("expected candidate to be prevalidated, confidence=%.3f", evaluation.Confidence)
	}
	if evaluation.Confidence < candidatePrevalidationThreshold {
		t.Fatalf("confidence %.3f below threshold", evaluation.Confidence)
	}
}

func TestUnrelatedLargeMovieDoesNotTriggerPrevalidation(t *testing.T) {
	meta := testMetaMovie("Dune", 2021)
	ctx := newCandidateMatchContext("movie", meta, []string{"Dune"}, true, 300*1024*1024)
	file := testVideo("Foundation.2021.2160p.WEB-DL.mkv", 8*1024*1024*1024)

	evaluation := evaluateCandidate(file, ctx)
	if evaluation.Accepted {
		t.Fatal("unrelated title must not be accepted")
	}
	if evaluation.Prevalidated {
		t.Fatal("unrelated large file must never count toward early exit")
	}
	if !strings.Contains(evaluation.Reason, "title") {
		t.Fatalf("expected title rejection, got %q", evaluation.Reason)
	}
}

func TestMovieYearMismatchRejected(t *testing.T) {
	meta := testMetaMovie("Dune", 2021)
	ctx := newCandidateMatchContext("movie", meta, []string{"Dune"}, true, 300*1024*1024)
	file := testVideo("Dune.1984.1080p.BluRay.mkv", 2*1024*1024*1024)

	evaluation := evaluateCandidate(file, ctx)
	if evaluation.Accepted {
		t.Fatal("wrong-year reboot candidate must be rejected")
	}
	if evaluation.Reason != "year_mismatch" {
		t.Fatalf("unexpected rejection reason %q", evaluation.Reason)
	}
}

func TestSeriesEpisodeCandidatePrevalidates(t *testing.T) {
	meta := MetaProviderResponse{
		Name:                "Severance",
		OriginalName:        "Severance",
		Year:                2022,
		Season:              "1",
		Episode:             "3",
		MetadataSource:      "tmdb",
		MetadataConfidence:  0.98,
		SeasonEpisodeCounts: map[int]int{1: 9},
		TitleVariants: []TitleVariant{
			{Title: "Severance", Source: "tmdb.primary", Kind: "primary", Confidence: 0.99},
		},
	}
	ctx := newCandidateMatchContext("series", meta, []string{"Severance"}, true, 80*1024*1024)
	file := testVideo("Severance.S01E03.1080p.WEB-DL.mkv", 900*1024*1024)

	evaluation := evaluateCandidate(file, ctx)
	if !evaluation.Accepted || !evaluation.Prevalidated {
		t.Fatalf("expected exact episode to prevalidate: accepted=%v prevalidated=%v reason=%s confidence=%.3f", evaluation.Accepted, evaluation.Prevalidated, evaluation.Reason, evaluation.Confidence)
	}
	if evaluation.EpisodeScore < 0.95 {
		t.Fatalf("expected strong episode score, got %.3f", evaluation.EpisodeScore)
	}
}

func TestWrongSeriesEpisodeDoesNotPrevalidate(t *testing.T) {
	meta := MetaProviderResponse{
		Name:               "Severance",
		Year:               2022,
		Season:             "1",
		Episode:            "3",
		MetadataSource:     "tmdb",
		MetadataConfidence: 0.98,
		TitleVariants: []TitleVariant{
			{Title: "Severance", Source: "tmdb.primary", Kind: "primary", Confidence: 0.99},
		},
	}
	ctx := newCandidateMatchContext("series", meta, []string{"Severance"}, true, 80*1024*1024)
	file := testVideo("Severance.S01E07.1080p.WEB-DL.mkv", 900*1024*1024)

	evaluation := evaluateCandidate(file, ctx)
	if evaluation.Accepted || evaluation.Prevalidated {
		t.Fatalf("wrong episode must not be accepted/prevalidated: %+v", evaluation)
	}
	if evaluation.Reason != "episode_mismatch" {
		t.Fatalf("unexpected rejection reason %q", evaluation.Reason)
	}
}

func TestSmallLegacyFileCanRemainAcceptedWithoutStoppingSearch(t *testing.T) {
	meta := testMetaMovie("Dune", 2021)
	ctx := newCandidateMatchContext("movie", meta, []string{"Dune"}, true, 300*1024*1024)
	file := testVideo("Dune.2021.480p.WEBRip.mkv", 120*1024*1024)

	evaluation := evaluateCandidate(file, ctx)
	if !evaluation.Accepted {
		t.Fatalf("legacy-sized valid file should preserve final eligibility, reason=%s", evaluation.Reason)
	}
	if evaluation.Prevalidated {
		t.Fatal("sub-300MB movie must not count toward early-exit confidence")
	}
}

func TestCandidateIdentityFallsBackWhenHashMissing(t *testing.T) {
	file := testVideo("Example.Movie.2024.1080p.mkv", 1024)
	file.Zero = ""
	identity := candidateIdentity(file)
	if !strings.HasPrefix(identity, "fallback:") {
		t.Fatalf("expected fallback identity, got %q", identity)
	}
}
