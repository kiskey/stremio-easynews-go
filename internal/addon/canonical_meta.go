package addon

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const canonicalMetadataVersion = "v3"

// CanonicalMetadata is the provider-neutral metadata model used internally
// before projecting data into the legacy MetaProviderResponse consumed by the
// search/query pipeline. It preserves title provenance and confidence without
// changing the Stremio-facing contract.
type CanonicalMetadata struct {
	Primary             TitleVariant
	Variants            []TitleVariant
	OriginalTitle       string
	Year                int
	Season              string
	Episode             string
	OriginalLanguage    string
	EpisodeAirDate      string
	IsAnimation         bool
	OriginCountries     []string
	SeasonEpisodeCount  int
	SeasonEpisodeCounts map[int]int
	Source              string
	Confidence          float64
}

func splitMetadataID(id string) (tt, season, episode string) {
	parts := strings.Split(id, ":")
	if len(parts) > 0 {
		tt = parts[0]
	}
	if len(parts) > 1 {
		season = parts[1]
	}
	if len(parts) > 2 {
		episode = parts[2]
	}
	return tt, season, episode
}

func yearFromISODate(value string) int {
	value = strings.TrimSpace(value)
	if len(value) < 4 {
		return 0
	}
	year, err := strconv.Atoi(value[:4])
	if err != nil || year < 1800 || year > 3000 {
		return 0
	}
	return year
}

func variantKey(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	key := strings.TrimSpace(strings.ToLower(SanitizeTitle(title)))
	if key == "" {
		key = strings.ToLower(title)
	}
	return key
}

func clampConfidence(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func sourceRank(source string) int {
	switch source {
	case "tmdb.primary":
		return 100
	case "tmdb.translation":
		return 95
	case "tmdb.original":
		return 90
	case "tmdb.alternative":
		return 85
	case "imdb.suggestion":
		return 80
	case "cinemeta":
		return 70
	case "transliteration":
		return 60
	case "generated":
		return 50
	default:
		return 0
	}
}

// rankTitleVariants deduplicates semantically equivalent titles and orders the
// survivors by confidence, then by source authority. When two providers yield
// the same title, the stronger provenance wins instead of duplicating Solr
// queries.
func rankTitleVariants(variants []TitleVariant) []TitleVariant {
	best := make(map[string]TitleVariant, len(variants))
	for _, variant := range variants {
		variant.Title = strings.TrimSpace(variant.Title)
		if variant.Title == "" {
			continue
		}
		variant.Confidence = clampConfidence(variant.Confidence)
		key := variantKey(variant.Title)
		if key == "" {
			continue
		}

		current, exists := best[key]
		if !exists || variant.Confidence > current.Confidence ||
			(variant.Confidence == current.Confidence && sourceRank(variant.Source) > sourceRank(current.Source)) {
			best[key] = variant
		}
	}

	out := make([]TitleVariant, 0, len(best))
	for _, variant := range best {
		out = append(out, variant)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Confidence != out[j].Confidence {
			return out[i].Confidence > out[j].Confidence
		}
		if sourceRank(out[i].Source) != sourceRank(out[j].Source) {
			return sourceRank(out[i].Source) > sourceRank(out[j].Source)
		}
		return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title)
	})
	return out
}

func appendGeneratedVariants(variants []TitleVariant, title string) []TitleVariant {
	for _, alt := range GetAlternativeTitles(title) {
		variants = append(variants, TitleVariant{
			Title:      alt,
			Source:     "generated",
			Kind:       "normalized-alternative",
			Confidence: 0.68,
		})
	}
	return variants
}

func appendTransliterations(variants []TitleVariant) []TitleVariant {
	base := append([]TitleVariant(nil), variants...)
	for _, variant := range base {
		transliterated := strings.TrimSpace(Transliterate(variant.Title))
		if transliterated == "" || strings.EqualFold(transliterated, variant.Title) {
			continue
		}
		confidence := variant.Confidence - 0.08
		if confidence < 0.55 {
			confidence = 0.55
		}
		variants = append(variants, TitleVariant{
			Title:      transliterated,
			Source:     "transliteration",
			Kind:       "transliteration",
			Language:   variant.Language,
			Country:    variant.Country,
			Confidence: confidence,
		})
	}
	return variants
}

