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

    "github.com/kiskey/stremio-easynews-go/internal/api"
    "github.com/kiskey/stremio-easynews-go/internal/shared"
)

var (
    shortDurationRe   = regexp.MustCompile(`^\d+s`)
    veryShortDurRe    = regexp.MustCompile(`^[0-5]m`)
    separatorsRe      = regexp.MustCompile(`[\.\-_:\s]+`)
    bracketsRe        = regexp.MustCompile(`[\[\]\(\){}]`)
    nonAlphanumericRe = regexp.MustCompile(`[^\w\s\x{00C0}-\x{024F}\x{1E00}-\x{1EFF}]`)
    seasonEpisodeRe   = regexp.MustCompile(`(?i)(s\d+e\d+|\b\d+x\d+\b)`)
    yearPatternRe     = regexp.MustCompile(`\b(19\d{2}|20\d{2})\b`)
    fourDigitYearRe   = regexp.MustCompile(`\b(\d{4})\b`)
    digitsOnlyRe      = regexp.MustCompile(`\d+`)
    floatValueRe      = regexp.MustCompile(`[\d.]+`)

    fallbackQualityPatterns = []struct {
        re      *regexp.Regexp
        quality string
    }{
        {regexp.MustCompile(`(?i)\b720p\b`), "720p"},
        {regexp.MustCompile(`(?i)\b1080p\b`), "1080p"},
        {regexp.MustCompile(`(?i)\b2160p\b`), "4K/2160p"},
        {regexp.MustCompile(`(?i)\b4k\b`), "4K"},
        {regexp.MustCompile(`(?i)\buhd\b`), "4K/UHD"},
        {regexp.MustCompile(`(?i)\bhdr\b`), "HDR"},
        {regexp.MustCompile(`(?i)\bhq\b`), "HQ"},
        {regexp.MustCompile(`(?i)\bbdrip\b`), "BDRip"},
        {regexp.MustCompile(`(?i)\bbluray\b`), "BluRay"},
        {regexp.MustCompile(`(?i)\bweb-?dl\b`), "WEB-DL"},
    }

    solrSpecialCharsRe = regexp.MustCompile(`[+\-!(){}\[\]^"~*?:\\|]`)
)

func SanitizeSolrString(s string) string {
    s = solrSpecialCharsRe.ReplaceAllString(s, " ")
    s = strings.Join(strings.Fields(s), " ")
    return s
}

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
    if !strings.EqualFold(file.Type, "VIDEO") {
        return true
    }
    if file.RawSize > 0 && file.RawSize < 20*1024*1024 {
        return true
    }
    return false
}

var titleReplacer = strings.NewReplacer(
    "ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss",
    "Ä", "Ae", "Ö", "Oe", "Ü", "Ue", "&", "and",
)

func SanitizeTitle(title string) string {
    result := titleReplacer.Replace(title)
    result = separatorsRe.ReplaceAllString(result, " ")
    result = bracketsRe.ReplaceAllString(result, " ")
    result = nonAlphanumericRe.ReplaceAllString(result, "")
    
    result = strings.ToLower(result)
    result = strings.TrimSpace(result)
    return result
}

func MatchesTitle(title, query string, strict bool) bool {
    if !strict {
        parsed := RobustParseInfo(title, 0)
        if parsed == nil || parsed.Title == "" {
            return strings.Contains(strings.ToLower(title), strings.ToLower(query))
        }
        if !passTitleGuardrail(query, parsed.Title) {
            return false
        }
        sanitizedTitle := SanitizeTitle(parsed.Title)
        sanitizedQuery := SanitizeTitle(query)
        return strings.Contains(sanitizedTitle, sanitizedQuery)
    }

    parsed := RobustParseInfo(title, 0)
    if parsed == nil || parsed.Title == "" {
        return false
    }

    if !passTitleGuardrail(query, parsed.Title) {
        return false
    }

    similarity := getTitleSimilarity(query, title)
    return similarity >= 0.80
}

type MissingBaseUrlError struct {
    msg string
}

func (e *MissingBaseUrlError) Error() string { return e.msg }

func CreateStreamUrl(downURL string, dlFarm string, dlPort int, username, password, filePath, baseUrl string) (string, error) {
    effectiveBaseUrl := baseUrl
    if effectiveBaseUrl == "" {
        effectiveBaseUrl = os.Getenv("ADDON_BASE_URL")
    }

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

func GetQuality(title string, fallbackResolution string) string {
    parsed := RobustParseInfo(title, 0)
    if parsed != nil && parsed.Quality != "" && parsed.Quality != "sd" {
        if parsed.Quality == "4k" {
            return "4K"
        }
        return parsed.Quality
    }

    for _, p := range fallbackQualityPatterns {
        if p.re.MatchString(title) {
            return p.quality
        }
    }

    if fallbackResolution != "" {
        cleanRes := strings.ReplaceAll(strings.ToLower(fallbackResolution), " ", "")
        if strings.Contains(cleanRes, "3840x2160") || strings.Contains(cleanRes, "2160p") {
            return "4K"
        }
        if strings.Contains(cleanRes, "1920x1080") || strings.Contains(cleanRes, "1080p") {
            return "1080p"
        }
        if strings.Contains(cleanRes, "1280x720") || strings.Contains(cleanRes, "720p") {
            return "720p"
        }
        return fallbackResolution
    }
    return ""
}

// BuildOptimizedGroupedQueries completely eliminates piped format groups (|) to prevent 
// Solr operator precedence splitting. It distributes every title-format combination 
// into separate space-AND queries.
func BuildOptimizedGroupedQueries(contentType string, meta MetaProviderResponse, allTitles []string) []string {
    var safeTitles []string

    for _, t := range allTitles {
        trimmed := strings.TrimSpace(t)
        if trimmed == "" || !IsLatinString(trimmed) {
            continue
        }
        safeTitle := SanitizeSolrString(trimmed)
        if safeTitle == "" {
            continue
        }
        safeTitles = append(safeTitles, safeTitle)
    }

    var formats []string
    exclusions := " !sample !trailer !passwd !password !preview"

    if contentType == "movie" {
        if meta.Year > 0 {
            formats = append(formats, strconv.Itoa(meta.Year))
        }
    } else if contentType == "series" {
        if meta.Season != "" && meta.Episode != "" {
            s, _ := strconv.Atoi(meta.Season)
            e, _ := strconv.Atoi(meta.Episode)
            if s > 0 && e > 0 {
                formats = append(formats, fmt.Sprintf("S%02dE%02d", s, e))
                formats = append(formats, fmt.Sprintf("%dx%02d", s, e))
            }
        }
        if meta.EpisodeAirDate != "" {
            formats = append(formats, meta.EpisodeAirDate)
            dashDate := strings.ReplaceAll(meta.EpisodeAirDate, ".", "-")
            if dashDate != meta.EpisodeAirDate {
                formats = append(formats, dashDate)
            }
        }
    }

    var queries []string

    // Distribute every title over every format to ensure strict AND logic
    for _, title := range safeTitles {
        if len(formats) > 0 {
            for _, f := range formats {
                queries = append(queries, fmt.Sprintf("%s %s%s", title, f, exclusions))
            }
        } else {
            queries = append(queries, title+exclusions)
        }
    }

    return queries
}

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
