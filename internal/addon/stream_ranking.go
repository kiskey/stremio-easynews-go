package addon

import (
	"sort"
	"strings"
)

const bytesPerGiB = int64(1024 * 1024 * 1024)

type streamSelectionOptions struct {
	SortingPreference    string
	QualityFilters       []string
	MaxFileSizeGB        float64
	MaxResultsPerQuality int
}

func qualityCategoryFromLabel(label string) string {
	q := strings.ToLower(strings.TrimSpace(label))
	switch {
	case strings.Contains(q, "4k"), strings.Contains(q, "2160"), strings.Contains(q, "uhd"), strings.Contains(q, "ultra hd"):
		return "4k"
	case strings.Contains(q, "1080"):
		return "1080p"
	case strings.Contains(q, "720"):
		return "720p"
	case strings.Contains(q, "480"), q == "sd", strings.Contains(q, "standard definition"):
		return "480p"
	default:
		return "other"
	}
}

func requestedQualityCategory(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "4k", "2160p", "2160", "uhd":
		return "4k"
	case "1080p", "1080":
		return "1080p"
	case "720p", "720":
		return "720p"
	case "480p", "480", "sd":
		return "480p"
	default:
		return ""
	}
}

func qualityFilterSelection(filters []string) (map[string]struct{}, bool) {
	defaults := map[string]struct{}{
		"4k":    {},
		"1080p": {},
		"720p":  {},
		"480p":  {},
	}
	selected := make(map[string]struct{}, len(filters))
	for _, filter := range filters {
		if category := requestedQualityCategory(filter); category != "" {
			selected[category] = struct{}{}
		}
	}
	if len(selected) == 0 {
		return nil, false
	}
	if len(selected) != len(defaults) {
		return selected, true
	}
	for category := range defaults {
		if _, ok := selected[category]; !ok {
			return selected, true
		}
	}
	return selected, false
}

func compareIntDesc(a, b int) int {
	switch {
	case a > b:
		return -1
	case a < b:
		return 1
	default:
		return 0
	}
}

func compareInt64Desc(a, b int64) int {
	switch {
	case a > b:
		return -1
	case a < b:
		return 1
	default:
		return 0
	}
}

func compareFloatDesc(a, b float64) int {
	switch {
	case a > b:
		return -1
	case a < b:
		return 1
	default:
		return 0
	}
}

func compareBoolTrueFirst(a, b bool) int {
	switch {
	case a && !b:
		return -1
	case !a && b:
		return 1
	default:
		return 0
	}
}

func properRepackRank(meta *SortMeta) int {
	if meta == nil {
		return 0
	}
	if meta.IsProper {
		return 2
	}
	if meta.IsRepack {
		return 1
	}
	return 0
}

func compareTechnical(a, b *SortMeta) int {
	if c := compareIntDesc(a.QualityScore, b.QualityScore); c != 0 {
		return c
	}
	if c := compareIntDesc(a.SourceScore, b.SourceScore); c != 0 {
		return c
	}
	if c := compareIntDesc(a.HDRScore, b.HDRScore); c != 0 {
		return c
	}
	return compareIntDesc(a.CodecScore, b.CodecScore)
}

func compareTechnicalWithinQuality(a, b *SortMeta) int {
	if c := compareIntDesc(a.SourceScore, b.SourceScore); c != 0 {
		return c
	}
	if c := compareIntDesc(a.HDRScore, b.HDRScore); c != 0 {
		return c
	}
	return compareIntDesc(a.CodecScore, b.CodecScore)
}