func canonicalFromLegacy(meta MetaProviderResponse, source string, confidence float64) CanonicalMetadata {
	variants := []TitleVariant{{
		Title:      meta.Name,
		Source:     source,
		Kind:       "primary",
		Confidence: confidence,
	}}
	for _, alt := range meta.AlternativeNames {
		variants = append(variants, TitleVariant{
			Title:      alt,
			Source:     source,
			Kind:       "alternative",
			Confidence: confidence - 0.12,
		})
	}
	variants = rankTitleVariants(variants)

	primary := TitleVariant{Title: meta.Name, Source: source, Kind: "primary", Confidence: confidence}
	return CanonicalMetadata{
		Primary:             primary,
		Variants:            variants,
		OriginalTitle:       meta.OriginalName,
		Year:                meta.Year,
		Season:              meta.Season,
		Episode:             meta.Episode,
		OriginalLanguage:    meta.OriginalLanguage,
		EpisodeAirDate:      meta.EpisodeAirDate,
		IsAnimation:         meta.IsAnimation,
		OriginCountries:     meta.OriginCountries,
		SeasonEpisodeCount:  meta.SeasonEpisodeCount,
		SeasonEpisodeCounts: meta.SeasonEpisodeCounts,
		Source:              source,
		Confidence:          confidence,
	}
}

func (meta CanonicalMetadata) toMetaProviderResponse() MetaProviderResponse {
	primaryKey := variantKey(meta.Primary.Title)
	alternatives := make([]string, 0, len(meta.Variants))
	orderedVariants := make([]TitleVariant, 0, len(meta.Variants)+1)
	orderedVariants = append(orderedVariants, meta.Primary)

	for _, variant := range rankTitleVariants(meta.Variants) {
		if variantKey(variant.Title) == primaryKey {
			continue
		}
		alternatives = append(alternatives, variant.Title)
		orderedVariants = append(orderedVariants, variant)
	}

	originalTitle := strings.TrimSpace(meta.OriginalTitle)
	if originalTitle == "" {
		originalTitle = meta.Primary.Title
	}

	return MetaProviderResponse{
		Name:                meta.Primary.Title,
		OriginalName:        originalTitle,
		AlternativeNames:    alternatives,
		TitleVariants:       orderedVariants,
		MetadataSource:      meta.Source,
		MetadataConfidence:  meta.Confidence,
		Year:                meta.Year,
		Season:              meta.Season,
		Episode:             meta.Episode,
		OriginalLanguage:    meta.OriginalLanguage,
		EpisodeAirDate:      meta.EpisodeAirDate,
		IsAnimation:         meta.IsAnimation,
		OriginCountries:     meta.OriginCountries,
		SeasonEpisodeCount:  meta.SeasonEpisodeCount,
		SeasonEpisodeCounts: meta.SeasonEpisodeCounts,
	}
}

