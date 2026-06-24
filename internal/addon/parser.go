package addon

import (
    "fmt"
    "regexp"
    "sort"
    "strconv"
    "strings"
    "time"

    "golang.org/x/text/unicode/norm"

    rtp "github.com/ovrlord-app/releasetitleparser"
)

type ParseResult struct {
    Title        string
    Season       int
    Episode      int
    Episodes     []int
    Year         int
    Language     string
    Quality      string
    IsPack       bool
    IsProper     bool
    IsRepack     bool
    Edition      string
    Date         string
    ReleaseGroup string
}

type CandidateFile struct {
    ID   int
    Path string
    Size int64
}

type BadgeFilter struct {
    ID        string
    GroupID   string
    Name      string
    Positive  *regexp.Regexp
    Negatives []*regexp.Regexp
}

var languageToISO = map[rtp.Language]string{
    rtp.LanguageEnglish:       "en",
    rtp.LanguageSpanish:       "es",
    rtp.LanguageGerman:        "de",
    rtp.LanguageFrench:        "fr",
    rtp.LanguageItalian:       "it",
    rtp.LanguageRussian:       "ru",
    rtp.LanguageJapanese:      "ja",
    rtp.LanguageChinese:       "zh",
    rtp.LanguageKorean:        "ko",
    rtp.LanguagePortuguese:    "pt",
    rtp.LanguagePortugueseBR:  "pt-BR",
    rtp.LanguageDutch:         "nl",
    rtp.LanguageDanish:        "da",
    rtp.LanguageNorwegian:     "no",
    rtp.LanguageSwedish:       "sv",
    rtp.LanguageFinnish:       "fi",
    rtp.LanguagePolish:        "pl",
    rtp.LanguageCzech:         "cs",
    rtp.LanguageSlovak:        "sk",
    rtp.LanguageHungarian:     "hu",
    rtp.LanguageRomanian:      "ro",
    rtp.LanguageBulgarian:     "bg",
    rtp.LanguageUkrainian:     "uk",
    rtp.LanguageGreek:         "el",
    rtp.LanguageTurkish:       "tr",
    rtp.LanguageArabic:        "ar",
    rtp.LanguageHindi:         "hi",
    rtp.LanguageThai:          "th",
    rtp.LanguageVietnamese:    "vi",
    rtp.LanguageHebrew:        "he",
    rtp.LanguagePersian:       "fa",
    rtp.LanguageBengali:       "bn",
    rtp.LanguageLatvian:       "lv",
    rtp.LanguageLithuanian:    "lt",
    rtp.LanguageSpanishLatino: "es-MX",
    rtp.LanguageTamil:         "ta",
    rtp.LanguageTelugu:        "te",
    rtp.LanguageMalayalam:     "ml",
    rtp.LanguageKannada:       "kn",
    rtp.LanguageAlbanian:      "sq",
    rtp.LanguageAfrikaans:     "af",
    rtp.LanguageMarathi:       "mr",
    rtp.LanguageTagalog:       "tl",
    rtp.LanguageIcelandic:     "is",
    rtp.LanguageFlemish:       "nl-BE",
    rtp.LanguageUrdu:          "ur",
    rtp.LanguageMongolian:     "mn",
    rtp.LanguageGeorgian:      "ka",
    rtp.LanguageRomansh:       "rm",
    rtp.LanguageOriginal:      "original",
    rtp.LanguageCatalan:       "ca",
    rtp.LanguageAzerbaijani:   "az",
    rtp.LanguageUzbek:         "uz",
}

var epPatternRegex = regexp.MustCompile(`(?i)(S\d+)?[\s\-_.]*\b(?:EP|EPISODE|E)[\s\-_.]*[\(\[]?\s*(\d+)\s*[\)\]]?\b`)
var urlRegex = regexp.MustCompile(`\b(https?://\S+|www\.\S+\.\w+|[\w.-]+@[\w.-]+)\b`)
var bracketRegex = regexp.MustCompile(`\[.*?[^\w\s-].*?\]`)

var rangeRegex = regexp.MustCompile(`(?i)\b(?:e|ep|episode)?\s*(\d+)\s*(?:-|to)\s*(?:e|ep|episode)?\s*(\d+)\b`)
var seasonFolderRegex = regexp.MustCompile(`(?i)\b(?:s|season|series|vol|volume|book|chapter)\s*0*(\d+)\b`)
var dateEpisodeRegex = regexp.MustCompile(`(?i)\b(\d{4})[\.\-_ ](\d{2})[\.\-_ ](\d{2})\b`)

