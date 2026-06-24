package addon

import (
    "regexp"
    "strconv"
    "strings"
    "sync"
    "unicode"

    "github.com/alexsergivan/transliterator"
)

var translit = transliterator.NewTransliterator(nil)

// Transliterate converts non-Latin scripts to Latin approximation.
func Transliterate(s string) string {
    hasNonLatin := false
    for _, r := range s {
        if r > unicode.MaxASCII {
            hasNonLatin = true
            break
        }
    }
    if !hasNonLatin {
        return s
    }
    return translit.Transliterate(s, "en")
}

// IsLatinString checks if a string contains only Latin characters.
// Updated to support Latin Extended-A/B and Latin Extended Additional (Vietnamese).
func IsLatinString(s string) bool {
    for _, r := range s {
        if r > 0x1EFF {
            return false
        }
    }
    return true
}

// GetAlternativeTitles generates sanitized and transliterated alternatives for a given title
func GetAlternativeTitles(name string) []string {
    var alts []string
    return injectNormalizedAltTitle(name, alts)
}

// Static Low-Entropy Grammatical Stop Words Set for PN-SILEC Filtering
var stopWords = map[string]bool{
    // English
    "the": true, "a": true, "an": true, "and": true, "or": true,
    "of": true, "in": true, "on": true, "at": true, "to": true,
    "for": true, "with": true, "by": true, "from": true, "aka": true,
    // French
    "le": true, "la": true, "les": true, "un": true, "une": true,
    "des": true, "du": true, "de": true, "et": true, "ou": true,
    // Spanish
    "el": true, "los": true, "las": true, "un": true, "una": true,
    "y": true, "o": true, "del": true, "en": true,
    // German
    "der": true, "die": true, "das": true, "den": true, "dem": true,
    "ein": true, "eine": true, "und": true, "oder": true, "von": true,
    "zu": true, "mit": true, "auf": true,
    // Italian
    "il": true, "lo": true, "la": true, "i": true, "gli": true,
    "un": true, "una": true, "e": true, "di": true,
    // Portuguese
    "o": true, "os": true, "as": true, "um": true, "uma": true,
    "e": true, "ou": true, "do": true, "da": true,
    // Dutch
    "het": true, "een": true, "en": true, "of": true,
}

