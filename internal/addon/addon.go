package addon

import (
    "context"
    "encoding/base64"
    "fmt"
    "math"
    "net/url"
    "sort"
    "strconv"
    "strings"
    "sync"
    "time"

    "github.com/bytedance/sonic"
    "github.com/kiskey/stremio-easynews-go/internal/api"
    "github.com/kiskey/stremio-easynews-go/internal/i18n"
    "github.com/kiskey/stremio-easynews-go/internal/shared"
    "golang.org/x/sync/errgroup"
)

var addonLogger = shared.CreateLogger("Addon", "")

func isMultiWord(title string) bool {
    return len(strings.Fields(title)) > 1
}

type AddonConfig struct {
    Username             string `json:"username"`
    Password             string `json:"password"`
    StrictTitleMatching  string `json:"strictTitleMatching"`
    EnableAltTitles      string `json:"enableAltTitles"`
    AltTitleCountry      string `json:"altTitleCountry"`
    PreferredLanguage    string `json:"preferredLanguage"`
    SortingPreference    string `json:"sortingPreference"`
    ShowQualities        string `json:"showQualities"`
    MaxResultsPerQuality string `json:"maxResultsPerQuality"`
    MaxFileSize          string `json:"maxFileSize"`
    BaseUrl              string `json:"baseUrl"`
    UILanguage           string `json:"uiLanguage"`
}

var defaultConfig = AddonConfig{
    StrictTitleMatching:  "true",
    EnableAltTitles:      "true",
    AltTitleCountry:      "",
    PreferredLanguage:    "",
    SortingPreference:    "quality_first",
    ShowQualities:        "4k,1080p,720p,480p",
    MaxResultsPerQuality: "0",
    MaxFileSize:          "0",
}

func ParseConfig(configStr string) AddonConfig {
    config := defaultConfig

    if configStr == "" {
        return config
    }

    if decodedStr, err := url.QueryUnescape(configStr); err == nil {
        if strings.HasPrefix(decodedStr, "{") && strings.HasSuffix(decodedStr, "}") {
            var jsonConfig AddonConfig
            if err := sonic.Unmarshal([]byte(decodedStr), &jsonConfig); err == nil {
                if jsonConfig.Username != "" {
                    return jsonConfig
                }
            }
        }
    }

    normalized := strings.ReplaceAll(configStr, "|", "&")
    normalized = strings.ReplaceAll(normalized, ";", "&")

    values, err := url.ParseQuery(normalized)
    if err == nil && (values.Get("username") != "" || values.Get("password") != "") {
        if u := values.Get("username"); u != "" { config.Username = u }
        if p := values.Get("password"); p != "" { config.Password = p }
        if s := values.Get("strictTitleMatching"); s != "" { config.StrictTitleMatching = s }
        if e := values.Get("enableAltTitles"); e != "" { config.EnableAltTitles = e }
        if c := values.Get("altTitleCountry"); c != "" { config.AltTitleCountry = c }
        if l := values.Get("preferredLanguage"); l != "" { config.PreferredLanguage = l }
        if o := values.Get("sortingPreference"); o != "" { config.SortingPreference = o }
        if q := values.Get("showQualities"); q != "" { config.ShowQualities = q }
        if m := values.Get("maxResultsPerQuality"); m != "" { config.MaxResultsPerQuality = m }
        if f := values.Get("maxFileSize"); f != "" { config.MaxFileSize = f }
        if b := values.Get("baseUrl"); b != "" { config.BaseUrl = b }
        if ui := values.Get("uiLanguage"); ui != "" { config.UILanguage = ui }
        return config
    }

    decoded, err := base64.URLEncoding.DecodeString(configStr)
    if err == nil {
        var b64Config AddonConfig
        if err := sonic.Unmarshal(decoded, &b64Config); err == nil {
            if b64Config.Username != "" {
                return b64Config
            }
        }
    }

    return config
}

var (
    requestCache           = make(map[string]*cacheItem)
    requestCacheMu         sync.RWMutex
    requestCacheMaxEntries = shared.ParseIntEnv("MAX_CACHE_ENTRIES", 1000)
    emptyResultCacheMaxAge = 10 * 60
    errorCacheMaxAge       = 60
)

type cacheItem struct {
    data      StreamHandlerResult
    expiresAt int64
}

func getFromRequestCache(key string) (StreamHandlerResult, bool) {
    requestCacheMu.RLock()
    item, ok := requestCache[key]
    requestCacheMu.RUnlock()
    if !ok {
        return StreamHandlerResult{}, false
    }
    if time.Now().UnixNano() > item.expiresAt {
        requestCacheMu.Lock()
        delete(requestCache, key)
        requestCacheMu.Unlock()
        return StreamHandlerResult{}, false
    }
    return item.data, true
}

func setRequestCache(key string, data StreamHandlerResult, ttl time.Duration) {
    requestCacheMu.Lock()
    defer requestCacheMu.Unlock()

    requestCache[key] = &cacheItem{
        data:      data,
        expiresAt: time.Now().Add(ttl).UnixNano(),
    }

    if len(requestCache) > requestCacheMaxEntries {
        toDelete := len(requestCache) / 2
        for k := range requestCache {
            if toDelete <= 0 {
                break
            }
            delete(requestCache, k)
            toDelete--
        }
    }
}