var properWordBoundaryRe = regexp.MustCompile(`(?i)\bproper\b`)
var repackWordBoundaryRe = regexp.MustCompile(`(?i)\brepack\b`)

var compiledEditionPatterns = []struct {
    Re   *regexp.Regexp
    Name string
}{
    {regexp.MustCompile(`(?i)director'?s?\s*cut`), "Director's Cut"},
    {regexp.MustCompile(`(?i)extended\s*(?:edition|cut)?`), "Extended"},
    {regexp.MustCompile(`(?i)theatrical`), "Theatrical"},
    {regexp.MustCompile(`(?i)uncut`), "Uncut"},
    {regexp.MustCompile(`(?i)uncensored`), "Uncensored"},
    {regexp.MustCompile(`(?i)remastered`), "Remastered"},
    {regexp.MustCompile(`(?i)criterion`), "Criterion"},
    {regexp.MustCompile(`(?i)imax`), "IMAX"},
}

var packRegex = regexp.MustCompile(`(?i)\b(?:(?:complete|full)\s+(?:series|collection|season|s?\d+)|season\s+complete|season\s+\d+(?:\s*-\s*(?:season\s+)?\d+)?\s+complete|(?:season\s+)?s\d+(?:\s*-\s*s\d+)?\s+complete|pack|bundle|all\s+episodes?)\b`)

var naturalCompareRegex = regexp.MustCompile(`\d+`)

