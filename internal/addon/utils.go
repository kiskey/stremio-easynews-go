package addon

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	tnp "github.com/ProfChaos/torrent-name-parser"
	"github.com/kiskey/stremio-easynews-go/internal/api"
	"github.com/kiskey/stremio-easynews-go/internal/shared"
)

// ---------------------------------------------------------------------------
// Pre-Compiled Regular Expressions (Avoid compile allocations on hot paths)
// ---------------------------------------------------------------------------

var (
	shortDurationRe   = regexp.MustCompile(`^\d+s`)
	veryShortDurRe    = regexp.MustCompile(`^[0-5]m`)
	separatorsRe      = regexp.MustCompile(`[\.\-_:\s]+`)
	bracketsRe        = regexp.MustCompile(`[\[\]\(\){}]`)
	nonAlphanumericRe = regexp.MustCompile(`[^\w\s\x{00C0}-\x{00FF}]`)
	seasonEpisodeRe   = regexp.MustCompile(`(?i)s\d+e\d+`)
	yearPatternRe     = regexp.MustCompile(`\b(19\d{2}|20\d{2})\b`)
	fourDigitYearRe   = regexp.MustCompile(`\b(\d{4})\b`)
	digitsOnlyRe      = regexp.MustCompile(`\d+`)
	floatValueRe      = regexp.MustCompile(`[\d.]+`)

	fallbackQualityPatterns = []struct {
		re      *regexp.Regexp
		quality string
	}{
		{regexp.MustCompile(`\b720p\b`), "720p"},
		{regexp.MustCompile(`\b1080p\b`), "1080p"},
		{regexp.MustCompile(`\b2160p\b`), "4K/2160p"},
		{regexp.MustCompile(`\b4k\b`), "4K"},
		{regexp.MustCompile(`\buhd\b`), "4K/UHD"},
		{regexp.MustCompile(`\bhdr\b`), "HDR"},
		{regexp.MustCompile(`\bhq\b`), "HQ"},
		{regexp.MustCompile(`\bbdrip\b`), "BDRip"},
		{regexp.MustCompile(`\bbluray\b`), "BluRay"},
		{regexp.MustCompile(`\bweb-?dl\b`), "WEB-DL"},
	}
)

// IsLatinString checks if a string contains exclusively ASCII printable characters
// or standard European accented Latin-1 Supplement characters (German, Spanish, French, etc.).
// Rejects Japanese, Korean, Chinese, Arabic, and Cyrillic character sets.
// Optimized down to a single branch-predicted CPU instruction.
func IsLatinString(s string) bool {
	for _, r := range s {
		if r > 0x00FF {
			return false
		}
	}
	return true
}

// GetAlternativeTitles serves as a clean, backward-compatible stub returning the main title.
// Advanced alternate titles are now fetched dynamically via the TMDB API inside the meta module.
func GetAlternativeTitles(title string) []string {
	return []string{title}
}

// ---------------------------------------------------------------------------
// Bad Video Logic
// ---------------------------------------------------------------------------

func IsBadVideo(file api.FileData) bool {
	duration := file.GetDuration()

	if shortDurationRe.MatchString(duration) {
		return true
	}
	if veryShortDurRe.MatchString(duration) {
		return true
	}
	if file.Passwd {
		return true
	}
	if file.Virus {
		return true
	}
	if strings.ToUpper(file.Type) != "VIDEO" {
		return true
	}
	if file.RawSize > 0 && file.RawSize < 20*1024*1024 {
		return true // < 20MB
	}
	return false
}

// ---------------------------------------------------------------------------
// Title Sanitization
// ---------------------------------------------------------------------------

func SanitizeTitle(title string) string {
	result := title
	result = strings.ReplaceAll(result, "ä", "ae")
	result = strings.ReplaceAll(result, "ö", "oe")
	result = strings.ReplaceAll(result, "ü", "ue")
	result = strings.ReplaceAll(result, "ß", "ss")
	result = strings.ReplaceAll(result, "Ä", "Ae")
	result = strings.ReplaceAll(result, "Ö", "Oe")
	result = strings.ReplaceAll(result, "Ü", "Ue")
	result = strings.ReplaceAll(result, "&", "and")
	result = separatorsRe.ReplaceAllString(result, " ")
	result = bracketsRe.ReplaceAllString(result, " ")
	result = nonAlphanumericRe.ReplaceAllString(result, "")
	
	result = strings.ToLower(result)
	result = strings.TrimSpace(result)
	return result
}