// Technical tags that should not trigger the single-word guardrail.
var metadataWords = map[string]bool{
    "1080p": true, "720p": true, "2160p": true, "480p": true, "360p": true,
    "4k": true, "uhd": true, "bluray": true, "bdrip": true, "brrip": true,
    "webdl": true, "webrip": true, "hdrip": true, "dvdrip": true, "pdtv": true,
    "hdtv": true, "cam": true, "camrip": true, "hdcam": true, "ts": true,
    "hdts": true, "tc": true, "predvd": true, "dvdscr": true, "screener": true,
    "scr": true, "hq": true, "v2": true, "v3": true, "hc": true, "clean": true,
    "imax": true, "h264": true, "x264": true, "h265": true, "x265": true,
    "hevc": true, "aac": true, "aac3": true, "dts": true, "dd51": true,
    "truehd": true, "ac3": true, "mp3": true, "xvid": true, "divx": true,
    "av1": true, "vp9": true, "hdr10": true, "hdr": true, "dv": true,
    "dolby": true, "vision": true, "atmos": true, "dts-hd": true, "ma": true,
    "dual": true, "audio": true, "dubbed": true, "dub": true, "multi": true,
    "hindi": true, "tamil": true, "telugu": true, "malayalam": true,
    "kannada": true, "bengali": true, "marathi": true, "punjabi": true,
    "english": true, "spanish": true, "french": true, "italic": true,
    "russian": true, "korean": true, "japanese": true, "chinese": true,
    "german": true, "dutch": true, "swedish": true, "norwegian": true,
    "danish": true, "finnish": true, "polish": true, "czech": true,
    "greek": true, "turkish": true, "arabic": true, "hebrew": true,
    "thai": true, "vietnamese": true, "indonesian": true, "malay": true,
    "mandarin": true, "cantonese": true,
    "51": true, "71": true, "20": true, "10bit": true, "remux": true,
    "3d": true, "sdr": true, "gb": true, "mb": true, "kb": true,
    "web": true, "dl": true, "hd": true,
    "complete": true, "repack": true, "proper": true, "vostfr": true,
    "subs": true, "sub": true, "esub": true, "vof": true, "vff": true,
    "vf": true, "season": true, "series": true, "episode": true, "pack": true,
    "mkv": true, "mp4": true, "avi": true, "mov": true, "wmv": true, "flv": true, "webm": true,
    "rar": true, "zip": true, "par2": true, "nfo": true, "srt": true,
    "us": true, "uk": true, "ca": true, "nz": true, "au": true,
    "fr": true, "de": true, "jp": true, "kr": true, "cn": true,
    "hk": true, "tw": true, "it": true, "es": true, "nl": true,
    "pl": true, "ru": true, "se": true, "no": true, "fi": true,
    "dk": true, "new": true, "full": true, "all": true,
    // Editions & Variants
    "extended": true, "edition": true, "theatrical": true, "uncut": true,
    "uncensored": true, "remastered": true, "criterion": true,
    "director": true, "directors": true, "cut": true,
    "special": true, "deluxe": true, "limited": true,
    "anniversary": true, "collector": true, "collector's": true,
    "fan": true, "edit": true, "fanedit": true,
    // Streaming Services
    "nflx": true, "netflix": true, "nf": true,
    "amzn": true, "amazon": true, "prime": true,
    "atvp": true, "appletv": true, "apple": true,
    "dsnp": true, "disney": true,
    "hmax": true, "hbomax": true, "hbo": true, "max": true,
    "hulu": true, "pcok": true, "peacock": true,
    "pmtp": true, "pamp": true, "paramount": true,
    "cr": true, "crunchyroll": true, "crunch": true,
    "stan": true, "bfi": true, "mubi": true, "sho": true, "tubi": true,
}

// sequelIndicators are words that strongly suggest a different franchise entry.
// Removed common false positives: "last", "final", "next", "new"
var sequelIndicators = map[string]bool{
    "part": true, "chapter": true, "episode": true, "season": true,
    "volume": true, "vol": true, "book": true, "returns": true,
    "rises": true, "begins": true, "forever": true, "legacy": true,
    "fallout": true, "crusade": true, "dynasty": true, "empire": true,
    "revenge": true, "resurrection": true, "reloaded": true,
    "revolutions": true, "origins": true, "awakens": true,
}

// homoglyphClasses maps standard stylizations/leetspeak lookalikes to represent equivalence classes.
var homoglyphClasses = map[rune][]rune{
    '0': {'0', 'o'},
    'o': {'0', 'o'},
    '1': {'1', 'i', 'l', '!'},
    'i': {'1', 'i', 'l', '!'},
    'l': {'1', 'i', 'l', '!'},
    '3': {'3', 'e'},
    'e': {'3', 'e'},
    '4': {'4', 'a', '@'},
    'a': {'4', 'a', '@'},
    '5': {'5', 's'},
    's': {'5', 's'},
    '7': {'7', 't', 'v', 'l'},
    't': {'7', 't'},
    'v': {'7', 'v'},
    '8': {'8', 'b'},
    'b': {'8', 'b'},
    '9': {'9', 'g'},
    'g': {'9', 'g'},
}