var filtersDef = []struct {
    ID        string
    GroupID   string
    Name      string
    Positive  string
    Negatives []string
}{
    {"q-r", "gq", "Remux", `(?i)\bremux\b`, nil},
    {"q-b", "gq", "BluRay", `(?i)\b(?:blu[-_. ]?ray|b[rd][-_. ]?rip)\b`, []string{`(?i)\bremux\b`}},
    {"q-w", "gq", "WEB-DL", `(?i)\bweb[-_. ]?dl\b`, nil},
    {"src-webrip", "gq", "WEBRip", `(?i)\bweb[-_. ]?rip\b`, nil},
    {"src-hdtv", "gq", "HDTV", `(?i)\bhdtv\b`, nil},
    {"src-hdrip", "gq", "HDRip", `(?i)\bhd[-_. ]?rip\b`, nil},
    {"src-dvdrip", "gq", "DVDRip", `(?i)\bdvd[-_. ]?rip\b`, nil},

    {"r-4k", "gr", "4K", `(?i)\b2160[pi]?\b|\b4k\b|\buhd\b`, []string{`(?i)\b1080[pi]?\b|\b720[pi]?\b`}},
    {"r-1080", "gr", "1080p", `(?i)\b1080[pi]?\b`, nil},
    {"r-720", "gr", "720p", `(?i)\b720[pi]?\b`, nil},

    {"v-seadex", "gv", "SeaDex", `(?i)\b(?:seadex|best[\s._-]?release|alt[\s._-]?release)\b|ᴀʟᴛ ʀᴇʟᴇᴀsᴇ|ʙᴇsᴛ ʀᴇʟᴇᴀsᴇ`, nil},
    {"v-hdr10p", "gv", "HDR10+", `(?i)\bhdr[\s._-]?10[\s._-]?(?:\+|plus|p)(?:\b|[^a-z0-9]|$)\b`, []string{`(?i)\b(?:dv|dovi|dolby[\s._-]?vision)\b`}},
    {"v-hdr10", "gv", "HDR10", `(?i)\bhdr[\s._-]?10\b`, []string{`(?i)\b(?:dv|dovi|dolby[\s._-]?vision)\b`, `(?i)\bhdr[\s._-]?10[\s._-]?(?:\+|plus|p)(?:\b|[^a-z0-9]|$)\b`}},
    {"v-hdr", "gv", "HDR", `(?i)\bhdr\b`, []string{`(?i)\b(?:dv|dovi|dolby[\s._-]?vision)\b`, `(?i)\bhdr[\s._-]?10\b`}},
    {"v-sdr", "gv", "SDR", `(?i)\bsdr\b`, []string{`(?i)\b(?:hdr|hdr10|hdr10\+|dv|dovi|dolby[\s._-]?vision)\b`}},
    {"v-imax-e", "gv", "IMAX Enhanced", `(?i)\bimax[\s._-]?enhanced\b`, nil},
    {"v-imax", "gv", "IMAX", `(?i)\bimax\b`, []string{`(?i)\benhanced\b`}},
    {"a-dv", "gv", "DV", `(?i)\b(?:dv|dovi|dolby[\s._-]?vision)\b`, nil},

    {"a-dtsx", "ga", "DTS:X", `(?i)\bdts[-_.: ]?x\b`, nil},
    {"a-dtsma", "ga", "DTS-HD MA", `(?i)\bdts[-_. ]?(?:hd[-_. ]?)?ma\b`, []string{`(?i)\bdts[-_.: ]?x\b`}},
    {"a-dtshd", "ga", "DTS-HD", `(?i)\bdts[-_. ]?hd\b`, []string{`(?i)\bdts[-_. ]?(?:hd[-_. ]?)?ma\b`, `(?i)\bdts[-_.: ]?x\b`}},
    {"a-dts", "ga", "DTS", `(?i)\bdts\b`, []string{`(?i)\bdts[-_. ]?(?:hd|ma|xll|x)\b`}},
    {"a-at", "ga", "Atmos", `(?i)\batmos\b`, nil},
    {"a-th", "ga", "TrueHD", `(?i)\btrue[\s._-]?hd\b`, nil},
    {"a-dp", "ga", "DD+", `(?i)\b(?:ddp|dd\+|eac-?3|e-?ac-?3)\b`, []string{`(?i)\btrue[\s._-]?hd\b`}},
    {"a-dd", "ga", "DD", `(?i)\b(?:dd[25][. ][01]|ac-?3)\b`, []string{`(?i)\b(?:ddp|dd\+|eac-?3|e-?ac-?3)\b`, `(?i)\batmos\b`, `(?i)\btrue[\s._-]?hd\b`}},

    {"ch-71", "gc", "7.1", `(?i)(?:^|[^0-9])[7-8][. ][01](?:[^0-9]|$)\b`, nil},
    {"ch-51", "gc", "5.1", `(?i)(?:^|[^0-9])5[. ][01](?:[^0-9]|$)\b`, []string{`(?i)(?:^|[^0-9])[7-8][. ][01](?:[^0-9]|$)\b`}},

    {"s-nflx", "gs", "NETFLIX", `(?i)\b(?:nflx|netflix|nf)\b`, nil},
    {"s-amzn", "gs", "PRIME VIDEO", `(?i)\b(?:amzn|amazon|prime[\s._-]?video)\b`, nil},
    {"s-atvp", "gs", "APPLE TV+", `(?i)\b(?:atvp|apple[\s._-]?tv\+?|appletv)\b`, nil},
    {"s-dsnp", "gs", "DISNEY+", `(?i)\b(?:dsnp|dsny|disney\+?|disney[\s._-]?plus)\b`, nil},
    {"s-hmax", "gs", "HBO MAX", `(?i)(?:\b(?:hmax|hbomax|hbo[\s._-]?max)\b|(?:^|[\s._-])max(?:[\s._-]|$))`, nil},
    {"s-hulu", "gs", "HULU", `(?i)\bhulu\b`, nil},
    {"s-pcok", "gs", "PEACOCK", `(?i)\b(?:pcok|peacock)\b`, nil},
    {"s-pamp", "gs", "PARAMOUNT+", `(?i)\b(?:pmtp|pamp|paramount\+?|paramount[\s._-]?plus)\b`, nil},
    {"s-croll", "gs", "CRUNCHYROLL", `(?i)\b(?:crunchyroll|crunch)\b`, nil},

    {"s-av1", "ge", "AV1", `(?i)\bav1\b`, nil},
    {"s-h265", "ge", "H265 HEVC", `(?i)\b(?:x265|h[._-]?265|hevc)\b`, nil},
    {"s-h264", "ge", "H264 AVC", `(?i)\b(?:x264|h[._-]?264|avc)\b`, nil},
}

var CompiledFilters []BadgeFilter

func init() {
    CompiledFilters = make([]BadgeFilter, len(filtersDef))
    for i, f := range filtersDef {
        var negatives []*regexp.Regexp
        for _, negPat := range f.Negatives {
            negatives = append(negatives, regexp.MustCompile(negPat))
        }

        CompiledFilters[i] = BadgeFilter{
            ID:        f.ID,
            GroupID:   f.GroupID,
            Name:      f.Name,
            Positive:  regexp.MustCompile(f.Positive),
            Negatives: negatives,
        }
    }
}

