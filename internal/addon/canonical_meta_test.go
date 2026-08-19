package addon

import "testing"

func TestRankTitleVariantsPrefersHigherConfidenceAndDeduplicates(t *testing.T) {
	variants := []TitleVariant{
		{Title: "Spirited Away", Source: "generated", Confidence: 0.68},
		{Title: "Spirited Away", Source: "tmdb.alternative", Confidence: 0.90},
		{Title: "Sen to Chihiro no Kamikakushi", Source: "tmdb.original", Confidence: 0.96},
		{Title: "Le Voyage de Chihiro", Source: "tmdb.translation", Confidence: 0.97},
	}

	ranked := rankTitleVariants(variants)
	if len(ranked) != 3 {
		t.Fatalf("expected 3 deduplicated variants, got %d: %#v", len(ranked), ranked)
	}
	if ranked[0].Source != "tmdb.translation" {
		t.Fatalf("expected preferred translation first, got %#v", ranked[0])
	}

	foundPrimaryAlt := false
	for _, variant := range ranked {
		if variant.Title == "Spirited Away" {
			foundPrimaryAlt = true
			if variant.Source != "tmdb.alternative" || variant.Confidence != 0.90 {
				t.Fatalf("duplicate title did not retain strongest provenance: %#v", variant)
			}
		}
	}
	if !foundPrimaryAlt {
		t.Fatal("expected Spirited Away variant")
	}
}

func TestCanonicalProjectionKeepsCompatibilityAndProvenance(t *testing.T) {
	canonical := CanonicalMetadata{
		Primary: TitleVariant{Title: "Primary", Source: "tmdb.primary", Confidence: 1},
		Variants: []TitleVariant{
			{Title: "Primary", Source: "generated", Confidence: 0.5},
			{Title: "Localized", Source: "tmdb.translation", Confidence: 0.97},
			{Title: "Original", Source: "tmdb.original", Confidence: 0.96},
		},
		OriginalTitle:      "Original",
		Year:               2024,
		Source:             "tmdb",
		Confidence:         0.99,
		OriginalLanguage:   "ja",
		OriginCountries:    []string{"JP"},
		SeasonEpisodeCount: 12,
	}

	projected := canonical.toMetaProviderResponse()
	if projected.Name != "Primary" {
		t.Fatalf("Name = %q", projected.Name)
	}
	if projected.OriginalName != "Original" {
		t.Fatalf("OriginalName = %q", projected.OriginalName)
	}
	if len(projected.AlternativeNames) != 2 || projected.AlternativeNames[0] != "Localized" || projected.AlternativeNames[1] != "Original" {
		t.Fatalf("unexpected ranked alternatives: %#v", projected.AlternativeNames)
	}
	if projected.MetadataSource != "tmdb" || projected.MetadataConfidence != 0.99 {
		t.Fatalf("metadata provenance lost: source=%q confidence=%v", projected.MetadataSource, projected.MetadataConfidence)
	}
	if len(projected.TitleVariants) != 3 || projected.TitleVariants[0].Source != "tmdb.primary" {
		t.Fatalf("typed title variants not preserved: %#v", projected.TitleVariants)
	}
}

func TestCanonicalFromLegacyPreservesLegacyMetadata(t *testing.T) {
	legacy := MetaProviderResponse{
		Name:               "Fallback Title",
		OriginalName:       "Fallback Title",
		AlternativeNames:   []string{"Fallback Alt"},
		Year:               1999,
		Season:             "2",
		Episode:            "3",
		OriginalLanguage:   "en",
		EpisodeAirDate:     "1999.02.03",
		IsAnimation:        true,
		OriginCountries:    []string{"US"},
		SeasonEpisodeCount: 10,
	}

	canonical := canonicalFromLegacy(legacy, "imdb.suggestion", 0.90)
	projected := canonical.toMetaProviderResponse()
	if projected.Name != legacy.Name || projected.Year != legacy.Year || projected.Season != legacy.Season || projected.Episode != legacy.Episode {
		t.Fatalf("legacy compatibility projection changed: %#v", projected)
	}
	if projected.MetadataSource != "imdb.suggestion" {
		t.Fatalf("unexpected fallback source %q", projected.MetadataSource)
	}
}

func TestYearFromISODate(t *testing.T) {
	tests := map[string]int{
		"2026-08-19": 2026,
		"1999-01-01": 1999,
		"":           0,
		"bad-date":   0,
		"0999-01-01": 0,
	}
	for input, want := range tests {
		if got := yearFromISODate(input); got != want {
			t.Fatalf("yearFromISODate(%q) = %d, want %d", input, got, want)
		}
	}
}
