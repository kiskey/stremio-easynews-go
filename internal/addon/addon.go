package addon

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bytedance/sonic"
	"github.com/kiskey/stremio-easynews-go/internal/api"
	"github.com/kiskey/stremio-easynews-go/internal/i18n"
	"github.com/kiskey/stremio-easynews-go/internal/seal"
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

	// === NEW: Encrypted token fast path (< 1 µs) ===
	if seal.IsSealed(configStr) {
		var sealedConfig AddonConfig
		if err := seal.OpenConfig(configStr, &sealedConfig); err == nil {
			return sealedConfig
		} else {
			// EXPLICIT ERROR LOGGING: If it fails, we will know exactly why
			addonLogger.Error("Failed to decrypt sealed config: %v", err)
		}
	}
	// === END NEW ===

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
		if u := values.Get("username"); u != "" {
			config.Username = u
		}
		if p := values.Get("password"); p != "" {
			config.Password = p
		}
		if s := values.Get("strictTitleMatching"); s != "" {
			config.StrictTitleMatching = s
		}
		if e := values.Get("enableAltTitles"); e != "" {
			config.EnableAltTitles = e
		}
		if c := values.Get("altTitleCountry"); c != "" {
			config.AltTitleCountry = c
		}
		if l := values.Get("preferredLanguage"); l != "" {
			config.PreferredLanguage = l
		}
		if o := values.Get("sortingPreference"); o != "" {
			config.SortingPreference = o
		}
		if q := values.Get("showQualities"); q != "" {
			config.ShowQualities = q
		}
		if m := values.Get("maxResultsPerQuality"); m != "" {
			config.MaxResultsPerQuality = m
		}
		if f := values.Get("maxFileSize"); f != "" {
			config.MaxFileSize = f
		}
		if b := values.Get("baseUrl"); b != "" {
			config.BaseUrl = b
		}
		if ui := values.Get("uiLanguage"); ui != "" {
			config.UILanguage = ui
		}
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
	requestCacheMaxEntries = shared.ParseIntEnv("MAX_CACHE_ENTRIES", 1000)
	requestCache           = shared.NewTTLCache[string, StreamHandlerResult](requestCacheMaxEntries, 0)
	emptyResultCacheMaxAge = 10 * 60
	errorCacheMaxAge       = 60
)

func getFromRequestCache(key string) (StreamHandlerResult, bool) {
	result, remaining, ok := requestCache.GetWithRemainingTTL(key)
	if !ok {
		return StreamHandlerResult{}, false
	}
	if result.CacheMaxAge > 0 && remaining > 0 {
		remainingSeconds := int((remaining + time.Second - 1) / time.Second)
		if remainingSeconds < 1 {
			remainingSeconds = 1
		}
		if remainingSeconds < result.CacheMaxAge {
			result.CacheMaxAge = remainingSeconds
		}
	}
	return result, true
}

func setRequestCache(key string, data StreamHandlerResult, ttl time.Duration) {
	requestCache.SetWithTTL(key, data, ttl)
}