func ParsePackOrRange(name string, targetE int) (isPack bool, startE int, endE int, hasRange bool) {
    lower := strings.ToLower(name)
    if packRegex.MatchString(lower) {
        return true, 0, 0, false
    }

    if match := rangeRegex.FindStringSubmatch(name); len(match) >= 3 {
        start, _ := strconv.Atoi(match[1])
        end, _ := strconv.Atoi(match[2])
        if targetE >= start && targetE <= end {
            return false, start, end, true
        }
    }
    return false, 0, 0, false
}

func FormatBadges(title string) string {
    var res, qual, vis, aud, ch, enc, str string

    for i := range CompiledFilters {
        f := &CompiledFilters[i]
        if f.Positive.MatchString(title) {
            excluded := false
            for _, neg := range f.Negatives {
                if neg.MatchString(title) {
                    excluded = true
                    break
                }
            }
            if excluded {
                continue
            }

            switch f.GroupID {
            case "gr":
                if res == "" {
                    res = f.Name
                }
            case "gq":
                if qual == "" {
                    qual = f.Name
                }
            case "gv":
                if vis == "" {
                    vis = f.Name
                }
            case "ga":
                if aud == "" {
                    aud = f.Name
                }
            case "gc":
                if ch == "" {
                    ch = f.Name
                }
            case "ge":
                if enc == "" {
                    enc = f.Name
                }
            case "gs":
                if str == "" {
                    str = f.Name
                }
            }
        }
    }

    parts := make([]string, 0, 7)
    if res != "" {
        parts = append(parts, "["+res+"]")
    }
    if qual != "" {
        parts = append(parts, "["+qual+"]")
    }
    if vis != "" {
        parts = append(parts, "["+vis+"]")
    }
    if aud != "" {
        parts = append(parts, "["+aud+"]")
    }
    if ch != "" {
        parts = append(parts, "["+ch+"]")
    }
    if enc != "" {
        parts = append(parts, "["+enc+"]")
    }
    if str != "" {
        parts = append(parts, "["+str+"]")
    }

    if len(parts) == 0 {
        return ""
    }
    return strings.Join(parts, " ")
}

func normalizeEpisodePatterns(s string) string {
    return epPatternRegex.ReplaceAllString(s, "${1}E${2}")
}

func getISO(lang rtp.Language) string {
    if iso, ok := languageToISO[lang]; ok {
        return iso
    }
    return "en"
}

func getQuality(res int) string {
    switch res {
    case 2160:
        return "4k"
    case 1080:
        return "1080p"
    case 720:
        return "720p"
    case 480:
        return "480p"
    case 360:
        return "360p"
    default:
        return "sd"
    }
}

func SanitizeName(name string) string {
    s := name

    s = strings.ReplaceAll(s, "\u00a0", " ")
    s = strings.ReplaceAll(s, "\u200b", " ")

    s = norm.NFKC.String(s)
    s = normalizeEpisodePatterns(s)
    s = urlRegex.ReplaceAllString(s, " ")
    s = bracketRegex.ReplaceAllString(s, " ")
    s = strings.Join(strings.Fields(s), " ")

    s = strings.TrimLeft(s, " .-_[]()/\\")
    s = strings.TrimRight(s, " .-_[]()/\\")
    return s
}

var robustParseCache = NewBoundedCache[string, *ParseResult](10000, 24*time.Hour)

