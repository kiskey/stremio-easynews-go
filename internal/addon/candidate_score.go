package addon

import (
	"fmt"
	"math"
	"strings"

	"github.com/kiskey/stremio-easynews-go/internal/api"
)

const candidatePrevalidationThreshold = 0.78

type candidateTitleVariant struct {
	Title      string
	Source     string
	Confidence float64
}

type candidateMatchContext struct {
	ContentType        string
	Meta               MetaProviderResponse
	Titles             []candidateTitleVariant
	Strict             bool
	TargetSeason       int
	TargetEpisode      int
	TargetAbsolute     int
	TargetPrior        float64
	MinEarlyExitSize   int64
	MetadataConfidence float64
}

// CandidateEvaluation is the single internal decision record used both by the
// search fan-out gate and the final stream mapper. Accepted mirrors the legacy
// hard guardrails. Prevalidated is intentionally stricter: it also requires a
// useful file size and enough aggregate metadata confidence to justify stopping
// fallback searches early.
type CandidateEvaluation struct {
	Accepted          bool
	Prevalidated      bool
	Confidence        float64
	TitleScore        float64
	VariantConfidence float64
	YearScore         float64
	EpisodeScore      float64
	MatchedTitle      string
	MatchedSource     string
	Reason            string
	Parsed            *ParseResult
}

func newCandidateMatchContext(contentType string, meta MetaProviderResponse, allTitles []string, strict bool, minEarlyExitSize int64) candidateMatchContext {
	targetSeason := 0
	targetEpisode := 0
	if meta.Season != "" {
		fmt.Sscanf(meta.Season, "%d", &targetSeason)
	}
	if meta.Episode != "" {
		fmt.Sscanf(meta.Episode, "%d", &targetEpisode)
	}

	targetAbsolute := 0
	if contentType == "series" && targetSeason > 1 && len(meta.SeasonEpisodeCounts) > 0 {
		totalPrevious := 0
		for season := 1; season < targetSeason; season++ {
			totalPrevious += meta.SeasonEpisodeCounts[season]
		}
		if totalPrevious > 0 {
			targetAbsolute = totalPrevious + targetEpisode
		}
	}

	metadataConfidence := clampConfidence(meta.MetadataConfidence)
	if metadataConfidence == 0 {
		metadataConfidence = 0.75
	}

	byKey := make(map[string]TitleVariant, len(meta.TitleVariants))
	for _, variant := range meta.TitleVariants {
		key := variantKey(variant.Title)
		if key == "" {
			continue
		}
		current, exists := byKey[key]
		if !exists || variant.Confidence > current.Confidence {
			byKey[key] = variant
		}
	}

	titles := make([]candidateTitleVariant, 0, len(allTitles))
	seen := make(map[string]struct{}, len(allTitles))
	for _, title := range allTitles {
		title = strings.TrimSpace(title)
		key := variantKey(title)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}

		entry := candidateTitleVariant{
			Title:      title,
			Source:     "generated",
			Confidence: 0.70,
		}
		if variant, ok := byKey[key]; ok {
			entry.Source = variant.Source
			entry.Confidence = clampConfidence(variant.Confidence)
		} else if key == variantKey(meta.Name) {
			entry.Source = meta.MetadataSource
			if entry.Source == "" {
				entry.Source = "primary"
			}
			entry.Confidence = metadataConfidence
		}
		titles = append(titles, entry)
	}

	context := candidateMatchContext{
		ContentType:        contentType,
		Meta:               meta,
		Titles:             titles,
		Strict:             strict,
		TargetSeason:       targetSeason,
		TargetEpisode:      targetEpisode,
		TargetAbsolute:     targetAbsolute,
		MinEarlyExitSize:   minEarlyExitSize,
		MetadataConfidence: metadataConfidence,
	}
	if contentType == "series" {
		context.TargetPrior = ClassifyTargetPrior(meta)
	}
	return context
}

func candidateIdentity(file api.FileData) string {
	if hash := strings.TrimSpace(file.GetHash()); hash != "" {
		return "hash:" + hash
	}
	return fmt.Sprintf("fallback:%s:%d:%s", SanitizeTitle(GetPostTitle(file)), file.RawSize, strings.ToLower(GetFileExtension(file)))
}