var writtenNumbers = map[string]string{
    "one": "1", "first": "1", "1st": "1",
    "two": "2", "second": "2", "2nd": "2",
    "three": "3", "third": "3", "3rd": "3",
    "four": "4", "fourth": "4", "4th": "4",
    "five": "5", "fifth": "5", "5th": "5",
    "six": "6", "sixth": "6", "6th": "6",
    "seven": "7", "seventh": "7", "7th": "7",
    "eight": "8", "eighth": "8", "8th": "8",
    "nine": "9", "ninth": "9", "9th": "9",
    "ten": "10", "tenth": "10", "10th": "10",
    "eleven": "11", "eleventh": "11", "11th": "11",
    "twelve": "12", "twelfth": "12", "12th": "12",
}

var sequelContexts = map[string]bool{
    "part": true, "vol": true, "volume": true, "chapter": true,
    "episode": true, "season": true, "act": true, "entry": true,
}

var ignoredNumbers = map[string]bool{
    "1080": true, "2160": true, "720": true, "480": true, "360": true,
    "576": true, "264": true, "265": true, "10": true, "8": true,
}

var seasonRangeRegex = regexp.MustCompile(`(?i)\b(?:s|season|seasons)\s*0*(\d+)\s*(?:-|to)\s*0*(\d+)\b`)

var romanFalsePositives = map[string]bool{
    "mix": true, "dim": true, "vim": true, "civil": true,
    "maid": true, "dial": true, "midi": true, "id": true,
    "did": true, "mid": true, "lid": true, "vid": true,
    "mic": true, "max": true, "maxim": true, "min": true,
    "mild": true, "mind": true, "mill": true, "milk": true,
}

// isBlockedArchive checks if a torrent name is a compressed archive that Stremio cannot play
func isBlockedArchive(name string) bool {
    lower := strings.ToLower(name)
    return strings.HasSuffix(lower, ".rar") ||
        strings.HasSuffix(lower, ".zip") ||
        strings.HasSuffix(lower, ".7z") ||
        strings.HasSuffix(lower, ".tar") ||
        strings.HasSuffix(lower, ".tgz") ||
        strings.HasSuffix(lower, ".gz")
}

func containsNonASCII(s string) bool {
    for _, r := range s {
        if r > 127 {
            return true
        }
    }
    return false
}

func stripLeadingArticles(s string) string {
    s = strings.TrimSpace(s)
    lower := strings.ToLower(s)

    // Prefix articles
    prefixes := []string{"the ", "a ", "an ", "le ", "la ", "les ", "l'", "der ", "die ", "das ", "el ", "los ", "las ", "il ", "lo ", "i ", "gli ", "o ", "os ", "as ", "de ", "het "}
    for _, art := range prefixes {
        if strings.HasPrefix(lower, art) {
            return s[len(art):]
        }
    }

    // Suffix-style articles (e.g., "Avengers, The")
    suffixes := []string{", the", ", a", ", an", ", le", ", la", ", les", ", der", ", die", ", das", ", el", ", los", ", las", ", il", ", lo", ", la", ", de", ", het"}
    for _, suff := range suffixes {
        if strings.HasSuffix(lower, suff) {
            // Move suffix to prefix
            return strings.TrimSpace(suff[2:]) + " " + strings.TrimSpace(s[:len(s)-len(suff)])
        }
    }

    return s
}

// cleanWord converts a string to lowercase and removes non-alphanumeric characters.
func cleanWord(w string) string {
    hasUpperOrNonAlpha := false
    for i := 0; i < len(w); i++ {
        c := w[i]
        if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
            hasUpperOrNonAlpha = true
            break
        }
    }
    if !hasUpperOrNonAlpha {
        return w
    }

    var buf []byte
    for i := 0; i < len(w); i++ {
        c := w[i]
        if c >= 'A' && c <= 'Z' {
            buf = append(buf, c+32)
        } else if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
            buf = append(buf, c)
        }
    }
    return string(buf)
}

// isYearNumber checks if a string is a standard 4-digit release year
func isYearNumber(s string) bool {
    if len(s) != 4 {
        return false
    }
    n, err := strconv.Atoi(s)
    if err != nil {
        return false
    }
    return n >= 1880 && n <= 2100
}

