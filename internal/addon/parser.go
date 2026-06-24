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
    Title    string
    Season   int
    Episode  int
    Episodes []int
    Year     int
    Language string
    Quality  string
    IsPack   bool
    IsProper bool
    IsRepack bool
    Edition  string
    Date     string
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

// Collapses spaces and symbols between SXX and EP(XX) to force standard SXXEXX grouping
// Expanded to handle EP, Episode, E, and various separators (., -, _, space)
var epPatternRegex = regexp.MustCompile(`(?i)(S\d+)?[\s\-_.]*\b(?:EP|EPISODE|E)[\s\-_.]*[\(\[]?\s*(\d+)\s*[\)\]]?\b`)
var urlRegex = regexp.MustCompile(`\b(https?://\S+|www\.\S+\.\w+|[\w.-]+@[\w.-]+)\b`)
var bracketRegex = regexp.MustCompile(`\[.*?[^\w\s-].*?\]`)

var rangeRegex = regexp.MustCompile(`(?i)\b(?:e|ep|episode)?\s*(\d+)\s*(?:-|to)\s*(?:e|ep|episode)?\s*(\d+)\b`)
var seasonFolderRegex = regexp.MustCompile(`(?i)\b(?:s|season|series)\s*0*(\d+)\b`)
var dateEpisodeRegex = regexp.MustCompile(`(?i)\b(\d{4})[\.\-_ ](\d{2})[\.\-_ ](\d{2})\b`)