func structuredStreamLess(a, b Stream, preference string) bool {
	am := a.SortMeta
	bm := b.SortMeta
	if am == nil && bm == nil {
		return false
	}
	if am == nil {
		return false
	}
	if bm == nil {
		return true
	}

	var comparisons []int
	switch strings.ToLower(strings.TrimSpace(preference)) {
	case "size_first":
		comparisons = []int{
			compareInt64Desc(am.VideoSizeBytes, bm.VideoSizeBytes),
			compareFloatDesc(am.CandidateConfidence, bm.CandidateConfidence),
			compareTechnical(am, bm),
			compareBoolTrueFirst(am.HasPreferredLang, bm.HasPreferredLang),
			compareIntDesc(properRepackRank(am), properRepackRank(bm)),
			compareInt64Desc(am.DateMs, bm.DateMs),
		}
	case "date_first":
		comparisons = []int{
			compareInt64Desc(am.DateMs, bm.DateMs),
			compareFloatDesc(am.CandidateConfidence, bm.CandidateConfidence),
			compareTechnical(am, bm),
			compareBoolTrueFirst(am.HasPreferredLang, bm.HasPreferredLang),
			compareIntDesc(properRepackRank(am), properRepackRank(bm)),
			compareInt64Desc(am.VideoSizeBytes, bm.VideoSizeBytes),
		}
	case "lang_first", "language_first":
		comparisons = []int{
			compareBoolTrueFirst(am.HasPreferredLang, bm.HasPreferredLang),
			compareFloatDesc(am.CandidateConfidence, bm.CandidateConfidence),
			compareTechnical(am, bm),
			compareIntDesc(properRepackRank(am), properRepackRank(bm)),
			compareInt64Desc(am.VideoSizeBytes, bm.VideoSizeBytes),
			compareInt64Desc(am.DateMs, bm.DateMs),
		}
	default: // quality_first
		comparisons = []int{
			compareIntDesc(am.QualityScore, bm.QualityScore),
			compareFloatDesc(am.CandidateConfidence, bm.CandidateConfidence),
			compareTechnicalWithinQuality(am, bm),
			compareIntDesc(properRepackRank(am), properRepackRank(bm)),
			compareBoolTrueFirst(am.HasPreferredLang, bm.HasPreferredLang),
			compareInt64Desc(am.VideoSizeBytes, bm.VideoSizeBytes),
			compareInt64Desc(am.DateMs, bm.DateMs),
		}
	}

	for _, comparison := range comparisons {
		if comparison < 0 {
			return true
		}
		if comparison > 0 {
			return false
		}
	}

	if am.StableKey != bm.StableKey {
		return am.StableKey < bm.StableKey
	}
	return a.URL < b.URL
}

func selectAndRankStreams(streams []Stream, options streamSelectionOptions) []Stream {
	if len(streams) == 0 {
		return streams
	}

	selected := append([]Stream(nil), streams...)

	if allowed, custom := qualityFilterSelection(options.QualityFilters); custom {
		filtered := make([]Stream, 0, len(selected))
		for _, stream := range selected {
			if stream.SortMeta == nil {
				continue
			}
			if _, ok := allowed[stream.SortMeta.QualityCategory]; ok {
				filtered = append(filtered, stream)
			}
		}
		selected = filtered
	}

	if options.MaxFileSizeGB > 0 {
		maxBytesFloat := options.MaxFileSizeGB * float64(bytesPerGiB)
		maxBytes := int64(maxBytesFloat)
		filtered := make([]Stream, 0, len(selected))
		for _, stream := range selected {
			if stream.SortMeta == nil || stream.SortMeta.VideoSizeBytes <= 0 || stream.SortMeta.VideoSizeBytes <= maxBytes {
				filtered = append(filtered, stream)
			}
		}
		selected = filtered
	}

	sort.SliceStable(selected, func(i, j int) bool {
		return structuredStreamLess(selected[i], selected[j], options.SortingPreference)
	})

	if options.MaxResultsPerQuality > 0 && len(selected) > 0 {
		counts := make(map[string]int, 5)
		limited := make([]Stream, 0, len(selected))
		for _, stream := range selected {
			category := "other"
			if stream.SortMeta != nil && stream.SortMeta.QualityCategory != "" {
				category = stream.SortMeta.QualityCategory
			}
			if counts[category] >= options.MaxResultsPerQuality {
				continue
			}
			counts[category]++
			limited = append(limited, stream)
		}
		selected = limited
	}

	return selected
}
