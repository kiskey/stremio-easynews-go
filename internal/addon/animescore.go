package addon

import (
    "regexp"
    "strings"
    "time"
)

var (
    // Exhaustive Anime Release Group Regex (incorporating scene, p2p, and localized groups)
    animeGroupRe = regexp.MustCompile(`(?i)\b(?:SubsPlease|Erai[-_]?raws|HorribleSubs|ASW|Judah|Judas|Ember|Vostfr|Yamez|AnimXT|Kawaiika[-_]?Raws|Shokorefa|Fumetsu|Nanatsu|PAS|PnPSubs|SeaDex|Cleo|Anime[-_]?Time|BlueLaguna|KUC[\s._]?NG|op[\s._]?tube|Shin[\s._]?Sekai|TARDiS|CEBRAY|SiGLA|ACEM)\b`)
    
    // Explicit Anime Audio/Language Tokens
    animeLangRe = regexp.MustCompile(`(?i)\b(?:vostfr|vost|eng[\s._-]?sub|multi[\s._-]?audio|dual[\s._-]?audio|jpn[\s._-]?subs|japanese[\s._-]?sub|\.sub\.|subbed|eng[\s._-]?softsub|vostfr[\s._-]?hd|multi[\s._-]?vf2|castellano|german[\s._-]?sub|ger[\s._-]?sub)\b`)
    
    // Standard Anime CRC32 Hex Hash (e.g. [8324A32F])
    animeCrcHashRe = regexp.MustCompile(`(?i)\[[0-9a-f]{8}\]`)
    
    // Standard Western Release Group Regex (incorporating global scene and WEB/P2P labels)
    // NOTE: Removed "Kitsune" (valid anime group) and added major Western scene/P2P encoders
    westernGroupRe = regexp.MustCompile(`(?i)\b(?:RARBG|NTb|FLUX|CMRG|PHoMo|DLAA|AJP69|KiNGS|GLHF|r00t|TEPES|ROCCaT|EZTV|aXXo|TOMMY|BAE|NOSiViD|BiNGE|SYNCOPY|EDITH|MeGusta|WADU|LoRD|D3G|RBB|PortalGoods|PSA|FWB|FLAME|SAUERKRAUT|higgsboson|ntropic|QxR|Tigole|GalaxyTV)\b`)
    
    // Anime Streaming Platform Indicators
    animeSourceRe = regexp.MustCompile(`(?i)\b(?:CR|Crunchyroll|Bilibili|BILI|iQiyi|MuseAsia|AniOne)\b`)

    // Western Streaming Platform Indicators (added "NFLX" explicitly)
    westernSourceRe = regexp.MustCompile(`(?i)\b(?:NF|Netflix|NFLX|AMZN|ATVP|DSNP|HMAX|PCOK|PMTP|HULU|STAN|STANAU|SHO|TUBI|BCORE|DSNP|AppleTV|Hulu|Amazon)\b`)
    
    // Live-Action Indicators
    liveActionMarkerRe = regexp.MustCompile(`(?i)\b(?:live[\s._-]?action|LA[\s._-]|netflix[\s._-]?series)\b`)
)

// isAnimeRelease evaluates the structural signature of the filename to detect Anime
func isAnimeRelease(filename string) bool {
    lower := strings.ToLower(filename)
    
    if animeGroupRe.MatchString(filename) || animeCrcHashRe.MatchString(filename) || animeLangRe.MatchString(filename) {
        return true
    }
    // Check manual indicators from the op tube / shin sekai formats
    if strings.Contains(lower, "op tube") || strings.Contains(lower, "shin sekai") {
        return true
    }
    return false
}

// isNewerShowDisqualified checks if a file was published before the show's premiere
func isNewerShowDisqualified(fileTs int64, premiereYear int) bool {
    if fileTs == 0 || premiereYear <= 1970 {
        return false
    }
    // Disqualify if posted more than 6 months (15552000s) before premiere year start
    premiereTs := time.Date(premiereYear, time.January, 1, 0, 0, 0, 0, time.UTC).Unix()
    return fileTs < (premiereTs - 15552000)
}

// ClassifyTargetPrior calculates the log-prior LLR of the requested show from TMDB Metadata
func ClassifyTargetPrior(meta MetaProviderResponse) float64 {
    var score float64 = 0.0

    switch strings.ToLower(meta.OriginalLanguage) {
    case "ja":
        score += 6.0
    case "en":
        score -= 1.0
    case "zh":
        score += 1.0
    case "ko":
        score += 0.5
    }

    if meta.IsAnimation {
        score += 10.0
    }

    if meta.SeasonEpisodeCount > 30 {
        score += 8.0
    } else if meta.SeasonEpisodeCount >= 8 && meta.SeasonEpisodeCount <= 15 {
        score -= 2.0
    }

    for _, c := range meta.OriginCountries {
        if c == "JP" {
            score += 3.0
        } else if c == "US" {
            score -= 1.5
        }
    }

    return score
}

// ComputeCandidateScore evaluates the candidate filename's anime probability
func ComputeCandidateScore(filename string) float64 {
    var score float64 = 0.0

    if animeGroupRe.MatchString(filename) {
        score += 6.0
    }
    if westernGroupRe.MatchString(filename) {
        score -= 5.0
    }
    if animeSourceRe.MatchString(filename) {
        score += 5.0
    }
    if westernSourceRe.MatchString(filename) {
        score -= 4.0
    }
    if animeLangRe.MatchString(filename) {
        score += 3.0
    }
    if liveActionMarkerRe.MatchString(filename) {
        score -= 5.0
    }

    return score
}