func tmdbCanonicalMetaProvider(id, contentType, preferredLanguage string, enableAltTitles bool, altTitleCountry string) (CanonicalMetadata, error) {
	if !useTMDB.Load() {
		return CanonicalMetadata{}, fmt.Errorf("TMDB integration unavailable")
	}

	tt, season, episode := splitMetadataID(id)
	tmdbID, isMovie, originalLanguage, err := resolveTMDBID(tt)
	if err != nil {
		return CanonicalMetadata{}, err
	}
	if tmdbID == 0 {
		return CanonicalMetadata{}, fmt.Errorf("no TMDB mapping for %s", tt)
	}
	if contentType == "movie" && !isMovie {
		return CanonicalMetadata{}, fmt.Errorf("TMDB mapping type mismatch for movie %s", tt)
	}
	if contentType == "series" && isMovie {
		return CanonicalMetadata{}, fmt.Errorf("TMDB mapping type mismatch for series %s", tt)
	}

	details := getTMDBDetails(tt)
	primaryTitle := strings.TrimSpace(details.DisplayTitle)
	if primaryTitle == "" {
		primaryTitle = strings.TrimSpace(details.OriginalTitle)
	}
	if primaryTitle == "" {
		return CanonicalMetadata{}, fmt.Errorf("TMDB details missing title for %s", tt)
	}

	variants := []TitleVariant{{
		Title:      primaryTitle,
		Source:     "tmdb.primary",
		Kind:       "primary",
		Language:   originalLanguage,
		Confidence: 1.0,
	}}

	if original := strings.TrimSpace(details.OriginalTitle); original != "" {
		variants = append(variants, TitleVariant{
			Title:      original,
			Source:     "tmdb.original",
			Kind:       "original-title",
			Language:   originalLanguage,
			Confidence: 0.96,
		})
	}

	if enableAltTitles {
		if altItems, altErr := getTMDBAlternativeTitleItems(tt, true, altTitleCountry); altErr == nil {
			for _, alt := range altItems {
				kind := strings.TrimSpace(alt.Type)
				if kind == "" {
					kind = "alternative-title"
				}
				variants = append(variants, TitleVariant{
					Title:      alt.Title,
					Source:     "tmdb.alternative",
					Kind:       kind,
					Country:    alt.Country,
					Confidence: 0.90,
				})
			}
		}
	}

	variants = appendGeneratedVariants(variants, primaryTitle)

	if preferredLanguage != "" {
		if translated, transErr := getTMDBTranslatedTitle(tt, preferredLanguage); transErr == nil && strings.TrimSpace(translated) != "" {
			variants = append(variants, TitleVariant{
				Title:      translated,
				Source:     "tmdb.translation",
				Kind:       "preferred-language",
				Language:   preferredLanguage,
				Confidence: 0.97,
			})
		}
	}

	variants = appendTransliterations(variants)
	variants = rankTitleVariants(variants)

	seasonEpisodeCount := 0
	episodeAirDate := ""
	seasonNum, _ := strconv.Atoi(season)
	episodeNum, _ := strconv.Atoi(episode)
	if !isMovie && seasonNum > 0 {
		seasonDetails := getTMDBSeasonDetails(tt, seasonNum)
		seasonEpisodeCount = seasonDetails.EpisodeCount
		if episodeNum > 0 {
			episodeAirDate = seasonDetails.AirDates[episodeNum]
		}
	}

	return CanonicalMetadata{
		Primary: TitleVariant{
			Title:      primaryTitle,
			Source:     "tmdb.primary",
			Kind:       "primary",
			Language:   originalLanguage,
			Confidence: 1.0,
		},
		Variants:            variants,
		OriginalTitle:       details.OriginalTitle,
		Year:                details.Year,
		Season:              season,
		Episode:             episode,
		OriginalLanguage:    originalLanguage,
		EpisodeAirDate:      episodeAirDate,
		IsAnimation:         details.IsAnimation,
		OriginCountries:     append([]string(nil), details.OriginCountry...),
		SeasonEpisodeCount:  seasonEpisodeCount,
		SeasonEpisodeCounts: details.SeasonEpisodeCounts,
		Source:              "tmdb",
		Confidence:          0.99,
	}, nil
}

func resolveCanonicalMetadata(id, contentType, preferredLanguage string, enableAltTitles bool, altTitleCountry string) (CanonicalMetadata, error) {
	if useTMDB.Load() {
		if meta, err := tmdbCanonicalMetaProvider(id, contentType, preferredLanguage, enableAltTitles, altTitleCountry); err == nil && meta.Primary.Title != "" {
			return meta, nil
		} else if err != nil {
			metaLogger.Debug("TMDB canonical metadata lookup failed, falling back: %v", err)
		}
	}

	if meta, err := imdbMetaProvider(id, preferredLanguage, enableAltTitles, altTitleCountry); err == nil && meta.Name != "" {
		return canonicalFromLegacy(meta, "imdb.suggestion", 0.90), nil
	} else if err != nil {
		metaLogger.Debug("IMDb metadata lookup failed, falling back to Cinemeta: %v", err)
	}

	meta, err := cinemetaMetaProvider(id, contentType, preferredLanguage, enableAltTitles, altTitleCountry)
	if err != nil || meta.Name == "" {
		if err == nil {
			err = fmt.Errorf("empty Cinemeta metadata")
		}
		return CanonicalMetadata{}, err
	}
	return canonicalFromLegacy(meta, "cinemeta", 0.82), nil
}