type StreamHandlerResult struct {
    Streams     []Stream `json:"streams"`
    CacheMaxAge int      `json:"cacheMaxAge,omitempty"`
}

func authErrorStream(langCode string) StreamHandlerResult {
    t := i18n.GetTranslations(langCode)
    return StreamHandlerResult{
        Streams: []Stream{
            {
                Name:        "Easynews++ Auth Error",
                Description: t.Errors.AuthFailed,
                URL:         "https://example.com/error",
                BehaviorHints: &BehaviorHints{
                    NotWebReady: true,
                },
            },
        },
    }
}

func configErrorStream() StreamHandlerResult {
    return StreamHandlerResult{
        Streams: []Stream{
            {
                Name:        "Easynews++ Config Error",
                Description: "This addon needs to be reconfigured. Open its configuration page and re-install, or set the ADDON_BASE_URL environment variable.",
                URL:         "https://example.com/error",
                BehaviorHints: &BehaviorHints{
                    NotWebReady: true,
                },
            },
        },
    }
}

// Spinoff Identification & Pruning (Series-Scoped)
var spinoffKeywords = []string{"special", "edited", "saga", "recap", "scenes", "interview", "letter", "spinoff", "movie", "film", "ova", "ona", "oad", "re-edited", "collaboration", "crossover"}

func isSpinoff(title, primaryName string) bool {
    lowerTitle := strings.ToLower(title)
    lowerPrimary := strings.ToLower(primaryName)
    
    // Edge Case Fix: Only trigger keyword pruning if the keyword is newly introduced 
    // in the alt title and is NOT part of the primary title's core identity.
    for _, kw := range spinoffKeywords {
        if strings.Contains(lowerTitle, kw) && !strings.Contains(lowerPrimary, kw) {
            return true
        }
    }
    
    if len(primaryName) > 0 {
        if float64(len(title))/float64(len(primaryName)) > 2.5 {
            return true
        }
        if strings.Contains(lowerTitle, lowerPrimary) && len(strings.Fields(title)) > len(strings.Fields(primaryName)) {
            return true
        }
    }
    if len(strings.Fields(title)) > 4 {
        return true
    }
    return false
}

// Helper to filter alternative titles for series-Scoped spinoffs
func filterAlternativeTitles(contentType string, name string, alternativeNames []string) []string {
    var filtered []string
    for _, alt := range alternativeNames {
        if contentType == "series" && isSpinoff(alt, name) {
            continue
        }
        
        isDup := false
        if SanitizeTitle(alt) == SanitizeTitle(name) {
            isDup = true
        }
        for _, f := range filtered {
            if SanitizeTitle(f) == SanitizeTitle(alt) {
                isDup = true
                break
            }
        }
        
        if !isDup {
            filtered = append(filtered, alt)
        }
    }
    
    // Capping at 2 alternative titles to prevent Solr query explosion (1 primary + 2 alts)
    if len(filtered) > 2 {
        filtered = filtered[:2]
    }
    return filtered
}