func RobustParseInfo(title string, fallbackSeason int) *ParseResult {
    // Cache Key Fix: Include fallbackSeason to prevent silent cache collisions
    cacheKey := fmt.Sprintf("%s:%d", title, fallbackSeason)
    if cached, ok := robustParseCache.Get(cacheKey); ok {
        return cached
    }

    clean := SanitizeName(title)

    var result *ParseResult

    if m := dateEpisodeRegex.FindStringSubmatch(clean); len(m) == 4 {
        dateStr := fmt.Sprintf("%s-%s-%s", m[1], m[2], m[3])
        idx := strings.Index(clean, m[0])
        titleStr := strings.Trim(strings.TrimSpace(clean[:idx]), " .-_")
        
        result = &ParseResult{
            Title:    titleStr,
            Season:   fallbackSeason,
            Episode:  0,
            Episodes: []int{},
            Year:     0,
            Language: "en",
            Quality:  "sd",
            Date:     dateStr,
        }
    } else {
        info := rtp.ParseSeriesTitle(clean)
        if info != nil && (info.SeasonNumber != 0 || len(info.EpisodeNumbers) > 0) {
            lang := "en"
            if len(info.Languages) > 0 {
                lang = getISO(info.Languages[0])
            }
            episode := 0
            episodes := []int{}
            if len(info.EpisodeNumbers) > 0 {
                episode = info.EpisodeNumbers[0]
                episodes = make([]int, len(info.EpisodeNumbers))
                copy(episodes, info.EpisodeNumbers)
            }
            result = &ParseResult{
                Title:        info.SeriesTitle,
                Season:       info.SeasonNumber,
                Episode:      episode,
                Episodes:     episodes,
                Year:         info.SeriesTitleInfo.Year,
                Language:     lang,
                Quality:      getQuality(info.Quality.Quality.Resolution),
                IsPack:       IsPack(info),
                ReleaseGroup: info.ReleaseGroup,
            }
        } else {
            movie := rtp.ParseMovieTitle(clean)
            if movie != nil {
                lang := "en"
                if len(movie.Languages) > 0 {
                    lang = getISO(movie.Languages[0])
                }
                result = &ParseResult{
                    Title:        movie.PrimaryMovieTitle(),
                    Season:       0,
                    Episode:      0,
                    Episodes:     []int{},
                    Year:         movie.Year,
                    Language:     lang,
                    Quality:      getQuality(movie.Quality.Quality.Resolution),
                    ReleaseGroup: movie.ReleaseGroup,
                }
            } else {
                result = &ParseResult{
                    Title:    clean,
                    Season:   fallbackSeason,
                    Episode:  0,
                    Episodes: []int{},
                    Language: "en",
                    Quality:  "sd",
                }
            }
        }
    }

    if properWordBoundaryRe.MatchString(clean) {
        result.IsProper = true
    }
    if repackWordBoundaryRe.MatchString(clean) {
        result.IsRepack = true
    }

    var editions []string
    for _, p := range compiledEditionPatterns {
        if p.Re.MatchString(clean) {
            editions = append(editions, p.Name)
        }
    }
    if len(editions) > 0 {
        result.Edition = strings.Join(editions, " + ")
    }

    robustParseCache.Set(cacheKey, result)
    return result
}

func ParseFilePath(path string, fallbackSeason int) *ParseResult {
    fileName := path
    if idx := strings.LastIndexAny(path, "/\\"); idx != -1 {
        fileName = path[idx+1:]
    }

    cleanPath := normalizeEpisodePatterns(fileName)
    info := rtp.ParseSeriesPath(cleanPath)
    if info != nil && (info.SeasonNumber != 0 || len(info.EpisodeNumbers) > 0) {
        episode := 0
        episodes := []int{}
        if len(info.EpisodeNumbers) > 0 {
            episode = info.EpisodeNumbers[0]
            episodes = make([]int, len(info.EpisodeNumbers))
            copy(episodes, info.EpisodeNumbers)
        }
        season := info.SeasonNumber
        if season == 0 {
            season = fallbackSeason
        }
        return &ParseResult{
            Title:        info.SeriesTitle,
            Season:       season,
            Episode:      episode,
            Episodes:     episodes,
            ReleaseGroup: info.ReleaseGroup,
        }
    }
    return &ParseResult{
        Season:   fallbackSeason,
        Episode:  0,
        Episodes: []int{},
    }
}

func IsPack(info *rtp.ParsedEpisodeInfo) bool {
    return info != nil && (info.FullSeason || info.IsPartialSeason || info.IsMultiSeason)
}

func isExtraOrSpecial(path string) bool {
    p := strings.ToLower(path)
    return strings.Contains(p, "special") ||
        strings.Contains(p, "bonus") ||
        strings.Contains(p, "trailer") ||
        strings.Contains(p, "featurette") ||
        strings.Contains(p, "recap") ||
        strings.Contains(p, "sample") ||
        strings.Contains(p, "extra") ||
        strings.Contains(p, "behind the scenes") ||
        strings.Contains(p, "interview") ||
        strings.Contains(p, "ova") ||
        strings.Contains(p, "ona") ||
        strings.Contains(p, "oad")
}

func isExtraOrSpecialRelaxed(path string) bool {
    p := strings.ToLower(path)
    return strings.Contains(p, "bonus") ||
        strings.Contains(p, "trailer") ||
        strings.Contains(p, "featurette") ||
        strings.Contains(p, "recap") ||
        strings.Contains(p, "sample") ||
        strings.Contains(p, "behind the scenes") ||
        strings.Contains(p, "interview")
}