func titleSimilarityFromParsed(targetTitle, parsedTitle string) float64 {
	if strings.TrimSpace(targetTitle) == "" || strings.TrimSpace(parsedTitle) == "" {
		return 0
	}

	cleanTarget := strings.Trim(strings.ToLower(targetTitle), " .-_[]()/\\")
	cleanParsed := strings.Trim(strings.ToLower(parsedTitle), " .-_[]()/\\")
	cleanTarget = ExpandAbbreviations(cleanTarget)
	cleanParsed = ExpandAbbreviations(cleanParsed)
	cleanTarget = normalizeNumbersInTitle(cleanTarget)
	cleanParsed = normalizeNumbersInTitle(cleanParsed)

	if hasNumericMismatch(cleanTarget, cleanParsed) {
		return 0
	}

	score := (OverlapCoefficient(cleanTarget, cleanParsed) * 0.7) +
		(tokenPositionOverlap(cleanTarget, cleanParsed) * 0.3)

	withoutArticleTarget := stripLeadingArticles(cleanTarget)
	withoutArticleParsed := stripLeadingArticles(cleanParsed)
	if withoutArticleTarget != cleanTarget || withoutArticleParsed != cleanParsed {
		cleanScore := (OverlapCoefficient(withoutArticleTarget, withoutArticleParsed) * 0.7) +
			(tokenPositionOverlap(withoutArticleTarget, withoutArticleParsed) * 0.3)
		if cleanScore > score {
			score = cleanScore
		}
	}

	return sequelGuardrail(targetTitle, parsedTitle, score)
}

func titleMatchesParsed(rawTitle string, parsed *ParseResult, query string, strict bool) (bool, float64) {
	if parsed == nil || parsed.Title == "" {
		if strict {
			return false, 0
		}
		matched := strings.Contains(strings.ToLower(rawTitle), strings.ToLower(query))
		if matched {
			return true, 0.80
		}
		return false, 0
	}

	if !passTitleGuardrail(query, parsed.Title) {
		return false, 0
	}

	similarity := titleSimilarityFromParsed(query, parsed.Title)
	if strict {
		return similarity >= 0.80, similarity
	}

	sanitizedTitle := SanitizeTitle(parsed.Title)
	sanitizedQuery := SanitizeTitle(query)
	matched := sanitizedQuery != "" && strings.Contains(sanitizedTitle, sanitizedQuery)
	if matched && similarity < 0.80 {
		similarity = 0.80
	}
	return matched, similarity
}

func candidateTitleIsolationPasses(parsed *ParseResult, titles []candidateTitleVariant) bool {
	if parsed == nil || parsed.Title == "" {
		return true
	}
	sanitizedParsed := SanitizeTitle(parsed.Title)
	for _, title := range titles {
		sanitizedMeta := SanitizeTitle(title.Title)
		if sanitizedMeta == "" {
			continue
		}
		if strings.Contains(sanitizedParsed, sanitizedMeta) || strings.Contains(sanitizedMeta, sanitizedParsed) {
			return true
		}
	}
	return false
}

func normalizedDate(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(value), "-", ".")
}