func StreamHandler(contentType, id string, config AddonConfig) (StreamHandlerResult, error) {
    if !strings.HasPrefix(id, "tt") {
        return StreamHandlerResult{Streams: []Stream{}}, nil
    }

    cacheKey := fmt.Sprintf("%s:v25:user=%s:strict=%s:lang=%s:sort=%s:qualities=%s:maxPerQuality=%s:maxSize=%s:enableAlt=%s:altCountry=%s",
        id,
        config.Username,
        config.StrictTitleMatching,
        config.PreferredLanguage,
        config.SortingPreference,
        config.ShowQualities,
        config.MaxResultsPerQuality,
        config.MaxFileSize,
        config.EnableAltTitles,
        config.AltTitleCountry,
    )

    if cached, ok := getFromRequestCache(cacheKey); ok {
        addonLogger.Info("Request Cache HIT for key ID %s (returning %d streams)", id, len(cached.Streams))
        return cached, nil
    }

    useStrictMatching := config.StrictTitleMatching == "on" || config.StrictTitleMatching == "true" || config.StrictTitleMatching == ""
    enableAltTitles := config.EnableAltTitles == "true" || config.EnableAltTitles == "on" || config.EnableAltTitles == ""
    preferredLang := config.PreferredLanguage
    sortingPreference := config.SortingPreference
    if sortingPreference == "" {
        sortingPreference = defaultConfig.SortingPreference
    }

    qualityFilters := []string{"4k", "1080p", "720p", "480p"}
    if config.ShowQualities != "" {
        parts := strings.Split(config.ShowQualities, ",")
        qualityFilters = make([]string, 0, len(parts))
        for _, p := range parts {
            if trimmed := strings.TrimSpace(p); trimmed != "" {
                qualityFilters = append(qualityFilters, strings.ToLower(trimmed))
            }
        }
    }

    maxResultsPerQualityVal := 0
    if v, err := strconv.Atoi(config.MaxResultsPerQuality); err == nil && v > 0 {
        maxResultsPerQualityVal = v
    }

    maxFileSizeGB := 0.0
    if v, err := strconv.ParseFloat(config.MaxFileSize, 64); err == nil && v > 0 {
        maxFileSizeGB = v
    }

    easynewsAPI, err := api.NewEasynewsAPI(config.Username, config.Password)
    if err != nil {
        addonLogger.Error("EasynewsAPI instantiation failed: %v", err)
        return authErrorStream(config.UILanguage), nil
    }

    meta, err := PublicMetaProvider(id, contentType, preferredLang, enableAltTitles, config.AltTitleCountry)
    if err != nil {
        addonLogger.Error("Metadata lookup failed for ID %s (type=%s): %v", id, contentType, err)
        return StreamHandlerResult{Streams: []Stream{}, CacheMaxAge: errorCacheMaxAge}, nil
    }

    addonLogger.Info("Initiating search for '%s' (type: %s, strict matching: %v, preferred lang: '%s')", meta.Name, contentType, useStrictMatching, preferredLang)

    // Filter spinoffs and select clean target alternative titles
    filteredAlts := filterAlternativeTitles(contentType, meta.Name, meta.AlternativeNames)

    // Construct full titles mapping list for matching, including abbreviations
    allTitles := append([]string{meta.Name}, filteredAlts...)
    for _, tv := range allTitles {
        expanded := ExpandAbbreviations(tv)
        if expanded != tv {
            isDup := false
            for _, existing := range allTitles {
                if SanitizeTitle(existing) == SanitizeTitle(expanded) {
                    isDup = true
                    break
                }
            }
            if !isDup {
                allTitles = append(allTitles, expanded)
            }
        }
    }

    var primaryQueries []string
    var primaryLegacyQueries []string
    var primaryDateQueries []string
    var altQueries []string
    var altLegacyQueries []string
    var altDateQueries []string
    var broadQueries []string

    // Generate isolated, dedicated formats to ensure standard S/E is executed first
    if contentType == "movie" {
        primaryQueries = BuildOptimizedGroupedQueries(contentType, meta, []string{meta.Name}, "standard")
        if len(filteredAlts) > 0 {
            altQueries = BuildOptimizedGroupedQueries(contentType, meta, filteredAlts, "standard")
        }
        
        mNoYear := meta
        mNoYear.Year = 0
        broadQueries = BuildOptimizedGroupedQueries(contentType, mNoYear, append([]string{meta.Name}, filteredAlts...), "standard")
    } else if contentType == "series" {
        // Compile standard and fallback segments individually
        primaryQueries = BuildOptimizedGroupedQueries(contentType, meta, []string{meta.Name}, "standard")
        primaryLegacyQueries = BuildOptimizedGroupedQueries(contentType, meta, []string{meta.Name}, "legacy")
        if meta.EpisodeAirDate != "" {
            primaryDateQueries = BuildOptimizedGroupedQueries(contentType, meta, []string{meta.Name}, "date")
        }

        if len(filteredAlts) > 0 {
            altQueries = BuildOptimizedGroupedQueries(contentType, meta, filteredAlts, "standard")
            altLegacyQueries = BuildOptimizedGroupedQueries(contentType, meta, filteredAlts, "legacy")
            if meta.EpisodeAirDate != "" {
                altDateQueries = BuildOptimizedGroupedQueries(contentType, meta, filteredAlts, "date")
            }
        }

        mNoYear := meta
        mNoYear.Year = 0
        mNoYear.Season = ""
        mNoYear.Episode = ""
        mNoYear.EpisodeAirDate = ""
        broadQueries = BuildOptimizedGroupedQueries(contentType, mNoYear, append([]string{meta.Name}, filteredAlts...), "standard")
    }

    searchConcurrency := shared.ParseIntEnv("SEARCH_CONCURRENCY", 5)
    if searchConcurrency < 1 {
        searchConcurrency = 1
    }
    totalMaxResults := shared.ParseIntEnv("TOTAL_MAX_RESULTS", 500)

    // Anime & Legacy SD File Size Protection
    minValidSize := int64(80 * 1024 * 1024) // 80MB for series
    if contentType == "movie" {
        minValidSize = int64(300 * 1024 * 1024) // 300MB for movies
    }
    isValidSize := func(file api.FileData) bool {
        return file.RawSize >= minValidSize
    }

    type searchResult struct {
        query  string
        result api.EasynewsSearchResponse
    }

    var allSearchResults []searchResult
    var resultsMu sync.Mutex
    totalFoundResults := 0
    validFileCount := 0

    runSearchPhase := func(queries []string) error {
        ctx, cancel := context.WithCancel(context.Background())
        defer cancel()

        var g errgroup.Group
        sem := make(chan struct{}, searchConcurrency)

        for _, query := range queries {
            if validFileCount >= 15 {
                break
            }

            query := query
            sem <- struct{}{}

            g.Go(func() error {
                defer func() {
                    if r := recover(); r != nil {
                        addonLogger.Error("Recovered from internal query execution panic for '%s': %v", query, r)
                    }
                    <-sem
                }()

                opts := api.SearchOptions{Query: query}
                res, err := easynewsAPI.SearchAll(ctx, opts)
                if err != nil {
                    if ctx.Err() != nil {
                        return nil 
                    }
                    addonLogger.Error("Easynews Solr search failed for query '%s': %v", query, err)
                    if IsAuthError(err) {
                        cancel()
                        return err
                    }
                    return nil
                }

                if len(res.Data) > 0 {
                    resultsMu.Lock()
                    allSearchResults = append(allSearchResults, searchResult{query: query, result: res})
                    
                    for _, f := range res.Data {
                        if isValidSize(f) {
                            validFileCount++
                        }
                    }

                    uniqueHashes := make(map[string]struct{})
                    for _, sr := range allSearchResults {
                        for _, f := range sr.result.Data {
                            uniqueHashes[f.GetHash()] = struct{}{}
                        }
                    }
                    totalFoundResults = len(uniqueHashes)
                    resultsMu.Unlock()

                    if validFileCount >= 15 {
                        addonLogger.Info("Early exit triggered: Found %d valid results, cancelling remaining searches.", validFileCount)
                        cancel()
                    }
                }
                return nil
            })
        }

        if err := g.Wait(); err != nil {
            if ctx.Err() != nil {
                return nil 
            }
            return err
        }
        return nil
    }

    // 1. Primary Phase (Standard S/E Fast Path - SXXEXX only): Completes instantly in ~200ms
    if len(primaryQueries) > 0 {
        if err := runSearchPhase(primaryQueries); err != nil {
            addonLogger.Error("Easynews API search failed: %v", err)
            return authErrorStream(config.UILanguage), nil
        }
    }

    // 1.1. Primary Legacy Fallback Phase (XXxXX): ONLY if standard S/E was sparse (< 10)
    if totalFoundResults < 10 && len(primaryLegacyQueries) > 0 {
        addonLogger.Info("Primary standard S/E sparse (%d). Running primary legacy queries...", totalFoundResults)
        _ = runSearchPhase(primaryLegacyQueries)
    }

    // 1.5. Primary Date Fallback Phase: ONLY if primary standard/legacy was sparse and we have a daily date
    if totalFoundResults < 10 && len(primaryDateQueries) > 0 {
        addonLogger.Info("Primary standard/legacy sparse (%d). Running primary date-based queries...", totalFoundResults)
        _ = runSearchPhase(primaryDateQueries)
    }

    // 2. Sequential Cascade Gating: Alternative Titles Standard S/E (Only if results still sparse < 10)
    if totalFoundResults < 10 && len(altQueries) > 0 {
        addonLogger.Info("Sparse results (%d) in primary phase. Running alternative standard queries...", totalFoundResults)
        _ = runSearchPhase(altQueries)
    }

    // 2.1. Alternative Legacy Fallback Phase: ONLY if results are still sparse (< 10)
    if totalFoundResults < 10 && len(altLegacyQueries) > 0 {
        addonLogger.Info("Alternative standard sparse (%d). Running alternative legacy queries...", totalFoundResults)
        _ = runSearchPhase(altLegacyQueries)
    }

    // 2.5. Alternative Date Fallback Phase: ONLY if results are still sparse (< 10)
    if totalFoundResults < 10 && len(altDateQueries) > 0 {
        addonLogger.Info("Primary & Alt S/E sparse (%d). Running alternative date queries...", totalFoundResults)
        _ = runSearchPhase(altDateQueries)
    }

    // 3. Lazy Gate Fallback: Broad searches (Only if results are still sparse < 10)
    if totalFoundResults < 10 && len(broadQueries) > 0 {
        addonLogger.Info("Sparse results (%d) found. Running broad fallbacks...", totalFoundResults)
        _ = runSearchPhase(broadQueries)
    }

    targetSeason, _ := strconv.Atoi(meta.Season)
    targetEpisode, _ := strconv.Atoi(meta.Episode)

    // 4. Absolute episode fallback for series (Only if results are extremely low < 5)
    if contentType == "series" && totalFoundResults < 5 && targetEpisode > 0 {
        addonLogger.Info("Low results (%d) for series '%s'. Running absolute episode fallback...", totalFoundResults, meta.Name)
        var absFallbackQueries []string
        searchTitles := append([]string{meta.Name}, filteredAlts...)
        for _, titleVariant := range searchTitles {
            if strings.TrimSpace(titleVariant) == "" || !IsLatinString(titleVariant) {
                continue
            }
            absFallbackQueries = append(absFallbackQueries, fmt.Sprintf("%s %d !sample !trailer !passwd !password !preview", titleVariant, targetEpisode))
        }
        if len(absFallbackQueries) > 0 {
            _ = runSearchPhase(absFallbackQueries)
        }
    }

    if len(allSearchResults) == 0 {
        addonLogger.Info("Search complete: zero results returned from Easynews Solr indices")
        result := StreamHandlerResult{
            Streams:     []Stream{},
            CacheMaxAge: emptyResultCacheMaxAge,
        }
        setRequestCache(cacheKey, result, time.Duration(emptyResultCacheMaxAge)*time.Second)
        return result, nil
    }

    processedHashes := make(map[string]struct{})
    var streams []Stream

    totalFilesSeen := 0
    rejectedSample := 0
    rejectedDuplicate := 0
    rejectedTitle := 0

    // Calculate the overall absolute episode number of the request (Fix B)
    targetAbsoluteEpisode := 0
    if contentType == "series" && targetSeason > 1 && len(meta.SeasonEpisodeCounts) > 0 {
        totalPrevEpisodes := 0
        for s := 1; s < targetSeason; s++ {
            totalPrevEpisodes += meta.SeasonEpisodeCounts[s]
        }
        if totalPrevEpisodes > 0 {
            targetAbsoluteEpisode = totalPrevEpisodes + targetEpisode
        }
    }

    for _, sr := range allSearchResults {
        if len(streams) >= totalMaxResults {
            break
        }
        for _, file := range sr.result.Data {
            if len(streams) >= totalMaxResults {
                break
            }

            title := GetPostTitle(file)
            fileHash := file.GetHash()
            totalFilesSeen++

            if IsBadVideo(file) {
                rejectedSample++
                continue
            }
            if _, dup := processedHashes[fileHash]; dup {
                rejectedDuplicate++
                continue
            }
            processedHashes[fileHash] = struct{}{}

            // Tier 1: Hard Deterministic Temporal Shield (0-allocation, executes in 1µs)
            if isNewerShowDisqualified(file.Ts, meta.Year) {
                rejectedTitle++
                continue
            }

            // Tier 2: Probabilistic Bayesian LLR Gated Shield
            if contentType == "series" {
                targetPrior := ClassifyTargetPrior(meta)
                
                // Only evaluate LLR patterns if the target classification is highly confident
                if math.Abs(targetPrior) >= 3.0 {
                    candScore := ComputeCandidateScore(title)
                    
                    if targetPrior > 3.0 && candScore < -3.0 {
                        rejectedTitle++
                        continue
                    }
                    if targetPrior < -3.0 && candScore > 4.0 {
                        rejectedTitle++
                        continue
                    }
                }
            }

            parsedInfo := RobustParseInfo(title, 0)

            // Tier 3: Double-Sided Title Isolation (Disqualifies Castle Rock episode "Severance")
            if len(parsedInfo.Title) > 0 {
                sanitizedParsed := SanitizeTitle(parsedInfo.Title)
                anyMatch := false
                for _, tv := range allTitles {
                    sanitizedMeta := SanitizeTitle(tv)
                    if strings.Contains(sanitizedParsed, sanitizedMeta) || strings.Contains(sanitizedMeta, sanitizedParsed) {
                        anyMatch = true
                        break
                    }
                }
                if !anyMatch {
                    rejectedTitle++
                    continue
                }
            }

            if contentType == "series" {
                matched := false
                // Local matching uses the full allTitles array (untruncated)
                for _, tv := range allTitles {
                    if MatchesTitle(title, tv, useStrictMatching) {
                        matched = true
                        break
                    }
                }
                if !matched {
                    rejectedTitle++
                    continue
                }

                // Season 1 Year Discrepancy Guardrail (highly robust reboot-proofing)
                if targetSeason == 1 && meta.Year > 0 && parsedInfo.Year > 0 {
                    diff := parsedInfo.Year - meta.Year
                    if diff < 0 {
                        diff = -diff
                    }
                    if diff > 1 {
                        rejectedTitle++
                        continue
                    }
                }

                if targetEpisode > 0 && isExtraOrSpecial(title) {
                    rejectedTitle++
                    continue
                }

                if targetSeason > 0 && targetEpisode > 0 {
                    isPack, _, _, hasRange := ParsePackOrRange(title, targetEpisode)

                    episodeMatches := parsedInfo.Episode == targetEpisode || (targetAbsoluteEpisode > 0 && parsedInfo.Episode == targetAbsoluteEpisode)
                    if !episodeMatches && len(parsedInfo.Episodes) > 1 {
                        for _, ep := range parsedInfo.Episodes {
                            if ep == targetEpisode || (targetAbsoluteEpisode > 0 && ep == targetAbsoluteEpisode) {
                                episodeMatches = true
                                break
                            }
                        }
                    }

                    if (parsedInfo.Season > 0 && parsedInfo.Season != targetSeason) ||
                        (parsedInfo.Episode > 0 && !episodeMatches && !hasRange && !isPack && !parsedInfo.IsPack) {
                        
                        if parsedInfo.Season == 0 && parsedInfo.Episode > 0 {
                            // Skip rejection
                        } else {
                            rejectedTitle++
                            continue
                        }
                    }

                    if parsedInfo.Season == 0 && parsedInfo.Episode == 0 && parsedInfo.Date == "" && !isPack && !parsedInfo.IsPack {
                        rejectedTitle++
                        continue
                    }
                }
            }

            if contentType == "movie" {
                matched := false
                // Local matching uses the full allTitles array (untruncated)
                for _, tv := range allTitles {
                    if MatchesTitle(title, tv, useStrictMatching) {
                        matched = true
                        break
                    }
                }
                if !matched {
                    rejectedTitle++
                    continue
                }

                if parsedInfo.Season > 0 || parsedInfo.Episode > 0 || parsedInfo.IsPack {
                    rejectedTitle++
                    continue
                }

                if meta.Year > 0 && parsedInfo.Year > 0 {
                    diff := parsedInfo.Year - meta.Year
                    if diff < 0 {
                        diff = -diff
                    }
                    if diff > 1 {
                        rejectedTitle++
                        continue
                    }
                }
            }

            streamPath := CreateStreamPath(file)
            streamUrl, err := CreateStreamUrl(
                sr.result.DownURL, sr.result.DlFarm, sr.result.DlPort,
                config.Username, config.Password, streamPath, config.BaseUrl,
            )
            if err != nil {
                if _, isMissing := err.(*MissingBaseUrlError); isMissing {
                    addonLogger.Error("Failed to map stream: missing ADDON_BASE_URL context")
                    return configErrorStream(), nil
                }
                continue
            }

            stream := MapStream(
                GetDuration(file),
                GetSize(file),
                file.Fullres,
                title,
                GetFileExtension(file),
                file.RawSize,
                streamUrl,
                file,
                preferredLang,
                parsedInfo,
            )
            streams = append(streams, stream)
        }
    }

    addonLogger.Info("Search complete: totalFilesSeen=%d matchingCount=%d (rejected: sample/quality=%d, duplicate=%d, titleMismatch=%d)",
        totalFilesSeen, len(streams), rejectedSample, rejectedDuplicate, rejectedTitle)

    calculateTotalScore := func(a *SortMeta) int {
        if a == nil {
            return 0
        }
        return a.QualityScore*1000 + a.SourceScore*100 + a.HDRScore*10 + a.CodecScore
    }

    if len(streams) > 0 {
        if sortingPreference == "language_first" && preferredLang != "" {
            var preferredLangStreams, otherStreams []Stream
            for _, s := range streams {
                if s.SortMeta != nil && s.SortMeta.HasPreferredLang {
                    preferredLangStreams = append(preferredLangStreams, s)
                } else {
                    otherStreams = append(otherStreams, s)
                }
            }

            sortByQualityAndSize := func(a, b Stream) bool {
                if a.SortMeta == nil && b.SortMeta == nil {
                    return false
                }
                if a.SortMeta == nil {
                    return false
                }
                if b.SortMeta == nil {
                    return true
                }
                
                aTotal := calculateTotalScore(a.SortMeta)
                bTotal := calculateTotalScore(b.SortMeta)
                if aTotal != bTotal {
                    return aTotal > bTotal
                }
                
                aScore := 0
                bScore := 0
                if a.SortMeta.IsProper { aScore = 2 }
                if a.SortMeta.IsRepack { aScore = 1 }
                if b.SortMeta.IsProper { bScore = 2 }
                if b.SortMeta.IsRepack { bScore = 1 }
                if aScore != bScore {
                    return aScore > bScore
                }
                
                return CompareSizeMeta(a.SortMeta, b.SortMeta) < 0
            }

            sort.Slice(preferredLangStreams, func(i, j int) bool {
                return sortByQualityAndSize(preferredLangStreams[i], preferredLangStreams[j])
            })
            sort.Slice(otherStreams, func(i, j int) bool {
                return sortByQualityAndSize(otherStreams[i], otherStreams[j])
            })

            streams = append(preferredLangStreams, otherStreams...)
        } else {
            sort.Slice(streams, func(i, j int) bool {
                a, b := streams[i].SortMeta, streams[j].SortMeta
                if a == nil && b == nil {
                    return false
                }
                if a == nil {
                    return false
                }
                if b == nil {
                    return true
                }
                switch sortingPreference {
                case "size_first":
                    sizeCompare := CompareSizeMeta(a, b)
                    if sizeCompare != 0 {
                        return sizeCompare < 0
                    }
                    if calculateTotalScore(a) != calculateTotalScore(b) {
                        return calculateTotalScore(a) > calculateTotalScore(b)
                    }
                    if a.HasPreferredLang != b.HasPreferredLang {
                        return a.HasPreferredLang
                    }
                    return false
                case "date_first":
                    if a.DateMs != b.DateMs {
                        return a.DateMs > b.DateMs
                    }
                    if calculateTotalScore(a) != calculateTotalScore(b) {
                        return calculateTotalScore(a) > calculateTotalScore(b)
                    }
                    if a.HasPreferredLang != b.HasPreferredLang {
                        return a.HasPreferredLang
                    }
                    return CompareSizeMeta(a, b) < 0
                case "lang_first", "language_first":
                    if a.HasPreferredLang != b.HasPreferredLang {
                        return a.HasPreferredLang
                    }
                    if calculateTotalScore(a) != calculateTotalScore(b) {
                        return calculateTotalScore(a) > calculateTotalScore(b)
                    }
                    return CompareSizeMeta(a, b) < 0
                default: // quality_first
                    if calculateTotalScore(a) != calculateTotalScore(b) {
                        return calculateTotalScore(a) > calculateTotalScore(b)
                    }
                    
                    aScore := 0
                    bScore := 0
                    if a.IsProper { aScore = 2 }
                    if a.IsRepack { aScore = 1 }
                    if b.IsProper { bScore = 2 }
                    if b.IsRepack { bScore = 1 }
                    if aScore != bScore {
                        return aScore > bScore
                    }
                    
                    if a.HasPreferredLang != b.HasPreferredLang {
                        return a.HasPreferredLang
                    }
                    return CompareSizeMeta(a, b) < 0
                }
            })
        }
    }

    if len(streams) > 0 {
        defaultQualitySet := []string{"4k", "1080p", "720p", "480p"}
        isCustomFilter := !(len(qualityFilters) == len(defaultQualitySet) &&
            hasAll(defaultQualitySet, qualityFilters))

        if isCustomFilter {
            qualityMap := map[string][]string{
                "4k":    {"4K", "UHD", "2160p"},
                "1080p": {"1080p"},
                "720p":  {"720p"},
                "480p":  {"480p", "SD"},
            }
            var allowedTerms []string
            for _, q := range qualityFilters {
                if terms, ok := qualityMap[q]; ok {
                    allowedTerms = append(allowedTerms, terms...)
                }
            }
            if len(allowedTerms) > 0 {
                filtered := make([]Stream, 0, len(streams))
                for _, s := range streams {
                    qualityLine := ""
                    parts := strings.Split(s.Name, "\n")
                    if len(parts) > 1 {
                        qualityLine = parts[1]
                    }
                    for _, term := range allowedTerms {
                        if strings.Contains(qualityLine, term) {
                            filtered = append(filtered, s)
                            break
                        }
                    }
                }
                if len(filtered) > 0 {
                    streams = filtered
                }
            }
        }

        if maxFileSizeGB > 0 {
            filtered := make([]Stream, 0, len(streams))
            for _, s := range streams {
                videoSize := int64(0)
                if s.BehaviorHints != nil {
                    videoSize = s.BehaviorHints.VideoSize
                }

                if videoSize > 0 {
                    sizeGB := float64(videoSize) / (1024 * 1024 * 1024)
                    if sizeGB <= maxFileSizeGB {
                        filtered = append(filtered, s)
                    }
                } else {
                    lines := strings.Split(s.Description, "\n")
                    for _, line := range lines {
                        if strings.Contains(line, "📦") {
                            beforeDate := strings.Split(line, "📅")[0]
                            beforeDate = strings.TrimSpace(beforeDate)
                            beforeDate = strings.Replace(beforeDate, "📦", "", 1)
                            beforeDate = strings.TrimSpace(beforeDate)
                            if strings.Contains(beforeDate, "GB") {
                                v, _ := strconv.ParseFloat(floatValueRe.FindString(beforeDate), 64)
                                if v <= maxFileSizeGB {
                                    filtered = append(filtered, s)
                                }
                            } else if strings.Contains(beforeDate, "MB") {
                                v, _ := strconv.ParseFloat(floatValueRe.FindString(beforeDate), 64)
                                if v/1024 <= maxFileSizeGB {
                                    filtered = append(filtered, s)
                                }
                            } else {
                                filtered = append(filtered, s)
                            }
                            break
                        }
                    }
                }
            }
            if len(filtered) > 0 {
                streams = filtered
            }
        }

        if maxResultsPerQualityVal > 0 {
            streamsByQuality := make(map[string][]Stream)
            for _, s := range streams {
                qualityLine := ""
                parts := strings.Split(s.Name, "\n")
                if len(parts) > 1 {
                    qualityLine = parts[1]
                }
                category := "other"
                if strings.Contains(qualityLine, "4K") || strings.Contains(qualityLine, "UHD") || strings.Contains(qualityLine, "2160p") {
                    category = "4k"
                } else if strings.Contains(qualityLine, "1080p") {
                    category = "1080p"
                } else if strings.Contains(qualityLine, "720p") {
                    category = "720p"
                } else if strings.Contains(qualityLine, "480p") || strings.Contains(qualityLine, "SD") {
                    category = "480p"
                }
                streamsByQuality[category] = append(streamsByQuality[category], s)
            }
            var limited []Stream
            orderKeys := []string{"4k", "1080p", "720p", "480p", "other"}
            for _, k := range orderKeys {
                qStreams := streamsByQuality[k]
                if len(qStreams) > maxResultsPerQualityVal {
                    qStreams = qStreams[:maxResultsPerQualityVal]
                }
                limited = append(limited, qStreams...)
            }
            if len(limited) > 0 {
                streams = limited
            }
        }
    }

    cacheMaxAge := getCacheMaxAge(len(streams))

    result := StreamHandlerResult{
        Streams:     streams,
        CacheMaxAge: cacheMaxAge,
    }

    setRequestCache(cacheKey, result, time.Duration(cacheMaxAge)*time.Second)
    return result, nil
}