// Low-Allocation pre-defined filters deconstructed from Perl badges.json to RE2 standard.
var filtersDef = []struct {
    ID        string
    GroupID   string
    Name      string
    Positive  string
    Negatives []string
}{
    // Quality
    {"q-r", "gq", "Remux", `(?i)\bremux\b`, nil},
    {"q-b", "gq", "BluRay", `(?i)\b(?:blu[-_. ]?ray|b[rd][-_. ]?rip)\b`, []string{`(?i)\bremux\b`}},
    {"q-w", "gq", "WEB-DL", `(?i)\bweb[-_. ]?dl\b`, nil},
    {"src-webrip", "gq", "WEBRip", `(?i)\bweb[-_. ]?rip\b`, nil},
    {"src-hdtv", "gq", "HDTV", `(?i)\bhdtv\b`, nil},
    {"src-hdrip", "gq", "HDRip", `(?i)\bhd[-_. ]?rip\b`, nil},
    {"src-dvdrip", "gq", "DVDRip", `(?i)\bdvd[-_. ]?rip\b`, nil},

    // Resolution
    {"r-4k", "gr", "4K", `(?i)\b2160[pi]?\b|\b4k\b|\buhd\b`, []string{`(?i)\b1080[pi]?\b|\b720[pi]?\b`}},
    {"r-1080", "gr", "1080p", `(?i)\b1080[pi]?\b`, nil},
    {"r-720", "gr", "720p", `(?i)\b720[pi]?\b`, nil},

    // Visual
    {"v-seadex", "gv", "SeaDex", `(?i)\b(?:seadex|best[\s._-]?release|alt[\s._-]?release)\b|ᴀʟᴛ ʀᴇʟᴇᴀsᴇ|ʙᴇsᴛ ʀᴇʟᴇᴀsᴇ`, nil},
    {"v-hdr10p", "gv", "HDR10+", `(?i)\bhdr[\s._-]?10[\s._-]?(?:\+|plus|p)(?:\b|[^a-z0-9]|$)\b`, []string{`(?i)\b(?:dv|dovi|dolby[\s._-]?vision)\b`}},
    {"v-hdr10", "gv", "HDR10", `(?i)\bhdr[\s._-]?10\b`, []string{`(?i)\b(?:dv|dovi|dolby[\s._-]?vision)\b`, `(?i)\bhdr[\s._-]?10[\s._-]?(?:\+|plus|p)(?:\b|[^a-z0-9]|$)\b`}},
    {"v-hdr", "gv", "HDR", `(?i)\bhdr\b`, []string{`(?i)\b(?:dv|dovi|dolby[\s._-]?vision)\b`, `(?i)\bhdr[\s._-]?10\b`}},
    {"v-sdr", "gv", "SDR", `(?i)\bsdr\b`, []string{`(?i)\b(?:hdr|hdr10|hdr10\+|dv|dovi|dolby[\s._-]?vision)\b`}},
    {"v-imax-e", "gv", "IMAX Enhanced", `(?i)\bimax[\s._-]?enhanced\b`, nil},
    {"v-imax", "gv", "IMAX", `(?i)\bimax\b`, []string{`(?i)\benhanced\b`}},
    {"a-dv", "gv", "DV", `(?i)\b(?:dv|dovi|dolby[\s._-]?vision)\b`, nil},

    // Audio
    {"a-dtsx", "ga", "DTS:X", `(?i)\bdts[-_.: ]?x\b`, nil},
    {"a-dtsma", "ga", "DTS-HD MA", `(?i)\bdts[-_. ]?(?:hd[-_. ]?)?ma\b`, []string{`(?i)\bdts[-_.: ]?x\b`}},
    {"a-dtshd", "ga", "DTS-HD", `(?i)\bdts[-_. ]?hd\b`, []string{`(?i)\bdts[-_. ]?(?:hd[-_. ]?)?ma\b`, `(?i)\bdts[-_.: ]?x\b`}},
    {"a-dts", "ga", "DTS", `(?i)\bdts\b`, []string{`(?i)\bdts[-_. ]?(?:hd|ma|xll|x)\b`}},
    {"a-at", "ga", "Atmos", `(?i)\batmos\b`, nil},
    {"a-th", "ga", "TrueHD", `(?i)\btrue[\s._-]?hd\b`, nil},
    {"a-dp", "ga", "DD+", `(?i)\b(?:ddp|dd\+|eac-?3|e-?ac-?3)\b`, []string{`(?i)\btrue[\s._-]?hd\b`}},
    {"a-dd", "ga", "DD", `(?i)\b(?:dd[25][. ][01]|ac-?3)\b`, []string{`(?i)\b(?:ddp|dd\+|eac-?3|e-?ac-?3)\b`, `(?i)\batmos\b`, `(?i)\btrue[\s._-]?hd\b`}},

    // Channels
    {"ch-71", "gc", "7.1", `(?i)(?:^|[^0-9])[7-8][. ][01](?:[^0-9]|$)\b`, nil},
    {"ch-51", "gc", "5.1", `(?i)(?:^|[^0-9])5[. ][01](?:[^0-9]|$)\b`, []string{`(?i)(?:^|[^0-9])[7-8][. ][01](?:[^0-9]|$)\b`}},

    // Streaming
    {"s-nflx", "gs", "NETFLIX", `(?i)\b(?:nflx|netflix|nf)\b`, nil},
    {"s-amzn", "gs", "PRIME VIDEO", `(?i)\b(?:amzn|amazon|prime[\s._-]?video)\b`, nil},
    {"s-atvp", "gs", "APPLE TV+", `(?i)\b(?:atvp|apple[\s._-]?tv\+?|appletv)\b`, nil},
    {"s-dsnp", "gs", "DISNEY+", `(?i)\b(?:dsnp|dsny|disney\+?|disney[\s._-]?plus)\b`, nil},
    {"s-hmax", "gs", "HBO MAX", `(?i)(?:\b(?:hmax|hbomax|hbo[\s._-]?max)\b|(?:^|[\s._-])max(?:[\s._-]|$))`, nil},
    {"s-hulu", "gs", "HULU", `(?i)\bhulu\b`, nil},
    {"s-pcok", "gs", "PEACOCK", `(?i)\b(?:pcok|peacock)\b`, nil},
    {"s-pamp", "gs", "PARAMOUNT+", `(?i)\b(?:pmtp|pamp|paramount\+?|paramount[\s._-]?plus)\b`, nil},
    {"s-croll", "gs", "CRUNCHYROLL", `(?i)\b(?:crunchyroll|crunch)\b`, nil},

    // Encoder
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

// ParsePackOrRange checks if a torrent name is a complete pack or contains an episode range
func ParsePackOrRange(name string, targetE int) (isPack bool, startE int, endE int, hasRange bool) {
    lower := strings.ToLower(name)
    if strings.Contains(lower, "complete") || strings.Contains(lower, "pack") || strings.Contains(lower, "bundle") {
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

// FormatBadges scans the source filename exactly once and extracts matched tags.
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

    // 1. Replace non-breaking spaces (\u00a0, \u200b) to standard spaces
    s = strings.ReplaceAll(s, "\u00a0", " ")
    s = strings.ReplaceAll(s, "\u200b", " ")

    // 2. Unicode NFKC normalization (converts full-width characters to half-width)
    s = norm.NFKC.String(s)

    // 3. Normalize episode patterns (e.g. S02 EP(15) -> S02E15)
    s = normalizeEpisodePatterns(s)

    // 4. Remove residual URLs/domains (e.g. www.BTHDTV.com)
    s = urlRegex.ReplaceAllString(s, " ")

    // 5. Remove residual empty/garbage brackets
    s = bracketRegex.ReplaceAllString(s, " ")

    // 6. Collapse whitespace
    s = strings.Join(strings.Fields(s), " ")

    // 7. Trim leftover leading/trailing punctuation
    s = strings.TrimLeft(s, " .-_[]()/\\")
    s = strings.TrimRight(s, " .-_[]()/\\")
    return s
}

// Global LRU Cache for RobustParseInfo
var robustParseCache = NewBoundedCache[string, *ParseResult](10000, 24*time.Hour)

func RobustParseInfo(title string, fallbackSeason int) *ParseResult {
    if cached, ok := robustParseCache.Get(title); ok {
        return cached
    }

    clean := SanitizeName(title)

    var result *ParseResult

    // Check for daily/date-based episodes first (e.g., Daily.Show.2024.05.15)
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
                Title:    info.SeriesTitle,
                Season:   info.SeasonNumber,
                Episode:  episode,
                Episodes: episodes,
                Year:     info.SeriesTitleInfo.Year,
                Language: lang,
                Quality:  getQuality(info.Quality.Quality.Resolution),
                IsPack:   IsPack(info),
            }
        } else {
            movie := rtp.ParseMovieTitle(clean)
            if movie != nil {
                lang := "en"
                if len(movie.Languages) > 0 {
                    lang = getISO(movie.Languages[0])
                }
                result = &ParseResult{
                    Title:    movie.PrimaryMovieTitle(),
                    Season:   0,
                    Episode:  0,
                    Episodes: []int{},
                    Year:     movie.Year,
                    Language: lang,
                    Quality:  getQuality(movie.Quality.Quality.Resolution),
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

    // Extract Editions, PROPER, REPACK
    lowerClean := strings.ToLower(clean)
    if strings.Contains(lowerClean, "proper") {
        result.IsProper = true
    }
    if strings.Contains(lowerClean, "repack") {
        result.IsRepack = true
    }

    editionPatterns := map[string]string{
        `director'?s?\s*cut`: "Director's Cut",
        `extended\s*(?:edition|cut)?`: "Extended",
        `theatrical`: "Theatrical",
        `uncut`: "Uncut",
        `uncensored`: "Uncensored",
        `remastered`: "Remastered",
        `criterion`: "Criterion",
        `imax`: "IMAX",
    }
    for pattern, name := range editionPatterns {
        if matched, _ := regexp.MatchString("(?i)"+pattern, clean); matched {
            result.Edition = name
            break
        }
    }

    robustParseCache.Set(title, result)
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
            Title:    info.SeriesTitle,
            Season:   season,
            Episode:  episode,
            Episodes: episodes,
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
        strings.Contains(p, "interview")
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

func FindBestSeriesFile(candidates []CandidateFile, targetSeason, targetEpisode, fallbackSeason int) (CandidateFile, bool) {
    var bestCandidate CandidateFile
    var found bool
    var maxWeight int64 = -1

    checkExtra := isExtraOrSpecial
    if targetSeason == 0 {
        checkExtra = isExtraOrSpecialRelaxed
    }

    // 1. Direct and Range-based Scanning with Size-weighting
    for _, c := range candidates {
        if checkExtra(c.Path) {
            continue
        }

        cleanPath := normalizeEpisodePatterns(c.Path)
        info := ParseFilePath(cleanPath, fallbackSeason)

        matched := false
        // Check standard parsing match
        if info.Season == targetSeason && info.Episode == targetEpisode {
            matched = true
        }

        // Check multi-episode parsed array
        if !matched && info.Season == targetSeason && len(info.Episodes) > 0 {
            for _, ep := range info.Episodes {
                if ep == targetEpisode {
                    matched = true
                    break
                }
            }
        }

        // Check Range Regex (e.g. S01E21-22)
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

    // 2. Index-Based Sequential Match Fallback (For absolute numbering in folder packs)
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
            return strings.Compare(strings.ToLower(seasonMatches[i].Path), strings.ToLower(seasonMatches[j].Path)) < 0
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