func matchRange(path string, targetEpisode int) bool {
    fileName := path
    if idx := strings.LastIndexAny(path, "/\\"); idx != -1 {
        fileName = path[idx+1:]
    }

    matches := rangeRegex.FindAllStringSubmatchIndex(fileName, -1)
    for _, match := range matches {
        if len(match) >= 6 {
            startNumStart := match[2]
            startNumEnd := match[3]
            endNumStart := match[4]
            endNumEnd := match[5]

            if startNumStart > 0 && isDecimalDot(fileName, startNumStart-1) {
                continue
            }
            if endNumEnd < len(fileName) && isDecimalDot(fileName, endNumEnd) {
                continue
            }

            startStr := fileName[startNumStart:startNumEnd]
            endStr := fileName[endNumStart:endNumEnd]

            start, err1 := strconv.Atoi(startStr)
            end, err2 := strconv.Atoi(endStr)
            if err1 == nil && err2 == nil {
                if start <= end && targetEpisode >= start && targetEpisode <= end {
                    return true
                }
            }
        }
    }
    return false
}

func isDecimalDot(s string, i int) bool {
    if i <= 0 || i >= len(s)-1 {
        return false
    }
    if s[i] != '.' {
        return false
    }
    left := s[i-1]
    right := s[i+1]
    return left >= '0' && left <= '9' && right >= '0' && right <= '9'
}

func naturalCompare(a, b string) bool {
    padA := naturalCompareRegex.ReplaceAllStringFunc(a, func(m string) string {
        n, _ := strconv.Atoi(m)
        return fmt.Sprintf("%020d", n)
    })
    padB := naturalCompareRegex.ReplaceAllStringFunc(b, func(m string) string {
        n, _ := strconv.Atoi(m)
        return fmt.Sprintf("%020d", n)
    })
    return padA < padB
}

func FindBestSeriesFile(candidates []CandidateFile, targetSeason, targetEpisode, fallbackSeason int) (CandidateFile, bool) {
    var bestCandidate CandidateFile
    var found bool
    var maxWeight int64 = -1

    checkExtra := isExtraOrSpecial
    if targetSeason == 0 {
        checkExtra = isExtraOrSpecialRelaxed
    }

    for _, c := range candidates {
        if checkExtra(c.Path) {
            continue
        }

        cleanPath := normalizeEpisodePatterns(c.Path)
        info := ParseFilePath(cleanPath, fallbackSeason)

        matched := false
        if info.Season == targetSeason && info.Episode == targetEpisode {
            matched = true
        }

        if !matched && info.Season == targetSeason && len(info.Episodes) > 0 {
            for _, ep := range info.Episodes {
                if ep == targetEpisode {
                    matched = true
                    break
                }
            }
        }

        if !matched && info.Season == targetSeason && matchRange(c.Path, targetEpisode) {
            matched = true
        }

        if matched {
            if c.Size > maxWeight {
                bestCandidate = c
                maxWeight = c.Size
                found = true
            }
        }
    }

    if found {
        return bestCandidate, true
    }

    var seasonMatches []CandidateFile
    for _, c := range candidates {
        if checkExtra(c.Path) {
            continue
        }

        matches := seasonFolderRegex.FindAllStringSubmatch(c.Path, -1)
        isDifferentSeason := false
        for _, match := range matches {
            if len(match) >= 2 {
                sNum, err := strconv.Atoi(match[1])
                if err == nil && sNum != targetSeason {
                    isDifferentSeason = true
                    break
                }
            }
        }
        if isDifferentSeason {
            continue
        }

        seasonMatches = append(seasonMatches, c)
    }

    if len(seasonMatches) > 0 {
        sort.Slice(seasonMatches, func(i, j int) bool {
            return naturalCompare(strings.ToLower(seasonMatches[i].Path), strings.ToLower(seasonMatches[j].Path))
        })

        if targetEpisode > 0 && targetEpisode <= len(seasonMatches) {
            candidate := seasonMatches[targetEpisode-1]

            candParsed := ParseFilePath(candidate.Path, fallbackSeason)
            if candParsed.Episode != 0 && candParsed.Episode != targetEpisode {
                if !matchRange(candidate.Path, targetEpisode) {
                    return CandidateFile{}, false
                }
            }
            return candidate, true
        }
    }

    return CandidateFile{}, false
}