func getCacheMaxAge(itemCount int) int {
    computed := int(float64(minVal(itemCount, 10)) / 10.0 * 3600 * 24 * 7)
    if computed < emptyResultCacheMaxAge {
        return emptyResultCacheMaxAge
    }
    return computed
}

func minVal(a, b int) int {
    if a < b {
        return a
    }
    return b
}

func hasAll(haystack, needles []string) bool {
    set := make(map[string]bool)
    for _, h := range haystack {
        set[h] = true
    }
    for _, n := range needles {
        if !set[n] {
            return false
        }
    }
    return true
}

func MapStream(duration, size, fullResolution, title, fileExtension string, videoSize int64, url string, file api.FileData, preferredLang string, parsedInfo *ParseResult) Stream {
    quality := GetQuality(title, fullResolution)
    badges := FormatBadges(title)

    publishDate := ""
    if file.Ts > 0 {
        publishDate = GetPublishDate(file.Ts)
    }

    languageInfo := "🌐 Unknown"
    if file.Alangs != nil && len(file.Alangs) > 0 {
        star := ""
        if preferredLang != "" && contains(file.Alangs, preferredLang) {
            star = " ⭐"
        }
        languageInfo = fmt.Sprintf("🌐 %s%s", strings.Join(file.Alangs, ", "), star)
    }

    sizeUnit, sizeValue := ParseSizeForSort(size)
    dateMs := int64(0)
    if file.Five != "" {
        if t, err := time.Parse(time.RFC3339, file.Five); err == nil {
            dateMs = t.UnixMilli()
        } else if t, err := time.Parse("2006-01-02 15:04:05", file.Five); err == nil {
            dateMs = t.UnixMilli()
        } else if t, err := time.Parse("01-02-2006 15:04:05", file.Five); err == nil {
            dateMs = t.UnixMilli()
        }
    }
    hasPreferredLang := preferredLang != "" && file.Alangs != nil && contains(file.Alangs, preferredLang)

    sourceScore := 0
    if strings.Contains(badges, "Remux") {
        sourceScore = 8
    } else if strings.Contains(badges, "BluRay") {
        sourceScore = 7
    } else if strings.Contains(badges, "WEB-DL") {
        sourceScore = 6
    } else if strings.Contains(badges, "WEBRip") {
        sourceScore = 5
    } else if strings.Contains(badges, "HDTV") {
        sourceScore = 5
    } else if strings.Contains(badges, "HDRip") {
        sourceScore = 4
    } else if strings.Contains(badges, "DVDRip") {
        sourceScore = 3
    }

    hdrScore := 0
    if strings.Contains(badges, "DV") {
        hdrScore = 4
    } else if strings.Contains(badges, "HDR10+") {
        hdrScore = 3
    } else if strings.Contains(badges, "HDR10") {
        hdrScore = 2
    } else if strings.Contains(badges, "HDR") {
        hdrScore = 1
    }

    codecScore := 0
    if strings.Contains(badges, "AV1") {
        codecScore = 3
    } else if strings.Contains(badges, "H265 HEVC") {
        codecScore = 2
    } else if strings.Contains(badges, "H264 AVC") {
        codecScore = 1
    }

    sortMeta := &SortMeta{
        QualityScore:     QualityScoreFromLabel(quality),
        SourceScore:      sourceScore,
        HDRScore:         hdrScore,
        CodecScore:       codecScore,
        SizeUnit:         sizeUnit,
        SizeValue:        sizeValue,
        DateMs:           dateMs,
        HasPreferredLang: hasPreferredLang,
        IsProper:         parsedInfo.IsProper,
        IsRepack:         parsedInfo.IsRepack,
        Edition:          parsedInfo.Edition,
    }

    bingeLang := "unknown"
    if file.Alangs != nil && len(file.Alangs) > 0 {
        langSet := make(map[string]struct{})
        for _, l := range file.Alangs {
            langSet[strings.ToLower(l)] = struct{}{}
        }
        var sortedLangs []string
        for l := range langSet {
            sortedLangs = append(sortedLangs, l)
        }
        sort.Strings(sortedLangs)
        bingeLang = strings.Join(sortedLangs, ",")
    }

    releaseGroup := ""
    if parsedInfo != nil && parsedInfo.ReleaseGroup != "" {
        releaseGroup = parsedInfo.ReleaseGroup
    }
    var bingeGroup string
    if releaseGroup != "" {
        bingeGroup = fmt.Sprintf("easynews-plus-plus|%s|%s|%s|%s", quality, bingeLang, fileExtension, releaseGroup)
    } else {
        hashSuffix := file.GetHash()
        if len(hashSuffix) > 8 {
            hashSuffix = hashSuffix[:8]
        }
        bingeGroup = fmt.Sprintf("easynews-plus-plus|%s|%s|%s|unique:%s", quality, bingeLang, fileExtension, hashSuffix)
    }

    name := "Easynews++"
    if quality != "" {
        name += "\n" + quality
    }

    description := fmt.Sprintf("%s%s\n%s\n🕛 %s\n📦 %s %s\n%s",
        title, fileExtension,
        badges,
        coalesce(duration, "unknown duration"),
        coalesce(size, "unknown size"),
        publishDate,
        languageInfo,
    )

    bh := &BehaviorHints{
        NotWebReady: true,
        Filename:    title + fileExtension,
        BingeGroup:  bingeGroup,
    }
    if videoSize > 0 {
        bh.VideoSize = videoSize
    }

    return Stream{
        Name:          name,
        URL:           url,
        Description:   description,
        BehaviorHints: bh,
        SortMeta:      sortMeta,
    }
}

func coalesce(a, b string) string {
    if a == "" {
        return b
    }
    return a
}

func contains(slice []string, item string) bool {
    for _, s := range slice {
        if s == item {
            return true
        }
    }
    return false
}