type StreamHandlerResult struct {
	Streams         []Stream `json:"streams"`
	CacheMaxAge     int      `json:"cacheMaxAge,omitempty"`
	StaleRevalidate int      `json:"staleRevalidate,omitempty"`
	StaleError      int      `json:"staleError,omitempty"`
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

	defer maybeLogCacheStats()

	// Tier 2: Instantiate API client early to access credentials fingerprint fast
	easynewsAPI, err := api.NewEasynewsAPI(config.Username, config.Password)
	if err != nil {
		addonLogger.Error("EasynewsAPI instantiation failed: %v", err)
		return authErrorStream(config.UILanguage), nil
	}

	// Fast fail-fast circuit-breaker short circuit (CPU check under 1µs)
	if api.IsCredentialsInvalid(easynewsAPI.GetCredKey()) {
		addonLogger.Warn("Short-circuiting stream handler: cached credentials invalid")
		return authErrorStream(config.UILanguage), nil
	}

	cacheKey := buildRequestCacheKey(contentType, id, easynewsAPI.GetCredKey(), config)

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

	// Anime & Legacy SD File Size Protection. This remains an early-exit
	// confidence input only; final stream eligibility keeps the legacy 20MB
	// IsBadVideo floor so existing playable-result behavior is preserved.
	minValidSize := int64(80 * 1024 * 1024) // 80MB for series
	if contentType == "movie" {
		minValidSize = int64(300 * 1024 * 1024) // 300MB for movies
	}

	matchContext := newCandidateMatchContext(contentType, meta, allTitles, useStrictMatching, minValidSize)
	targetEpisode := matchContext.TargetEpisode
	earlyExitTarget := shared.ParseIntEnv("EARLY_EXIT_CANDIDATES", 15)
	if earlyExitTarget < 1 {
		earlyExitTarget = 15
	}

	type searchResult struct {
		query  string
		result api.EasynewsSearchResponse
	}

	var allSearchResults []searchResult
	var resultsMu sync.Mutex
	discoveredHashes := make(map[string]struct{})
	prevalidatedHashes := make(map[string]struct{})
	candidateEvaluations := make(map[string]CandidateEvaluation)
	var totalFoundResults atomic.Int64
	var prevalidatedCandidateCount atomic.Int64

	runSearchPhase := func(queries []string) error {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		var g errgroup.Group
		sem := make(chan struct{}, searchConcurrency)

		for _, query := range queries {
			if int(prevalidatedCandidateCount.Load()) >= earlyExitTarget {
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
					type evaluatedFile struct {
						key  string
						eval CandidateEvaluation
					}
					evaluated := make([]evaluatedFile, 0, len(res.Data))
					for _, file := range res.Data {
						evaluated = append(evaluated, evaluatedFile{
							key:  candidateIdentity(file),
							eval: evaluateCandidate(file, matchContext),
						})
					}

					resultsMu.Lock()
					allSearchResults = append(allSearchResults, searchResult{query: query, result: res})
					for _, item := range evaluated {
						discoveredHashes[item.key] = struct{}{}
						if existing, exists := candidateEvaluations[item.key]; !exists || item.eval.Confidence > existing.Confidence {
							candidateEvaluations[item.key] = item.eval
						}
						if item.eval.Prevalidated {
							prevalidatedHashes[item.key] = struct{}{}
						}
					}
					totalFound := len(discoveredHashes)
					prevalidatedFound := len(prevalidatedHashes)
					totalFoundResults.Store(int64(totalFound))
					prevalidatedCandidateCount.Store(int64(prevalidatedFound))
					resultsMu.Unlock()

					if prevalidatedFound >= earlyExitTarget {
						addonLogger.Info("Early exit triggered: Found %d unique prevalidated candidates (raw unique hits=%d), cancelling remaining searches.", prevalidatedFound, totalFound)
						cancel()
					}
				}
				return nil
			})
		}

		if err := g.Wait(); err != nil {
			if IsAuthError(err) {
				return err
			}
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
	if prevalidatedCandidateCount.Load() < 10 && len(primaryLegacyQueries) > 0 {
		addonLogger.Info("Primary standard S/E sparse (%d prevalidated, %d raw). Running primary legacy queries...", prevalidatedCandidateCount.Load(), totalFoundResults.Load())
		_ = runSearchPhase(primaryLegacyQueries)
	}

	// 1.5. Primary Date Fallback Phase: ONLY if primary standard/legacy was sparse and we have a daily date
	if prevalidatedCandidateCount.Load() < 10 && len(primaryDateQueries) > 0 {
		addonLogger.Info("Primary standard/legacy sparse (%d prevalidated, %d raw). Running primary date-based queries...", prevalidatedCandidateCount.Load(), totalFoundResults.Load())
		_ = runSearchPhase(primaryDateQueries)
	}

	// 2. Sequential Cascade Gating: Alternative Titles Standard S/E (Only if results still sparse < 10)
	if prevalidatedCandidateCount.Load() < 10 && len(altQueries) > 0 {
		addonLogger.Info("Sparse primary results (%d prevalidated, %d raw). Running alternative standard queries...", prevalidatedCandidateCount.Load(), totalFoundResults.Load())
		_ = runSearchPhase(altQueries)
	}

	// 2.1. Alternative Legacy Fallback Phase: ONLY if results are still sparse (< 10)
	if prevalidatedCandidateCount.Load() < 10 && len(altLegacyQueries) > 0 {
		addonLogger.Info("Alternative standard sparse (%d prevalidated, %d raw). Running alternative legacy queries...", prevalidatedCandidateCount.Load(), totalFoundResults.Load())
		_ = runSearchPhase(altLegacyQueries)
	}

	// 2.5. Alternative Date Fallback Phase: ONLY if results are still sparse (< 10)
	if prevalidatedCandidateCount.Load() < 10 && len(altDateQueries) > 0 {
		addonLogger.Info("Primary & Alt S/E sparse (%d prevalidated, %d raw). Running alternative date queries...", prevalidatedCandidateCount.Load(), totalFoundResults.Load())
		_ = runSearchPhase(altDateQueries)
	}

	// 3. Lazy Gate Fallback: Broad searches (Only if results are still sparse < 10)
	if prevalidatedCandidateCount.Load() < 10 && len(broadQueries) > 0 {
		addonLogger.Info("Sparse results (%d prevalidated, %d raw). Running broad fallbacks...", prevalidatedCandidateCount.Load(), totalFoundResults.Load())
		_ = runSearchPhase(broadQueries)
	}

	// 4. Absolute episode fallback for series (Only if results are extremely low < 5)
	if contentType == "series" && prevalidatedCandidateCount.Load() < 5 && targetEpisode > 0 {
		addonLogger.Info("Low results (%d prevalidated, %d raw) for series '%s'. Running absolute episode fallback...", prevalidatedCandidateCount.Load(), totalFoundResults.Load(), meta.Name)
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
		result := streamResultWithCachePolicy([]Stream{}, emptyResultCacheMaxAge)
		setRequestCache(cacheKey, result, time.Duration(emptyResultCacheMaxAge)*time.Second)
		return result, nil
	}

	processedCandidates := make(map[string]struct{})
	var streams []Stream

	totalFilesSeen := 0
	rejectedSample := 0
	rejectedDuplicate := 0
	rejectedTitle := 0
	rejectionReasons := make(map[string]int)

	for _, sr := range allSearchResults {
		if len(streams) >= totalMaxResults {
			break
		}
		for _, file := range sr.result.Data {
			if len(streams) >= totalMaxResults {
				break
			}

			title := GetPostTitle(file)
			totalFilesSeen++

			// Preserve the legacy accounting order: obvious bad videos are
			// rejected before duplicate detection.
			if IsBadVideo(file) {
				rejectedSample++
				continue
			}

			candidateKey := candidateIdentity(file)
			if _, dup := processedCandidates[candidateKey]; dup {
				rejectedDuplicate++
				continue
			}
			processedCandidates[candidateKey] = struct{}{}

			evaluation, ok := candidateEvaluations[candidateKey]
			if !ok {
				evaluation = evaluateCandidate(file, matchContext)
			}
			if !evaluation.Accepted {
				rejectedTitle++
				rejectionReasons[evaluation.Reason]++
				continue
			}

			parsedInfo := evaluation.Parsed

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
			if stream.SortMeta != nil {
				stream.SortMeta.CandidateConfidence = evaluation.Confidence
				stream.SortMeta.MatchedTitle = evaluation.MatchedTitle
				stream.SortMeta.MatchedTitleSource = evaluation.MatchedSource
			}
			streams = append(streams, stream)
		}
	}

	addonLogger.Info("Search complete: totalFilesSeen=%d matchingCount=%d prevalidated=%d rawUnique=%d (rejected: sample/quality=%d, duplicate=%d, candidateMismatch=%d)",
		totalFilesSeen, len(streams), prevalidatedCandidateCount.Load(), totalFoundResults.Load(), rejectedSample, rejectedDuplicate, rejectedTitle)
	if len(rejectionReasons) > 0 {
		addonLogger.Debug("Candidate rejection reasons: %v", rejectionReasons)
	}

	streams = selectAndRankStreams(streams, streamSelectionOptions{
		SortingPreference:    sortingPreference,
		QualityFilters:       qualityFilters,
		MaxFileSizeGB:        maxFileSizeGB,
		MaxResultsPerQuality: maxResultsPerQualityVal,
	})

	cacheMaxAge := getCacheMaxAge(len(streams))

	result := streamResultWithCachePolicy(streams, cacheMaxAge)

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

	sortMeta := buildStructuredSortMeta(file, preferredLang, parsedInfo, quality, size, title)

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
		Filename:    SanitizeFilenameForStremio(title + fileExtension),
		BingeGroup:  bingeGroup,
	}
	if sortMeta != nil && sortMeta.VideoSizeBytes > 0 {
		bh.VideoSize = sortMeta.VideoSizeBytes
	} else if videoSize > 0 {
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

func SanitizeFilenameForStremio(filename string) string {
	// Replace spaces and toxic brackets/symbols with periods
	r := strings.NewReplacer(
		" ", ".",
		",", ".", // Safely convert commas to periods to prevent HTTP header/URL conflicts [8]
		"(", "",
		")", "",
		"[", "",
		"]", "",
		"{", "", // Curly braces (extremely common in P2P/TamilMV)
		"}", "",
		"&", "and", // Ampersand
		"+", ".", // Plus sign
		";", ".", // Semicolon
		"\"", "", // Double quotes
		"'", "", // Single quote
		":", ".",
		"-", ".",
		"_", ".",
		"/", ".", // Slashes (breaks routing)
		"\\", ".",
		"?", "", // Question mark (breaks query strings)
		"%", "", // Percent sign (breaks URI decoding)
		"=", ".", // Equal sign
	)
	sanitized := r.Replace(filename)

	// De-duplicate double periods (e.g. ".." -> ".")
	for strings.Contains(sanitized, "..") {
		sanitized = strings.ReplaceAll(sanitized, "..", ".")
	}

	return strings.Trim(sanitized, ".")
}
