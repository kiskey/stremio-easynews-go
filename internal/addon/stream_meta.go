package addon

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/kiskey/stremio-easynews-go/internal/api"
)

func structuredSize(rawSize int64, sourceSize string) (bytes int64, unit string, value float64) {
	if rawSize > 0 {
		bytes = rawSize
		if rawSize >= bytesPerGiB {
			return bytes, "GB", float64(rawSize) / float64(bytesPerGiB)
		}
		return bytes, "MB", float64(rawSize) / float64(1024*1024)
	}

	unit, value = ParseSizeForSort(strings.ToUpper(sourceSize))
	switch unit {
	case "GB":
		bytes = int64(math.Round(value * float64(bytesPerGiB)))
	case "MB":
		bytes = int64(math.Round(value * float64(1024*1024)))
	}
	return bytes, unit, value
}

func structuredDateMs(file api.FileData) int64 {
	if file.Five != "" {
		for _, layout := range []string{
			time.RFC3339,
			"2006-01-02 15:04:05",
			"01-02-2006 15:04:05",
		} {
			if parsed, err := time.Parse(layout, file.Five); err == nil {
				return parsed.UnixMilli()
			}
		}
	}
	if file.Ts > 0 {
		return file.Ts * 1000
	}
	return 0
}

func normalizedAudioLanguages(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i]) < strings.ToLower(out[j])
	})
	return out
}

func hasPreferredAudioLanguage(values []string, preferred string) bool {
	preferred = strings.TrimSpace(preferred)
	if preferred == "" {
		return false
	}
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), preferred) {
			return true
		}
	}
	return false
}

func releaseFilterMatches(filter *BadgeFilter, title string) bool {
	if filter == nil || filter.Positive == nil || !filter.Positive.MatchString(title) {
		return false
	}
	for _, negative := range filter.Negatives {
		if negative != nil && negative.MatchString(title) {
			return false
		}
	}
	return true
}

func structuredReleaseTraits(title string) (sourceScore int, sourceLabel string, hdrScore int, hdrLabel string, codecScore int, codecLabel string) {
	for i := range CompiledFilters {
		filter := &CompiledFilters[i]
		if !releaseFilterMatches(filter, title) {
			continue
		}
		switch filter.ID {
		case "q-r":
			if sourceScore < 8 {
				sourceScore, sourceLabel = 8, filter.Name
			}
		case "q-b":
			if sourceScore < 7 {
				sourceScore, sourceLabel = 7, filter.Name
			}
		case "q-w":
			if sourceScore < 6 {
				sourceScore, sourceLabel = 6, filter.Name
			}
		case "src-webrip", "src-hdtv":
			if sourceScore < 5 {
				sourceScore, sourceLabel = 5, filter.Name
			}
		case "src-hdrip":
			if sourceScore < 4 {
				sourceScore, sourceLabel = 4, filter.Name
			}
		case "src-dvdrip":
			if sourceScore < 3 {
				sourceScore, sourceLabel = 3, filter.Name
			}
		case "a-dv":
			if hdrScore < 4 {
				hdrScore, hdrLabel = 4, filter.Name
			}
		case "v-hdr10p":
			if hdrScore < 3 {
				hdrScore, hdrLabel = 3, filter.Name
			}
		case "v-hdr10":
			if hdrScore < 2 {
				hdrScore, hdrLabel = 2, filter.Name
			}
		case "v-hdr":
			if hdrScore < 1 {
				hdrScore, hdrLabel = 1, filter.Name
			}
		case "s-av1":
			if codecScore < 3 {
				codecScore, codecLabel = 3, filter.Name
			}
		case "s-h265":
			if codecScore < 2 {
				codecScore, codecLabel = 2, filter.Name
			}
		case "s-h264":
			if codecScore < 1 {
				codecScore, codecLabel = 1, filter.Name
			}
		}
	}
	return sourceScore, sourceLabel, hdrScore, hdrLabel, codecScore, codecLabel
}

func buildStructuredSortMeta(file api.FileData, preferredLang string, parsedInfo *ParseResult, quality, sourceSize, title string) *SortMeta {
	videoSizeBytes, sizeUnit, sizeValue := structuredSize(file.RawSize, sourceSize)
	languages := normalizedAudioLanguages(file.Alangs)
	sourceScore, sourceLabel, hdrScore, hdrLabel, codecScore, codecLabel := structuredReleaseTraits(title)

	meta := &SortMeta{
		QualityScore:     QualityScoreFromLabel(quality),
		QualityCategory:  qualityCategoryFromLabel(quality),
		QualityLabel:     quality,
		SourceScore:      sourceScore,
		SourceLabel:      sourceLabel,
		HDRScore:         hdrScore,
		HDRLabel:         hdrLabel,
		CodecScore:       codecScore,
		CodecLabel:       codecLabel,
		VideoSizeBytes:   videoSizeBytes,
		SizeUnit:         sizeUnit,
		SizeValue:        sizeValue,
		DateMs:           structuredDateMs(file),
		Languages:        languages,
		HasPreferredLang: hasPreferredAudioLanguage(languages, preferredLang),
		StableKey:        candidateIdentity(file),
	}
	if parsedInfo != nil {
		meta.IsProper = parsedInfo.IsProper
		meta.IsRepack = parsedInfo.IsRepack
		meta.Edition = parsedInfo.Edition
		meta.ReleaseGroup = parsedInfo.ReleaseGroup
	}
	return meta
}
