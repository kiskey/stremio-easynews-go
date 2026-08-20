package addon

import "testing"

func rankedTestStream(key, category string, qualityScore int, confidence float64, sizeBytes int64) Stream {
	return Stream{
		URL: key,
		SortMeta: &SortMeta{
			StableKey:           key,
			QualityCategory:     category,
			QualityScore:        qualityScore,
			CandidateConfidence: confidence,
			VideoSizeBytes:      sizeBytes,
		},
	}
}

func TestSelectAndRankStreamsCustomQualityFilterIsExact(t *testing.T) {
	streams := []Stream{
		rankedTestStream("720", "720p", 2, 0.95, 2*bytesPerGiB),
	}

	got := selectAndRankStreams(streams, streamSelectionOptions{
		SortingPreference: "quality_first",
		QualityFilters:    []string{"4k"},
	})

	if len(got) != 0 {
		t.Fatalf("expected no 4K matches, got %d streams", len(got))
	}
}

func TestSelectAndRankStreamsUsesStructuredSizeLimit(t *testing.T) {
	streams := []Stream{
		rankedTestStream("small", "1080p", 3, 0.90, 4*bytesPerGiB),
		rankedTestStream("large", "1080p", 3, 0.95, 8*bytesPerGiB),
		rankedTestStream("unknown", "1080p", 3, 0.80, 0),
	}

	got := selectAndRankStreams(streams, streamSelectionOptions{
		SortingPreference: "quality_first",
		QualityFilters:    []string{"4k", "1080p", "720p", "480p"},
		MaxFileSizeGB:     5,
	})

	if len(got) != 2 {
		t.Fatalf("expected small + unknown-size streams, got %d", len(got))
	}
	for _, stream := range got {
		if stream.SortMeta.StableKey == "large" {
			t.Fatal("8 GiB stream should be excluded by 5 GiB structured limit")
		}
	}
}

func TestMaxPerQualityPreservesRequestedGlobalOrder(t *testing.T) {
	streams := []Stream{
		rankedTestStream("4k-10", "4k", 4, 0.95, 10*bytesPerGiB),
		rankedTestStream("1080-20", "1080p", 3, 0.95, 20*bytesPerGiB),
		rankedTestStream("1080-5", "1080p", 3, 0.95, 5*bytesPerGiB),
	}

	got := selectAndRankStreams(streams, streamSelectionOptions{
		SortingPreference:    "size_first",
		QualityFilters:       []string{"4k", "1080p", "720p", "480p"},
		MaxResultsPerQuality: 1,
	})

	if len(got) != 2 {
		t.Fatalf("expected two streams after per-quality cap, got %d", len(got))
	}
	if got[0].SortMeta.StableKey != "1080-20" || got[1].SortMeta.StableKey != "4k-10" {
		t.Fatalf("per-quality cap must preserve size_first ordering; got %s then %s",
			got[0].SortMeta.StableKey, got[1].SortMeta.StableKey)
	}
}

func TestQualityFirstUsesCandidateConfidenceWithinSameQuality(t *testing.T) {
	highSourceLowConfidence := rankedTestStream("remux-low-confidence", "1080p", 3, 0.81, 10*bytesPerGiB)
	highSourceLowConfidence.SortMeta.SourceScore = 8

	lowerSourceHighConfidence := rankedTestStream("web-high-confidence", "1080p", 3, 0.97, 5*bytesPerGiB)
	lowerSourceHighConfidence.SortMeta.SourceScore = 6

	got := selectAndRankStreams([]Stream{highSourceLowConfidence, lowerSourceHighConfidence}, streamSelectionOptions{
		SortingPreference: "quality_first",
		QualityFilters:    []string{"4k", "1080p", "720p", "480p"},
	})

	if got[0].SortMeta.StableKey != "web-high-confidence" {
		t.Fatalf("candidate confidence should rank trusted same-quality match first, got %s", got[0].SortMeta.StableKey)
	}
}

func TestLanguageFirstIsPrimarySignal(t *testing.T) {
	preferred := rankedTestStream("preferred", "720p", 2, 0.90, 2*bytesPerGiB)
	preferred.SortMeta.HasPreferredLang = true

	nonPreferred := rankedTestStream("nonpreferred", "4k", 4, 0.99, 20*bytesPerGiB)

	got := selectAndRankStreams([]Stream{nonPreferred, preferred}, streamSelectionOptions{
		SortingPreference: "language_first",
		QualityFilters:    []string{"4k", "1080p", "720p", "480p"},
	})

	if got[0].SortMeta.StableKey != "preferred" {
		t.Fatalf("language_first should keep preferred language first, got %s", got[0].SortMeta.StableKey)
	}
}

func TestQualityCategoryFromLabel(t *testing.T) {
	cases := map[string]string{
		"4K/UHD":  "4k",
		"2160p":   "4k",
		"1080p":   "1080p",
		"720p":    "720p",
		"SD":      "480p",
		"unknown": "other",
	}
	for input, want := range cases {
		if got := qualityCategoryFromLabel(input); got != want {
			t.Fatalf("qualityCategoryFromLabel(%q) = %q, want %q", input, got, want)
		}
	}
}
