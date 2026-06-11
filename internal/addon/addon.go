package addon

import (
	"encoding/base64"
	"fmt"
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

// Helper to determine if a title contains multiple words (preventing stop-word/index explosions)
func isMultiWord(title string) bool {
	return len(strings.Fields(title)) > 1
}

// ---------------------------------------------------------------------------
// Addon Configuration
// ---------------------------------------------------------------------------

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

// ParseConfig decodes a base64-encoded configuration payload or extracts
// query fields from a URL query-styled parameters string.
func ParseConfig(configStr string) AddonConfig {
	config := defaultConfig

	if configStr == "" {
		return config
	}

	// 1. Try URL-decoded JSON parsing (to support original Node.js addon URLs)
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

	// 2. Try URL query format (replacing common separators like | with &)
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

	// 3. Try URL-safe Base64 JSON parsing
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

// ---------------------------------------------------------------------------
// In-Memory Request Cache (Optimized with pointers to bypass copy overhead)
// ---------------------------------------------------------------------------

var (
	requestCache           = make(map[string]*cacheItem)
	requestCacheMu         sync.RWMutex
	requestCacheMaxEntries = shared.ParseIntEnv("MAX_CACHE_ENTRIES", 1000)
	emptyResultCacheMaxAge = 10 * 60 // 10 minutes in seconds
	errorCacheMaxAge       = 60      // 1 minute in seconds
)

type cacheItem struct {
	data      StreamHandlerResult
	expiresAt int64 // UnixNano
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

// ---------------------------------------------------------------------------
// Diagnostic Error Payload Stream Results
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Main Stream Resolution Pipeline Entry
// ---------------------------------------------------------------------------

func StreamHandler(contentType, id string, config AddonConfig) (StreamHandlerResult, error) {
	if !strings.HasPrefix(id, "tt") {
		return StreamHandlerResult{Streams: []Stream{}}, nil
	}

	cacheKey := fmt.Sprintf("%s:v3:user=%s:strict=%s:lang=%s:sort=%s:qualities=%s:maxPerQuality=%s:maxSize=%s:enableAlt=%s:altCountry=%s",
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

	// Dynamic alternative titles are resolved dynamically using the TMDB API
	allTitles := []string{meta.Name}
	if meta.AlternativeNames != nil {
		for _, alt := range meta.AlternativeNames {
			found := false
			for _, t := range allTitles {
				// Sanitized Deduplication (Prevents redundant parallel searches for identically tokenized names)
				if SanitizeTitle(t) == SanitizeTitle(alt) {
					found = true
					break
				}
			}
			if !found {
				allTitles = append(allTitles, alt)
			}
		}
	}

	buildQueries := func(withYear bool) []string {
		var queries []string
		for _, titleVariant := range allTitles {
			if strings.TrimSpace(titleVariant) == "" {
				continue
			}
			m := meta
			m.Name = titleVariant
			if !withYear {
				m.Year = 0
			}
			queries = append(queries, BuildSearchQuery(contentType, m))
		}
		return queries
	}

	var primaryQueries []string
	var fallbackQueries []string

	if contentType == "movie" {
		if meta.Year > 0 {
			if isMultiWord(meta.Name) {
				// Step 1: Specific Year Search First (Low-latency, highly targeted) [1]
				primaryQueries = buildQueries(true)
				// Step 2: Broad Title-Only Fallback (Safe from stop-word explosions since it's multi-word) [1]
				fallbackQueries = buildQueries(false)
			} else {
				// Single-word movie: ONLY search with year to prevent Solr index explosion! [1]
				primaryQueries = buildQueries(true)
			}
		} else {
			// No year known: must search without year
			primaryQueries = buildQueries(false)
		}
	} else if contentType == "series" {
		// Series search (uses standard non-year SxxExx)
		primaryQueries = buildQueries(false)
	}

	searchConcurrency := shared.ParseIntEnv("SEARCH_CONCURRENCY", 5)
	if searchConcurrency < 1 {
		searchConcurrency = 1
	}
	totalMaxResults := shared.ParseIntEnv("TOTAL_MAX_RESULTS", 500)

	type searchResult struct {
		query  string
		result api.EasynewsSearchResponse
	}

	var allSearchResults []searchResult
	var resultsMu sync.Mutex
	totalFoundResults := 0

	// Continuous Queue-Based Semaphore Pipeline (Optimized with panic-safeguard recover blocks)
	runSearchPhase := func(queries []string) error {
		var g errgroup.Group
		sem := make(chan struct{}, searchConcurrency)

		for _, query := range queries {
			if totalFoundResults >= totalMaxResults {
				break
			}

			query := query
			sem <- struct{}{} // Block-acquire

			g.Go(func() error {
				defer func() {
					if r := recover(); r != nil {
						addonLogger.Error("Recovered from internal query execution panic for '%s': %v", query, r)
					}
					<-sem // Always release
				}()

				opts := api.SearchOptions{Query: query}
				res, err := easynewsAPI.SearchAll(opts)
				if err != nil {
					// Fixed: Expose and log Solr network failures in the console (no longer swallowed silently)
					addonLogger.Error("Easynews Solr search failed for query '%s': %v", query, err)
					if IsAuthError(err) {
						return err
					}
					return nil
				}

				if len(res.Data) > 0 {
					resultsMu.Lock()
					allSearchResults = append(allSearchResults, searchResult{query: query, result: res})
					resultsMu.Unlock()
				}
				return nil
			})
		}

		if err := g.Wait(); err != nil {
			return err
		}

		uniqueHashes := make(map[string]struct{})
		resultsMu.Lock()
		for _, sr := range allSearchResults {
			for _, f := range sr.result.Data {
				uniqueHashes[f.GetHash()] = struct{}{}
			}
		}
		resultsMu.Unlock()
		totalFoundResults = len(uniqueHashes)

		return nil
	}

	// Phase 1: Specific Year/Standard Search First [1]
	if len(primaryQueries) > 0 {
		if err := runSearchPhase(primaryQueries); err != nil {
			addonLogger.Error("Easynews API search failed with authorization/connection error: %v", err)
			return authErrorStream(config.UILanguage), nil
		}
	}

	// Phase 2: If we are still below the total max results threshold, query broad fallback (multi-word movies only) [1]
	if totalFoundResults < totalMaxResults && len(fallbackQueries) > 0 {
		addonLogger.Info("Current results (%d) under cap (%d). Executing fallback broad searches...", totalFoundResults, totalMaxResults)
		if err := runSearchPhase(fallbackQueries); err != nil {
			addonLogger.Error("Easynews API search (broad fallback) failed: %v", err)
			return authErrorStream(config.UILanguage), nil
		}
	}

	// Phase 3 Fallback: If results are extremely low (e.g. < 5) and the series title is multi-word,
	// we perform a broad, title-only search to let our Go addon's MatchesTitle filter
	// catch non-standard episode filename releases (especially useful when strict matching is off) [1]
	if contentType == "series" && totalFoundResults < 5 && isMultiWord(meta.Name) {
		addonLogger.Info("Low results (%d) for multi-word series '%s'. Running title-only fallback search...", totalFoundResults, meta.Name)
		
		var titleFallbackQueries []string
		for _, titleVariant := range allTitles {
			if strings.TrimSpace(titleVariant) == "" || !isMultiWord(titleVariant) {
				continue
			}
			// Build query with just the title (no season/episode) using movie config template [1]
			m := MetaProviderResponse{Name: titleVariant}
			titleFallbackQueries = append(titleFallbackQueries, BuildSearchQuery("movie", m))
		}
		
		if len(titleFallbackQueries) > 0 {
			if err := runSearchPhase(titleFallbackQueries); err != nil {
				addonLogger.Error("Easynews API search (title-only fallback) failed: %v", err)
			}
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

			if contentType == "series" {
				matched := false
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

				// Strict Season/Episode Matching for Series to prevent leakage of other episodes/seasons
				targetSeason, _ := strconv.Atoi(meta.Season)
				targetEpisode, _ := strconv.Atoi(meta.Episode)
				if targetSeason > 0 && targetEpisode > 0 {
					parsedEpisode := RobustParseInfo(title, 0)
					
					// Parse any pack or range info from the title
					isPack, _, _, hasRange := ParsePackOrRange(title, targetEpisode)
					
					// If the parsed file has an explicit mismatching season or mismatching episode, reject it.
					if (parsedEpisode.Season > 0 && parsedEpisode.Season != targetSeason) ||
						(parsedEpisode.Episode > 0 && parsedEpisode.Episode != targetEpisode && !hasRange && !isPack && !parsedEpisode.IsPack) {
						rejectedTitle++
						continue
					}

					// Explicit Movie/Empty Season Verification for Series to prevent unrelated non-series releases (like Slappy and the Stinkers)
					if parsedEpisode.Season == 0 && parsedEpisode.Episode == 0 && !isPack && !parsedEpisode.IsPack {
						rejectedTitle++
						continue
					}
				}
			}

			if contentType == "movie" {
				matched := false
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
			)
			streams = append(streams, stream)
		}
	}

	addonLogger.Info("Search complete: totalFilesSeen=%d matchingCount=%d (rejected: sample/quality=%d, duplicate=%d, titleMismatch=%d)",
		totalFilesSeen, len(streams), rejectedSample, rejectedDuplicate, rejectedTitle)

	// -----------------------------------------------------------------------
	// Low-Latency Sorting Pipeline (With Defensive Pointer Safety checks)
	// -----------------------------------------------------------------------

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
					return false // Shift items lacking sort keys to bottom
				}
				if b.SortMeta == nil {
					return true
				}
				if a.SortMeta.QualityScore != b.SortMeta.QualityScore {
					return a.SortMeta.QualityScore > b.SortMeta.QualityScore
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
					return false // Shift items lacking sort keys to bottom
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
					if a.QualityScore != b.QualityScore {
						return a.QualityScore > b.QualityScore
					}
					if a.HasPreferredLang != b.HasPreferredLang {
						return a.HasPreferredLang
					}
					return false
				case "date_first":
					if a.DateMs != b.DateMs {
						return a.DateMs > b.DateMs
					}
					if a.QualityScore != b.QualityScore {
						return a.QualityScore > b.QualityScore
					}
					if a.HasPreferredLang != b.HasPreferredLang {
						return a.HasPreferredLang
					}
					return CompareSizeMeta(a, b) < 0
				case "lang_first", "language_first":
					if a.HasPreferredLang != b.HasPreferredLang {
						return a.HasPreferredLang
					}
					if a.QualityScore != b.QualityScore {
						return a.QualityScore > b.QualityScore
					}
					return CompareSizeMeta(a, b) < 0
				default: // quality_first
					if a.QualityScore != b.QualityScore {
						return a.QualityScore > b.QualityScore
					}
					if a.HasPreferredLang != b.HasPreferredLang {
						return a.HasPreferredLang
					}
					return CompareSizeMeta(a, b) < 0
				}
			})
		}
	}

	// -----------------------------------------------------------------------
	// Post-Sorting Local Filters
	// -----------------------------------------------------------------------

	if len(streams) > 0 {
		// 1. Resolution / Quality filters
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

		// 2. Max File Size filter (With defensive pointer dereference checks)
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

		// 3. Max results limit per quality category
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

// ---------------------------------------------------------------------------
// Single Stream Resource Mapping
// ---------------------------------------------------------------------------

func MapStream(duration, size, fullResolution, title, fileExtension string, videoSize int64, url string, file api.FileData, preferredLang string) Stream {
	quality := GetQuality(title, fullResolution)

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

	sortMeta := &SortMeta{
		QualityScore:     QualityScoreFromLabel(quality),
		SizeUnit:         sizeUnit,
		SizeValue:        sizeValue,
		DateMs:           dateMs,
		HasPreferredLang: hasPreferredLang,
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
	bingeGroup := fmt.Sprintf("easynews-plus-plus|%s|%s|%s", quality, bingeLang, fileExtension)

	name := "Easynews++"
	if quality != "" {
		name += "\n" + quality
	}

	// Statically formatted emoji literal characters to ensure safe cross-platform UTF-8 compilation
	description := fmt.Sprintf("%s%s\n%s\n🕛 %s\n📦 %s %s\n%s",
		title, fileExtension,
		FormatBadges(title), // Native badge metadata [1]
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