// ---------------------------------------------------------------------------
// Title Matching Execution
// ---------------------------------------------------------------------------

func MatchesTitle(title, query string, strict bool) bool {
	sanitizedQuery := SanitizeTitle(query)
	sanitizedTitle := SanitizeTitle(title)

	mainQueryPart := seasonEpisodeRe.Split(sanitizedQuery, 2)[0]
	mainQueryPart = strings.TrimSpace(mainQueryPart)

	if strict {
		hasSeasonEpisode := seasonEpisodeRe.MatchString(sanitizedQuery)

		if hasSeasonEpisode {
			queryWords := strings.Fields(mainQueryPart)

			if sanitizedTitle == mainQueryPart {
				return true
			}

			seMatch := seasonEpisodeRe.FindStringIndex(sanitizedTitle)
			if seMatch != nil {
				titleBeforeSE := strings.TrimSpace(sanitizedTitle[:seMatch[0]])
				if titleBeforeSE == mainQueryPart {
					return true
				}

				titleWithoutYear := strings.TrimSpace(yearPatternRe.ReplaceAllString(titleBeforeSE, ""))
				if titleWithoutYear == mainQueryPart {
					return true
				}

				titleWordsWithoutYear := strings.Fields(titleWithoutYear)
				if len(titleWordsWithoutYear) > len(queryWords) {
					return false
				}
			} else {
				return false
			}

			seIdx := seasonEpisodeRe.FindStringIndex(sanitizedTitle)
			if seIdx == nil {
				return false
			}
			titleBeforeSE := strings.TrimSpace(sanitizedTitle[:seIdx[0]])
			titleWithoutYear := strings.TrimSpace(yearPatternRe.ReplaceAllString(titleBeforeSE, ""))
			titleWordsWithoutYear := strings.Fields(titleWithoutYear)

			isExactWordMatch := true
			for i, qw := range queryWords {
				if i >= len(titleWordsWithoutYear) || titleWordsWithoutYear[i] != qw {
					isExactWordMatch = false
					break
				}
			}
			return isExactWordMatch
		}

		parsed, err := tnp.ParseName(title)
		if err == nil && parsed.Title != "" {
			sanitizedParsed := SanitizeTitle(parsed.Title)
			queryWords := strings.Fields(sanitizedQuery)

			if sanitizedParsed == sanitizedQuery {
				return true
			}

			if parsed.Year > 0 {
				titleWithoutYear := strings.TrimSpace(yearPatternRe.ReplaceAllString(sanitizedParsed, ""))
				if titleWithoutYear == sanitizedQuery {
					return true
				}
			}

			queryYearMatch := fourDigitYearRe.FindString(sanitizedQuery)
			if queryYearMatch != "" && parsed.Year > 0 {
				queryWithoutYear := strings.TrimSpace(fourDigitYearRe.ReplaceAllString(sanitizedQuery, ""))
				titleWithoutYear := strings.TrimSpace(yearPatternRe.ReplaceAllString(sanitizedParsed, ""))
				if queryWithoutYear == titleWithoutYear && strconv.Itoa(parsed.Year) == queryYearMatch {
					return true
				}
			}

			parsedTitleWithoutYear := strings.TrimSpace(yearPatternRe.ReplaceAllString(sanitizedParsed, ""))
			parsedWordsWithoutYear := strings.Fields(parsedTitleWithoutYear)
			if len(parsedWordsWithoutYear) > len(queryWords) {
				return false
			}
		}
		return false
	}

	if seasonEpisodeRe.MatchString(sanitizedQuery) {
		seMatch := seasonEpisodeRe.FindString(sanitizedQuery)
		if seMatch != "" {
			pattern := strings.ToLower(seMatch)
			if !strings.Contains(sanitizedTitle, pattern) {
				return false
			}

			nameWords := strings.Fields(seasonEpisodeRe.ReplaceAllString(sanitizedQuery, " "))
			nameWords = filterLongWords(nameWords, 2)

			if len(nameWords) == 0 {
				return true
			}

			matching := 0
			for _, w := range nameWords {
				if strings.Contains(sanitizedTitle, w) {
					matching++
				}
			}
			return float64(matching)/float64(len(nameWords)) >= 0.7
		}
	}

	queryWords := strings.Fields(sanitizedQuery)
	for _, word := range queryWords {
		if len(word) <= 2 {
			continue
		}
		if !strings.Contains(sanitizedTitle, word) {
			return false
		}
	}

	if len(queryWords) > 1 {
		significant := filterLongWords(queryWords, 2)
		if len(significant) > 0 {
			matching := 0
			for _, w := range significant {
				if strings.Contains(sanitizedTitle, w) {
					matching++
				}
			}
			return float64(matching)/float64(len(significant)) >= 0.7
		}
	}

	return true
}