// isTechnicalToken performs an allocation-free dynamic check to identify season, episode,
// and pack-specific serialization tokens, allowing them to safely bypass the guardrail.
func isTechnicalToken(s string) bool {
    if metadataWords[s] || stopWords[s] {
        return true
    }

    if isNumber(s) {
        return true
    }

    if len(s) >= 2 {
        first := s[0]
        if (first == 's' || first == 'e' || first == 'p') && isNumber(s[1:]) {
            return true
        }
        if len(s) >= 3 {
            prefix2 := s[:2]
            if (prefix2 == "se" || prefix2 == "ep") && isNumber(s[2:]) {
                return true
            }
        }
        if len(s) >= 4 {
            if s[:3] == "epi" && isNumber(s[3:]) {
                return true
            }
        }
        if len(s) >= 5 {
            prefix4 := s[:4]
            if (prefix4 == "seas" || prefix4 == "part") && isNumber(s[4:]) {
                return true
            }
        }
        if len(s) >= 7 {
            if s[:6] == "season" && isNumber(s[6:]) {
                return true
            }
        }
        if len(s) >= 8 {
            if s[:7] == "episode" && isNumber(s[7:]) {
                return true
            }
        }
    }
    return false
}

// passTitleGuardrail prevents single-word titles (e.g. "Up", "It") from matching
// unrelated multi-word torrents (e.g. "Upgraded", "Italian").
func passTitleGuardrail(targetTitle, parsedTitle string) bool {
    cleanTarget := strings.Trim(strings.ToLower(targetTitle), " .-_[]()/\\")
    cleanParsed := strings.Trim(strings.ToLower(parsedTitle), " .-_[]()/\\")

    if cleanTarget == cleanParsed {
        return true
    }

    targetNoArt := stripLeadingArticles(cleanTarget)
    parsedNoArt := stripLeadingArticles(cleanParsed)
    if targetNoArt == parsedNoArt {
        return true
    }

    targetWords := strings.Fields(targetNoArt)
    parsedWords := strings.Fields(parsedNoArt)

    // ── UPGRADE: Substantive Word Guardrail ──
    targetWordSet := make(map[string]bool)
    for _, w := range targetWords {
        targetWordSet[cleanWord(w)] = true
    }

    hasUnrelatedSubstantiveWord := false
    for _, w := range parsedWords {
        cw := cleanWord(w)
        if cw == "" {
            continue
        }
        if targetWordSet[cw] || isTechnicalToken(cw) {
            continue
        }
        hasUnrelatedSubstantiveWord = true
        break
    }

    if hasUnrelatedSubstantiveWord {
        return false
    }

    // ── UPGRADE: PN-SILEC Multi-Word Franchise Leakage Guardrail ──
    if len(targetWords) > 1 && len(parsedWords) > len(targetWords) {
        startsSame := true
        for i := 0; i < len(targetWords); i++ {
            if cleanWord(parsedWords[i]) != cleanWord(targetWords[i]) {
                startsSame = false
                break
            }
        }

        if startsSame {
            extraWords := parsedWords[len(targetWords):]
            hasSubstantiveProperNoun := false
            for _, w := range extraWords {
                cw := cleanWord(w)
                if cw == "" {
                    continue
                }
                if !isTechnicalToken(cw) {
                    hasSubstantiveProperNoun = true
                    break
                }
            }
            if hasSubstantiveProperNoun {
                return false
            }
        }
    }

    // ── Standard Single-Word Title Guardrail ──
    if len(targetWords) == 1 {
        singleWord := cleanWord(targetWords[0])
        if len(parsedWords) > 1 {
            hasExtraNonMeta := false
            for _, w := range parsedWords {
                cw := cleanWord(w)
                if cw != "" && cw != singleWord && !isTechnicalToken(cw) {
                    hasExtraNonMeta = true
                    break
                }
            }
            if hasExtraNonMeta {
                return false
            }
            return true
        }
    }
    return true
}

