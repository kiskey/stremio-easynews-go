package addon

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Static Low-Entropy Grammatical Stop Words Set for PN-SILEC Filtering
var stopWords = map[string]bool{
	"the": true, "a": true, "an": true, "and": true, "or": true,
	"of": true, "in": true, "on": true, "at": true, "to": true,
	"for": true, "with": true, "by": true, "from": true, "aka": true,
	"la": true, "le": true, "les": true, "el": true, "un": true, "une": true,
}

// Technical tags that should not trigger the single-word guardrail.
// These are common torrent metadata tokens that appear after the actual movie title.
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
	"english": true, "spanish": true, "french": true, "italian": true,
	"russian": true, "korean": true, "japanese": true, "chinese": true,
	"51": true, "71": true, "20": true, "10bit": true, "remux": true,
	"3d": true, "sdr": true, "gb": true, "mb": true, "kb": true,
	"web": true, "dl": true, "hd": true,
	"complete": true, "repack": true, "proper": true, "vostfr": true,
	"subs":     true, "sub": true, "esub": true, "vof": true, "vff": true,
	"vf":       true, "season": true, "series": true, "episode": true, "pack": true,
}

// sequelIndicators are words that strongly suggest a different franchise entry.
var sequelIndicators = map[string]bool{
	"part": true, "chapter": true, "episode": true, "season": true,
	"volume": true, "vol": true, "book": true, "returns": true,
	"rises": true, "begins": true, "forever": true, "legacy": true,
	"fallout": true, "crusade": true, "dynasty": true, "empire": true,
	"revenge": true, "resurrection": true, "reloaded": true,
	"revolutions": true, "origins": true, "awakens": true,
	"last": true, "final": true, "next": true, "new": true,
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
	articles := []string{"the ", "a ", "an ", "le ", "la ", "les ", "l'"}
	for _, art := range articles {
		if strings.HasPrefix(s, art) {
			return strings.TrimPrefix(s, art)
		}
	}
	return s
}

func cleanWord(w string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return -1
	}, strings.ToLower(w))
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
// unrelated multi-word torrents (e.g. "Upgraded", "Italian"). It allows metadata
// words (codecs, quality tags, languages) to pass through.
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
				return false // ❌ REJECTED (Substantive Proper-Noun Detected)
			}
		}
	}

	// ── Standard Single-Word Title Guardrail (Preserved & Fine-Tuned) ──
	if len(targetWords) == 1 {
		singleWord := cleanWord(targetWords[0])
		if len(parsedWords) > 1 {
			firstWord := cleanWord(parsedWords[0])
			if firstWord == singleWord {
				return true
			}

			hasExtraNonMeta := false
			for _, w := range parsedWords {
				cw := cleanWord(w)
				if cw != "" && cw != singleWord && !isTechnicalToken(cw) {
					hasExtraNonMeta = true
					break
				}
			}
			if hasExtraNonMeta {
				return false // ❌ REJECTED
			}
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

// OverlapCoefficient computes the overlap coefficient between two strings
// using multi-representation homoglyph character bigrams.
func OverlapCoefficient(s1, s2 string) float64 {
	if s1 == s2 {
		return 1.0
	}

	if len(s1) < 2 || len(s2) < 2 {
		return 0.0
	}

	bg1 := make(map[string]struct{}, len(s1)*2)
	runes1 := []rune(s1)
	for i := 0; i < len(runes1)-1; i++ {
		repsA := getHomoglyphRepresentations(runes1[i])
		repsB := getHomoglyphRepresentations(runes1[i+1])
		for _, charA := range repsA {
			for _, charB := range repsB {
				bg1[string(charA)+string(charB)] = struct{}{}
			}
		}
	}

	bg2 := make(map[string]struct{}, len(s2)*2)
	runes2 := []rune(s2)
	intersection := 0
	for i := 0; i < len(runes2)-1; i++ {
		repsA := getHomoglyphRepresentations(runes2[i])
		repsB := getHomoglyphRepresentations(runes2[i+1])
		for _, charA := range repsA {
			for _, charB := range repsB {
				bigram := string(charA) + string(charB)
				if _, ok := bg2[bigram]; !ok {
					bg2[bigram] = struct{}{}
					if _, exists := bg1[bigram]; exists {
						intersection++
					}
				}
			}
		}
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
	if s == "" {
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
				if !ignoredNumbers[val] && !(len(val) == 4 && (strings.HasPrefix(val, "19") || strings.HasPrefix(val, "20"))) {
					nums = append(nums, val)
				}
				current.Reset()
			}
		}
	}
	if current.Len() > 0 {
		val := current.String()
		if !ignoredNumbers[val] && !(len(val) == 4 && (strings.HasPrefix(val, "19") || strings.HasPrefix(val, "20"))) {
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
		if isRomanSequence(cw) || isNumber(cw) || sequelIndicators[cw] {
			return score * (float64(shorter) / float64(longer))
		}
	}

	return score
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

	cleanTmdbNoArt := stripLeadingArticles(cleanTmdb)
	cleanParsedNoArt := stripLeadingArticles(cleanParsed)
	if cleanTmdbNoArt != cleanTmdb || cleanParsedNoArt != cleanParsed {
		ocClean := OverlapCoefficient(cleanTmdbNoArt, cleanParsedNoArt)
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