func filterLongWords(words []string, minLen int) []string {
	out := make([]string, 0, len(words))
	for _, w := range words {
		if len(w) > minLen {
			out = append(out, w)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Missing Base URL Error Pattern
// ---------------------------------------------------------------------------

type MissingBaseUrlError struct {
	msg string
}

func (e *MissingBaseUrlError) Error() string { return e.msg }

// ---------------------------------------------------------------------------
// URL and Path Construction Helpers
// ---------------------------------------------------------------------------

func CreateStreamUrl(downURL string, dlFarm string, dlPort int, username, password, filePath, baseUrl string) (string, error) {
	effectiveBaseUrl := baseUrl
	if effectiveBaseUrl == "" {
		effectiveBaseUrl = os.Getenv("ADDON_BASE_URL")
	}

	// Safeguard: Escape any raw spaces in file paths before constructing target URLs to prevent parsing failures
	sanitizedFilePath := strings.ReplaceAll(filePath, " ", "%20")

	if effectiveBaseUrl == "" {
		if os.Getenv("ALLOW_INSECURE_CREDENTIAL_URLS") == "true" {
			url := fmt.Sprintf("%s/%s/%d/%s",
				strings.Replace(downURL, "https://", fmt.Sprintf("https://%s:%s@", username, password), 1),
				dlFarm, dlPort, sanitizedFilePath)
			return url, nil
		}
		return "", &MissingBaseUrlError{
			msg: "createStreamUrl: no baseUrl available. Re-install via /configure or set ADDON_BASE_URL",
		}
	}

	fullUrl := fmt.Sprintf("%s/%s/%d/%s", downURL, dlFarm, dlPort, sanitizedFilePath)
	authUrl := fmt.Sprintf("%s?u=%s&p=%s", fullUrl, url.QueryEscape(username), url.QueryEscape(password))
	
	// Optimized: Uses RawURLEncoding (unpadded) to match Node's 'base64url' output exactly, preventing player URL parsing errors
	encodedUrl := base64.RawURLEncoding.EncodeToString([]byte(authUrl))
	
	fileName := path.Base(filePath)
	normalizedBase := strings.TrimRight(effectiveBaseUrl, "/")

	return fmt.Sprintf("%s/resolve/%s/%s", normalizedBase, encodedUrl, fileName), nil
}

func CreateStreamPath(file api.FileData) string {
	postHash := file.GetHash()
	postTitle := file.GetPostTitle()
	ext := file.GetPathExt()
	return postHash + ext + "/" + postTitle + ext
}

// ---------------------------------------------------------------------------
// Quality Processing
// ---------------------------------------------------------------------------

func GetQuality(title string, fallbackResolution string) string {
	parsed, err := tnp.ParseName(title)
	if err == nil && parsed.Resolution != "" {
		resStr := string(parsed.Resolution)
		if resStr == "2160p" || strings.Contains(resStr, "4k") {
			return "4K"
		}
		return resStr
	}

	lowerTitle := strings.ToLower(title)
	for _, p := range fallbackQualityPatterns {
		if p.re.MatchString(lowerTitle) {
			return p.quality
		}
	}

	if fallbackResolution != "" {
		return fallbackResolution
	}
	return ""
}

// ---------------------------------------------------------------------------
// Advanced, Or-Fanned Solr Query Builders (Sonarr / Scene compliant)
// ---------------------------------------------------------------------------

func BuildSearchQuery(contentType string, meta MetaProviderResponse) string {
	exclusions := " !sample !trailer !passwd !password !preview"

	switch contentType {
	case "movie":
		baseQuery := meta.Name
		if meta.Year > 0 {
			return fmt.Sprintf("%s %d%s", baseQuery, meta.Year, exclusions)
		}
		return baseQuery + exclusions

	case "series":
		if meta.Episode != "" && meta.Season != "" {
			sNum, _ := strconv.Atoi(meta.Season)
			eNum, _ := strconv.Atoi(meta.Episode)

			if sNum > 0 && eNum > 0 {
				v1 := fmt.Sprintf("S%02dE%02d", sNum, eNum) // S01E05
				v2 := fmt.Sprintf("%dx%02d", sNum, eNum)   // 1x05
				v3 := fmt.Sprintf("%d%02d", sNum, eNum)    // 105
				v4 := fmt.Sprintf("S%dE%d", sNum, eNum)     // S1E5

				episodeOrPipe := fmt.Sprintf("%s|%s|%s|%s", v1, v2, v3, v4)

				return fmt.Sprintf("%s %s%s", meta.Name, episodeOrPipe, exclusions)
			}
		}
		return meta.Name + exclusions

	default:
		return meta.Name + exclusions
	}
}

// ---------------------------------------------------------------------------
// Simple String Helpers
// ---------------------------------------------------------------------------

func ExtractDigits(value string) *int {
	if value == "" {
		return nil
	}
	match := digitsOnlyRe.FindString(value)
	if match == "" {
		return nil
	}
	n, _ := strconv.Atoi(match)
	return &n
}

func GetSize(file api.FileData) string          { return file.GetSize() }
func GetDuration(file api.FileData) string      { return file.GetDuration() }
func GetPostTitle(file api.FileData) string     { return file.GetPostTitle() }
func GetFileExtension(file api.FileData) string { return file.GetExtension() }

func CapitalizeFirstLetter(str string) string {
	if str == "" {
		return str
	}
	return strings.ToUpper(str[:1]) + str[1:]
}

func IsAuthError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "auth") || strings.Contains(s, "login") ||
		strings.Contains(s, "username") || strings.Contains(s, "password") ||
		strings.Contains(s, "credentials") || strings.Contains(s, "unauthorized") ||
		strings.Contains(s, "forbidden")
}

type ErrorContext struct {
	Resource string
	ID       string
	Type     string
}

func LogError(logger shared.Logger, msg string, err error, ctx ErrorContext) {
	logger.Error("Error: %s | resource=%s id=%s type=%s err=%v", msg, ctx.Resource, ctx.ID, ctx.Type, err)
}

func GetPublishDate(timestamp int64) string {
	if timestamp == 0 {
		return ""
	}
	uploadDate := time.Unix(timestamp, 0)
	diff := time.Since(uploadDate)
	days := int(diff.Hours() / 24)
	if days < 1 {
		days = 1
	}
	return fmt.Sprintf("📅 %dd", days)
}

func CreateThumbnailUrl(res api.EasynewsSearchResponse, file api.FileData) string {
	id := file.GetHash()
	idChars := ""
	if len(id) >= 3 {
		idChars = id[:3]
	}
	thumbnailSlug := file.GetPostTitle()
	return fmt.Sprintf("%s%s/pr-%s.jpg/th-%s.jpg", res.ThumbURL, idChars, id, thumbnailSlug)
}

// ---------------------------------------------------------------------------
// Sorting Preparation Helpers
// ---------------------------------------------------------------------------

func QualityScoreFromLabel(quality string) int {
	if quality == "" {
		return 0
	}
	q := strings.ToUpper(quality)
	if strings.Contains(q, "4K") || strings.Contains(q, "2160P") ||
		strings.Contains(q, "UHD") || strings.Contains(q, "2160") ||
		strings.Contains(q, "ULTRA HD") {
		return 4
	}
	if strings.Contains(q, "1080P") || strings.Contains(q, "1080") {
		return 3
	}
	if strings.Contains(q, "720P") || strings.Contains(q, "720") {
		return 2
	}
	if strings.Contains(q, "480P") || strings.Contains(q, "480") || strings.Contains(q, "SD") {
		return 1
	}
	return 0
}

func ParseSizeForSort(size string) (unit string, value float64) {
	if strings.Contains(size, "GB") {
		v, _ := strconv.ParseFloat(floatValueRe.FindString(size), 64)
		return "GB", v
	}
	if strings.Contains(size, "MB") {
		v, _ := strconv.ParseFloat(floatValueRe.FindString(size), 64)
		return "MB", v
	}
	return "", 0
}

func CompareSizeMeta(a, b *SortMeta) int {
	if a == nil || b == nil {
		return 0
	}
	if a.SizeUnit == "GB" && b.SizeUnit == "GB" {
		if a.SizeValue > b.SizeValue {
			return -1
		} else if a.SizeValue < b.SizeValue {
			return 1
		}
		return 0
	}
	if a.SizeUnit == "GB" && b.SizeUnit == "MB" {
		return -1
	}
	if a.SizeUnit == "MB" && b.SizeUnit == "GB" {
		return 1
	}
	if a.SizeUnit == "MB" && b.SizeUnit == "MB" {
		if a.SizeValue > b.SizeValue {
			return -1
		} else if a.SizeValue < b.SizeValue {
			return 1
		}
		return 0
	}
	return 0
}