func getHomoglyphRepresentations(r rune) []rune {
    if classes, ok := homoglyphClasses[r]; ok {
        return classes
    }
    return []rune{r}
}

// Global recycled pool for fast map reuse
var uint64MapPool = sync.Pool{
    New: func() interface{} {
        return make(map[uint64]struct{}, 64)
    },
}

func clearMap(m map[uint64]struct{}) {
    for k := range m {
        delete(m, k)
    }
}

// OverlapCoefficient computes the overlap coefficient between two strings
// using multi-representation homoglyph character bigrams.
func OverlapCoefficient(s1, s2 string) float64 {
    if s1 == s2 {
        return 1.0
    }

    if len(s1) < 2 || len(s2) < 2 {
        return 0.0
    }

    bg1 := uint64MapPool.Get().(map[uint64]struct{})
    bg2 := uint64MapPool.Get().(map[uint64]struct{})
    defer func() {
        clearMap(bg1)
        uint64MapPool.Put(bg1)
        clearMap(bg2)
        uint64MapPool.Put(bg2)
    }()

    var lastRune rune
    hasLast := false
    for _, r := range s1 {
        if !hasLast {
            lastRune = r
            hasLast = true
            continue
        }
        repsA := getHomoglyphRepresentations(lastRune)
        repsB := getHomoglyphRepresentations(r)
        for _, charA := range repsA {
            for _, charB := range repsB {
                packed := (uint64(charA) << 32) | uint64(charB)
                bg1[packed] = struct{}{}
            }
        }
        lastRune = r
    }

    intersection := 0
    hasLast = false
    for _, r := range s2 {
        if !hasLast {
            lastRune = r
            hasLast = true
            continue
        }
        repsA := getHomoglyphRepresentations(lastRune)
        repsB := getHomoglyphRepresentations(r)
        for _, charA := range repsA {
            for _, charB := range repsB {
                packed := (uint64(charA) << 32) | uint64(charB)
                if _, ok := bg2[packed]; !ok {
                    bg2[packed] = struct{}{}
                    if _, exists := bg1[packed]; exists {
                        intersection++
                    }
                }
            }
        }
        lastRune = r
    }

    if len(bg1) == 0 || len(bg2) == 0 {
        return 0.0
    }

    minSize := len(bg1)
    if len(bg2) < minSize {
        minSize = len(bg2)
    }

    return float64(intersection) / float64(minSize)
}

func isRomanSequence(s string) bool {
    if len(s) < 2 {
        return false
    }
    lower := strings.ToLower(s)
    if romanFalsePositives[lower] {
        return false
    }
    for _, r := range s {
        if r != 'i' && r != 'v' && r != 'x' && r != 'l' && r != 'c' && r != 'd' && r != 'm' {
            return false
        }
    }
    return true
}

func isNumber(s string) bool {
    if s == "" {
        return false
    }
    for _, c := range s {
        if c < '0' || c > '9' {
            return false
        }
    }
    return true
}

func romanToArabic(s string) int {
    romanMap := map[rune]int{
        'i': 1, 'v': 5, 'x': 10, 'l': 50, 'c': 100, 'd': 500, 'm': 1000,
    }
    total := 0
    lastVal := 0
    for i := len(s) - 1; i >= 0; i-- {
        val, ok := romanMap[rune(s[i])]
        if !ok {
            return 0
        }
        if val < lastVal {
            total -= val
        } else {
            total += val
            lastVal = val
        }
    }
    return total
}