func evaluateCandidate(file api.FileData, context candidateMatchContext) CandidateEvaluation {
	evaluation := CandidateEvaluation{
		YearScore:    0.75,
		EpisodeScore: 1.0,
		Reason:       "title_mismatch",
	}

	if IsBadVideo(file) {
		evaluation.Reason = "bad_video"
		return evaluation
	}
	if isNewerShowDisqualified(file.Ts, context.Meta.Year) {
		evaluation.Reason = "temporal_mismatch"
		return evaluation
	}

	title := GetPostTitle(file)
	if context.ContentType == "series" && math.Abs(context.TargetPrior) >= 3.0 {
		candidatePrior := ComputeCandidateScore(title)
		if context.TargetPrior > 3.0 && candidatePrior < -3.0 {
			evaluation.Reason = "series_type_mismatch"
			return evaluation
		}
		if context.TargetPrior < -3.0 && candidatePrior > 4.0 {
			evaluation.Reason = "series_type_mismatch"
			return evaluation
		}
	}

	if context.ContentType == "series" && context.TargetPrior > 3.0 {
		duration := file.GetDuration()
		if strings.Contains(duration, "h") || strings.Contains(duration, "hour") {
			evaluation.Reason = "anime_duration_mismatch"
			return evaluation
		}
	}

	parsed := RobustParseInfo(title, 0)
	evaluation.Parsed = parsed
	if !candidateTitleIsolationPasses(parsed, context.Titles) {
		evaluation.Reason = "title_isolation_mismatch"
		return evaluation
	}

	bestWeighted := -1.0
	for _, variant := range context.Titles {
		matched, similarity := titleMatchesParsed(title, parsed, variant.Title, context.Strict)
		if !matched {
			continue
		}
		variantConfidence := clampConfidence(variant.Confidence)
		if variantConfidence == 0 {
			variantConfidence = 0.70
		}
		weighted := similarity*0.90 + variantConfidence*0.10
		if weighted > bestWeighted {
			bestWeighted = weighted
			evaluation.TitleScore = similarity
			evaluation.VariantConfidence = variantConfidence
			evaluation.MatchedTitle = variant.Title
			evaluation.MatchedSource = variant.Source
		}
	}
	if bestWeighted < 0 {
		evaluation.Reason = "title_mismatch"
		return evaluation
	}

	if parsed == nil {
		evaluation.Reason = "parse_failure"
		return evaluation
	}

	if context.ContentType == "series" {
		if context.TargetSeason == 1 && context.Meta.Year > 0 && parsed.Year > 0 {
			diff := parsed.Year - context.Meta.Year
			if diff < 0 {
				diff = -diff
			}
			if diff > 1 {
				evaluation.Reason = "year_mismatch"
				return evaluation
			}
			if diff == 0 {
				evaluation.YearScore = 1.0
			} else {
				evaluation.YearScore = 0.90
			}
		}

		if context.TargetEpisode > 0 && isExtraOrSpecial(title) {
			evaluation.Reason = "special_episode_mismatch"
			return evaluation
		}

		if context.TargetSeason > 0 && context.TargetEpisode > 0 {
			isPack, _, _, hasRange := ParsePackOrRange(title, context.TargetEpisode)

			episodeMatches := parsed.Episode == context.TargetEpisode ||
				(context.TargetAbsolute > 0 && parsed.Episode == context.TargetAbsolute)
			if !episodeMatches && len(parsed.Episodes) > 1 {
				for _, episode := range parsed.Episodes {
					if episode == context.TargetEpisode || (context.TargetAbsolute > 0 && episode == context.TargetAbsolute) {
						episodeMatches = true
						break
					}
				}
			}

			if (parsed.Season > 0 && parsed.Season != context.TargetSeason) ||
				(parsed.Episode > 0 && !episodeMatches && !hasRange && !isPack && !parsed.IsPack) {
				// Preserve the legacy absolute-episode ambiguity: an episode-only
				// parse is not hard rejected, but it receives zero episode confidence
				// and therefore cannot normally trigger early cancellation.
				if parsed.Season == 0 && parsed.Episode > 0 {
					evaluation.EpisodeScore = 0
				} else {
					evaluation.Reason = "episode_mismatch"
					return evaluation
				}
			}

			if parsed.Season == 0 && parsed.Episode == 0 && parsed.Date == "" && !isPack && !parsed.IsPack {
				evaluation.Reason = "episode_marker_missing"
				return evaluation
			}

			if evaluation.EpisodeScore != 0 {
				switch {
				case parsed.Season == context.TargetSeason && episodeMatches:
					evaluation.EpisodeScore = 1.0
				case episodeMatches:
					evaluation.EpisodeScore = 0.92
				case hasRange || isPack:
					evaluation.EpisodeScore = 0.88
				case parsed.IsPack:
					evaluation.EpisodeScore = 0.84
				case parsed.Date != "":
					evaluation.EpisodeScore = 0.72
				default:
					evaluation.EpisodeScore = 0.60
				}
			}

			if parsed.Date != "" && context.Meta.EpisodeAirDate != "" {
				if normalizedDate(parsed.Date) == normalizedDate(context.Meta.EpisodeAirDate) {
					if evaluation.EpisodeScore < 0.98 {
						evaluation.EpisodeScore = 0.98
					}
				} else if evaluation.EpisodeScore > 0.55 {
					evaluation.EpisodeScore = 0.55
				}
			}
		}
	}

	if context.ContentType == "movie" {
		if parsed.Season > 0 || parsed.Episode > 0 || parsed.IsPack {
			evaluation.Reason = "episodic_movie_mismatch"
			return evaluation
		}
		if context.Meta.Year > 0 && parsed.Year > 0 {
			diff := parsed.Year - context.Meta.Year
			if diff < 0 {
				diff = -diff
			}
			if diff > 1 {
				evaluation.Reason = "year_mismatch"
				return evaluation
			}
			if diff == 0 {
				evaluation.YearScore = 1.0
			} else {
				evaluation.YearScore = 0.90
			}
		}
	}

	if evaluation.VariantConfidence == 0 {
		evaluation.VariantConfidence = 0.70
	}
	if context.ContentType == "series" {
		evaluation.Confidence =
			evaluation.TitleScore*0.40 +
				evaluation.VariantConfidence*0.10 +
				context.MetadataConfidence*0.10 +
				evaluation.YearScore*0.10 +
				evaluation.EpisodeScore*0.30
	} else {
		evaluation.Confidence =
			evaluation.TitleScore*0.55 +
				evaluation.VariantConfidence*0.15 +
				context.MetadataConfidence*0.10 +
				evaluation.YearScore*0.20
	}
	evaluation.Confidence = clampConfidence(evaluation.Confidence)
	evaluation.Accepted = true
	evaluation.Prevalidated = file.RawSize >= context.MinEarlyExitSize &&
		evaluation.Confidence >= candidatePrevalidationThreshold
	evaluation.Reason = "accepted"
	return evaluation
}