func normalizeNumbersInTitle(title string) string {
    titleClean := strings.ReplaceAll(title, ":", " ")
    titleClean = strings.ReplaceAll(titleClean, "-", " ")

    words := strings.Fields(strings.ToLower(titleClean))
    for i, w := range words {
        if numDigit, ok := writtenNumbers[w]; ok {
            words[i] = numDigit
            continue
        }

        if isRomanSequence(w) {
            shouldConvert := false
            if len(w) >= 2 {
                shouldConvert = true
            } else if len(w) == 1 {
                if i > 0 && sequelContexts[words[i-1]] {
                    shouldConvert = true
                }
                if i == len(words)-1 {
                    shouldConvert = true
                }
            }

            if shouldConvert {
                val := romanToArabic(w)
                if val > 0 {
                    words[i] = strconv.Itoa(val)
                }
            }
        }
    }
    return strings.Join(words, " ")
}

func extractNonYearNumbers(s string) []string {
    var nums []string
    var current strings.Builder
    for _, r := range s {
        if unicode.IsDigit(r) {
            current.WriteRune(r)
        } else {
            if current.Len() > 0 {
                val := current.String()
                if !ignoredNumbers[val] && !isYearNumber(val) {
                    nums = append(nums, val)
                }
                current.Reset()
            }
        }
    }
    if current.Len() > 0 {
        val := current.String()
        if !ignoredNumbers[val] && !isYearNumber(val) {
            nums = append(nums, val)
        }
    }
    return nums
}

func hasNumericMismatch(target, parsed string) bool {
    targetNums := extractNonYearNumbers(target)
    parsedNums := extractNonYearNumbers(parsed)

    if len(targetNums) == 0 || len(parsedNums) == 0 {
        return false
    }

    for _, tn := range targetNums {
        tnInt, err1 := strconv.Atoi(tn)
        if err1 != nil {
            continue
        }
        for _, pn := range parsedNums {
            pnInt, err2 := strconv.Atoi(pn)
            if err2 == nil && tnInt == pnInt {
                return false
            }
        }
    }
    return true
}

func sequelGuardrail(targetTitle, parsedTitle string, score float64) float64 {
    cleanTarget := strings.Trim(strings.ToLower(targetTitle), " .-_[]()/\\")
    cleanParsed := strings.Trim(strings.ToLower(parsedTitle), " .-_[]()/\\")

    cleanTarget = normalizeNumbersInTitle(cleanTarget)
    cleanParsed = normalizeNumbersInTitle(cleanParsed)

    targetNoArt := stripLeadingArticles(cleanTarget)
    parsedNoArt := stripLeadingArticles(cleanParsed)

    shorter := len(targetNoArt)
    longer := len(parsedNoArt)
    if shorter > longer {
        shorter, longer = longer, shorter
    }

    if longer == 0 || shorter == 0 {
        return score
    }

    ratio := float64(longer) / float64(shorter)
    if ratio <= 1.3 {
        return score
    }

    if !strings.Contains(targetNoArt, parsedNoArt) && !strings.Contains(parsedNoArt, targetNoArt) {
        return score
    }

    var longerStr, shorterStr string
    if len(targetNoArt) > len(parsedNoArt) {
        longerStr, shorterStr = targetNoArt, parsedNoArt
    } else {
        longerStr, shorterStr = parsedNoArt, targetNoArt
    }

    var extra string
    if strings.HasPrefix(longerStr, shorterStr) {
        extra = strings.TrimSpace(longerStr[len(shorterStr):])
    } else if strings.HasSuffix(longerStr, shorterStr) {
        extra = strings.TrimSpace(longerStr[:len(longerStr)-len(shorterStr)])
    } else {
        return score
    }

    extraWords := strings.Fields(extra)
    for _, w := range extraWords {
        cw := cleanWord(w)
        if isRomanSequence(cw) || (isNumber(cw) && !isYearNumber(cw)) || sequelIndicators[cw] {
            return score * (float64(shorter) / float64(longer))
        }
    }

    return score
}

func tokenPositionOverlap(s1, s2 string) float64 {
    t1 := strings.Fields(strings.ToLower(s1))
    t2 := strings.Fields(strings.ToLower(s2))
    if len(t1) == 0 || len(t2) == 0 {
        return 0
    }
    minLen := len(t1)
    if len(t2) < minLen {
        minLen = len(t2)
    }
    matches := 0
    for i := 0; i < minLen; i++ {
        if cleanWord(t1[i]) == cleanWord(t2[i]) {
            matches++
        }
    }
    return float64(matches) / float64(minLen)
}

func getTitleSimilarity(tmdbTitle, torrentName string) float64 {
    if tmdbTitle == "" {
        return 0
    }
    parsed := RobustParseInfo(torrentName, 0)
    if parsed.Title == "" {
        return 0
    }

    cleanTmdb := strings.Trim(strings.ToLower(tmdbTitle), " .-_[]()/\\")
    cleanParsed := strings.Trim(strings.ToLower(parsed.Title), " .-_[]()/\\")

    cleanTmdb = normalizeNumbersInTitle(cleanTmdb)
    cleanParsed = normalizeNumbersInTitle(cleanParsed)

    if hasNumericMismatch(cleanTmdb, cleanParsed) {
        return 0.0
    }

    oc := OverlapCoefficient(cleanTmdb, cleanParsed)
    posOc := tokenPositionOverlap(cleanTmdb, cleanParsed)

    // Blend bigram overlap with positional word overlap
    oc = (oc * 0.7) + (posOc * 0.3)

    cleanTmdbNoArt := stripLeadingArticles(cleanTmdb)
    cleanParsedNoArt := stripLeadingArticles(cleanParsed)
    if cleanTmdbNoArt != cleanTmdb || cleanParsedNoArt != cleanParsed {
        ocClean := OverlapCoefficient(cleanTmdbNoArt, cleanParsedNoArt)
        posOcClean := tokenPositionOverlap(cleanTmdbNoArt, cleanParsedNoArt)
        ocClean = (ocClean * 0.7) + (posOcClean * 0.3)
        if ocClean > oc {
            oc = ocClean
        }
    }

    oc = sequelGuardrail(tmdbTitle, parsed.Title, oc)

    return oc
}

// stripDiacritics maps standard Latin-1 and advanced unicode diacritics to ASCII base characters
func stripDiacritics(s string) string {
    var replacer = strings.NewReplacer(
        "ā", "a", "á", "a", "à", "a", "ä", "a", "â", "a", "ã", "a", "å", "a",
        "ē", "e", "é", "e", "è", "e", "ë", "e", "ê", "e",
        "ī", "i", "í", "i", "ì", "i", "ï", "i", "î", "i",
        "ō", "o", "ó", "o", "ò", "o", "ö", "o", "ô", "o", "õ", "o", "ø", "o",
        "ū", "u", "ú", "u", "ù", "u", "ü", "u", "û", "u",
        "ý", "y", "ÿ", "y",
        "ñ", "n", "ç", "c",
        "Ā", "A", "Á", "A", "À", "A", "Ä", "A", "Â", "A", "Ã", "A", "Å", "A",
        "Ē", "E", "É", "E", "È", "E", "Ë", "E", "Ê", "E",
        "Ī", "I", "Í", "I", "Ì", "I", "Ï", "I", "Î", "I",
        "Ō", "O", "Ó", "O", "Ò", "O", "Ö", "O", "Ô", "O", "Õ", "O", "Ø", "O",
        "Ū", "U", "Ú", "U", "Ù", "U", "Ü", "U", "Û", "U",
        "Ý", "Y", "Ñ", "N", "Ç", "C",
    )
    return replacer.Replace(s)
}

// injectNormalizedAltTitle adds the un-accented ASCII representation to AltTitles if it differs from the primary name
func injectNormalizedAltTitle(name string, alts []string) []string {
    normalized := stripDiacritics(name)
    if normalized != name {
        isUnique := true
        for _, existing := range alts {
            if existing == normalized {
                isUnique = false
                break
            }
        }
        if isUnique {
            alts = append(alts, normalized)
        }
    }
    return alts
}
