package addon

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/bytedance/sonic"
	"github.com/kiskey/stremio-easynews-go/internal/i18n"
	"github.com/kiskey/stremio-easynews-go/internal/shared"
	"golang.org/x/sync/singleflight"
)

var metaLogger = shared.CreateLogger("Meta", "")

var (
	tmdbAPIKey      = strings.TrimSpace(os.Getenv("TMDB_API_KEY"))
	tmdbAccessToken = strings.TrimSpace(os.Getenv("TMDB_ACCESS_TOKEN"))
	useTMDB         atomic.Bool
)

const (
	metaFetchTimeout  = 5 * time.Second
	metadataUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

var metadataHTTPClient = &http.Client{
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   20,
		MaxConnsPerHost:       40,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: metaFetchTimeout,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
	},
}

func init() {
	useTMDB.Store(tmdbAccessToken != "" || tmdbAPIKey != "")
}

type tmdbIDMapping struct {
	id               int
	isMovie          bool
	originalLanguage string
}

func NewBoundedCache[K comparable, V any](maxEntries int, ttl time.Duration) *shared.TTLCache[K, V] {
	return shared.NewTTLCache[K, V](maxEntries, ttl)
}

type tmdbAlternativeTitle struct {
	Country string
	Title   string
	Type    string
}

type tmdbDetails struct {
	DisplayTitle        string
	OriginalTitle       string
	Year                int
	OriginCountry       []string
	IsAnimation         bool
	SeasonEpisodeCounts map[int]int
	AlternativeTitles   []tmdbAlternativeTitle
	TranslatedTitles    map[string]string
}

type tmdbSeasonDetails struct {
	AirDates     map[int]string
	EpisodeCount int
}

var (
	metadataCacheMaxEntries = shared.ParseIntEnv("METADATA_CACHE_ENTRIES", 2000)
	imdbToTMDBIDCache       = NewBoundedCache[string, tmdbIDMapping](metadataCacheMaxEntries, 48*time.Hour)
	tmdbDetailsCache        = NewBoundedCache[string, tmdbDetails](metadataCacheMaxEntries, 48*time.Hour)
	tmdbSeasonAirDateCache  = NewBoundedCache[string, tmdbSeasonDetails](metadataCacheMaxEntries, 24*time.Hour)
	metaResponseCache       = NewBoundedCache[string, MetaProviderResponse](metadataCacheMaxEntries, 24*time.Hour)

	tmdbIDSingleflight        singleflight.Group
	tmdbDetailsSingleflight   singleflight.Group
	seasonAirDateSingleflight singleflight.Group
	metaSingleflight          singleflight.Group
)

func drainAndClose(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, body)
	_ = body.Close()
}

func retryAfterDuration(value string, fallback time.Duration) time.Duration {
	if value == "" {
		return fallback
	}
	if secs, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
		if secs < 0 {
			return fallback
		}
		return time.Duration(secs) * time.Second
	}
	if retryAt, err := http.ParseTime(value); err == nil {
		delay := time.Until(retryAt)
		if delay < 0 {
			return 0
		}
		return delay
	}
	return fallback
}

func newMetadataRequest(ctx context.Context, rawURL, acceptLanguage string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", metadataUserAgent)
	if acceptLanguage != "" {
		req.Header.Set("Accept-Language", acceptLanguage)
	}
	return req, nil
}

func tmdbEndpoint(path string, query url.Values) string {
	u := url.URL{
		Scheme: "https",
		Host:   "api.themoviedb.org",
		Path:   path,
	}
	if query != nil {
		u.RawQuery = query.Encode()
	}
	return u.String()
}

func newTMDBRequestWithCredentials(ctx context.Context, path string, query url.Values, acceptLanguage, accessToken, apiKey string) (*http.Request, error) {
	req, err := newMetadataRequest(ctx, tmdbEndpoint(path, query), acceptLanguage)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	accessToken = strings.TrimSpace(accessToken)
	apiKey = strings.TrimSpace(apiKey)
	switch {
	case accessToken != "":
		req.Header.Set("Authorization", "Bearer "+accessToken)
	case apiKey != "":
		values := req.URL.Query()
		values.Set("api_key", apiKey)
		req.URL.RawQuery = values.Encode()
	default:
		return nil, fmt.Errorf("TMDB credentials are not configured")
	}
	return req, nil
}

func newTMDBRequest(ctx context.Context, path string, query url.Values, acceptLanguage string) (*http.Request, error) {
	return newTMDBRequestWithCredentials(ctx, path, query, acceptLanguage, tmdbAccessToken, tmdbAPIKey)
}

func validateTMDBResponse(resp *http.Response, operation string) error {
	if resp == nil {
		return fmt.Errorf("TMDB %s returned an empty response", operation)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		useTMDB.Store(false)
		metaLogger.Error("TMDB: Authentication rejected while performing %s. Disabling TMDB integration for this process.", operation)
		return fmt.Errorf("TMDB authentication failed")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("TMDB %s error: %d", operation, resp.StatusCode)
	}
	return nil
}

func fetchWithRetry(ctx context.Context, client *http.Client, req *http.Request) (*http.Response, error) {
	const maxRetries = 3

	if client == nil {
		client = metadataHTTPClient
	}

	var lastErr error
	backoff := 500 * time.Millisecond

	for attempt := 0; attempt < maxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		reqWithCtx := req.WithContext(ctx)
		resp, err := client.Do(reqWithCtx)
		if err == nil && resp != nil {
			if resp.StatusCode < http.StatusInternalServerError && resp.StatusCode != http.StatusTooManyRequests {
				return resp, nil
			}

			statusCode := resp.StatusCode
			retryAfter := ""
			if statusCode == http.StatusTooManyRequests {
				retryAfter = resp.Header.Get("Retry-After")
			}
			drainAndClose(resp.Body)

			if statusCode == http.StatusTooManyRequests {
				lastErr = fmt.Errorf("metadata downstream rate limited: %d", statusCode)
			} else {
				lastErr = fmt.Errorf("metadata downstream server error: %d", statusCode)
			}

			if attempt == maxRetries-1 {
				break
			}

			wait := backoff
			if statusCode == http.StatusTooManyRequests {
				wait = retryAfterDuration(retryAfter, backoff)
			}

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
			backoff *= 2
			continue
		}

		if resp != nil {
			drainAndClose(resp.Body)
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("metadata downstream returned an empty response")
		}

		if attempt == maxRetries-1 {
			break
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
	}

	return nil, lastErr
}

func resolveTMDBID(imdbID string) (int, bool, string, error) {
	if !useTMDB.Load() {
		return 0, false, "", nil
	}
	if val, ok := imdbToTMDBIDCache.Get(imdbID); ok {
		return val.id, val.isMovie, val.originalLanguage, nil
	}

	res, err, _ := tmdbIDSingleflight.Do(imdbID, func() (interface{}, error) {
		if val, ok := imdbToTMDBIDCache.Get(imdbID); ok {
			return val, nil
		}
		if !useTMDB.Load() {
			return tmdbIDMapping{}, nil
		}

		metaLogger.Info("TMDB: Resolving TMDB ID from IMDb ID '%s'...", imdbID)

		ctx, cancel := context.WithTimeout(context.Background(), metaFetchTimeout)
		defer cancel()

		findQuery := url.Values{"external_source": []string{"imdb_id"}}
		req, err := newTMDBRequest(ctx, "/3/find/"+imdbID, findQuery, "en-US")
		if err != nil {
			return tmdbIDMapping{}, err
		}

		resp, err := fetchWithRetry(ctx, metadataHTTPClient, req)
		if err != nil {
			metaLogger.Error("TMDB: Request failed to find TMDB mapping for IMDb ID '%s': %v", imdbID, err)
			return tmdbIDMapping{}, err
		}
		defer drainAndClose(resp.Body)

		if err := validateTMDBResponse(resp, "find"); err != nil {
			metaLogger.Error("TMDB: Find lookup failed for IMDb ID '%s': %v", imdbID, err)
			return tmdbIDMapping{}, err
		}

		var findData struct {
			MovieResults []struct {
				ID               int    `json:"id"`
				OriginalLanguage string `json:"original_language"`
			} `json:"movie_results"`
			TVResults []struct {
				ID               int    `json:"id"`
				OriginalLanguage string `json:"original_language"`
			} `json:"tv_results"`
		}
		if err := sonic.ConfigStd.NewDecoder(resp.Body).Decode(&findData); err != nil {
			return tmdbIDMapping{}, err
		}

		isMovie := len(findData.MovieResults) > 0
		isTV := len(findData.TVResults) > 0
		if !isMovie && !isTV {
			metaLogger.Info("TMDB: No TMDB ID mapping found on find endpoint for IMDb ID '%s'", imdbID)
			return tmdbIDMapping{}, nil
		}

		var tmdbID int
		var origLang string
		if isMovie {
			tmdbID = findData.MovieResults[0].ID
			origLang = findData.MovieResults[0].OriginalLanguage
		} else {
			tmdbID = findData.TVResults[0].ID
			origLang = findData.TVResults[0].OriginalLanguage
		}

		mapping := tmdbIDMapping{id: tmdbID, isMovie: isMovie, originalLanguage: origLang}
		imdbToTMDBIDCache.Set(imdbID, mapping)
		metaLogger.Info("TMDB: Successfully resolved IMDb ID '%s' to TMDB ID %d (isMovie: %v, Lang: %s)", imdbID, tmdbID, isMovie, origLang)
		return mapping, nil
	})

	if err != nil {
		return 0, false, "", err
	}
	mapping := res.(tmdbIDMapping)
	return mapping.id, mapping.isMovie, mapping.originalLanguage, nil
}

func getTMDBSeasonDetails(imdbID string, season int) tmdbSeasonDetails {
	if !useTMDB.Load() || season == 0 {
		return tmdbSeasonDetails{AirDates: make(map[int]string)}
	}

	cacheKey := fmt.Sprintf("%s:%d", imdbID, season)
	if cached, ok := tmdbSeasonAirDateCache.Get(cacheKey); ok {
		return cached
	}

	res, err, _ := seasonAirDateSingleflight.Do(cacheKey, func() (interface{}, error) {
		if cached, ok := tmdbSeasonAirDateCache.Get(cacheKey); ok {
			return cached, nil
		}

		tmdbID, isMovie, _, err := resolveTMDBID(imdbID)
		if err != nil || tmdbID == 0 || isMovie {
			return tmdbSeasonDetails{AirDates: make(map[int]string)}, err
		}

		ctx, cancel := context.WithTimeout(context.Background(), metaFetchTimeout)
		defer cancel()

		seasonPath := fmt.Sprintf("/3/tv/%d/season/%d", tmdbID, season)
		req, err := newTMDBRequest(ctx, seasonPath, nil, "en-US")
		if err != nil {
			return tmdbSeasonDetails{AirDates: make(map[int]string)}, err
		}

		resp, err := fetchWithRetry(ctx, metadataHTTPClient, req)
		if err != nil {
			return tmdbSeasonDetails{AirDates: make(map[int]string)}, err
		}
		defer drainAndClose(resp.Body)

		if err := validateTMDBResponse(resp, "season details"); err != nil {
			return tmdbSeasonDetails{AirDates: make(map[int]string)}, err
		}

		var data struct {
			Episodes []struct {
				EpisodeNumber int    `json:"episode_number"`
				AirDate       string `json:"air_date"`
			} `json:"episodes"`
		}
		if err := sonic.ConfigStd.NewDecoder(resp.Body).Decode(&data); err != nil {
			return tmdbSeasonDetails{AirDates: make(map[int]string)}, err
		}

		airDates := make(map[int]string)
		for _, ep := range data.Episodes {
			dateStr := strings.ReplaceAll(ep.AirDate, "-", ".")
			if dateStr != "" {
				airDates[ep.EpisodeNumber] = dateStr
			}
		}

		details := tmdbSeasonDetails{
			AirDates:     airDates,
			EpisodeCount: len(data.Episodes),
		}

		tmdbSeasonAirDateCache.Set(cacheKey, details)
		return details, nil
	})

	if err != nil {
		return tmdbSeasonDetails{AirDates: make(map[int]string)}
	}
	return res.(tmdbSeasonDetails)
}

func getTMDBSeasonAirDates(imdbID string, season int) map[int]string {
	return getTMDBSeasonDetails(imdbID, season).AirDates
}

func decodeTMDBDetails(body io.Reader, isMovie bool) (tmdbDetails, error) {
	var details struct {
		Title               string   `json:"title"`
		Name                string   `json:"name"`
		OriginalTitle       string   `json:"original_title"`
		OriginalName        string   `json:"original_name"`
		ReleaseDate         string   `json:"release_date"`
		FirstAirDate        string   `json:"first_air_date"`
		OriginCountry       []string `json:"origin_country"`
		ProductionCountries []struct {
			ISO3166_1 string `json:"iso_3166_1"`
		} `json:"production_countries"`
		Genres []struct {
			ID int `json:"id"`
		} `json:"genres"`
		Seasons []struct {
			SeasonNumber int `json:"season_number"`
			EpisodeCount int `json:"episode_count"`
		} `json:"seasons"`
		AlternativeTitles struct {
			Titles []struct {
				ISO3166_1 string `json:"iso_3166_1"`
				Title     string `json:"title"`
				Type      string `json:"type"`
			} `json:"titles"`
			Results []struct {
				ISO3166_1 string `json:"iso_3166_1"`
				Title     string `json:"title"`
				Type      string `json:"type"`
			} `json:"results"`
		} `json:"alternative_titles"`
		Translations struct {
			Translations []struct {
				ISO639_1 string `json:"iso_639_1"`
				Data     struct {
					Title string `json:"title"`
					Name  string `json:"name"`
				} `json:"data"`
			} `json:"translations"`
		} `json:"translations"`
	}
	if err := sonic.ConfigStd.NewDecoder(body).Decode(&details); err != nil {
		return tmdbDetails{}, err
	}

	displayTitle := strings.TrimSpace(details.Title)
	if displayTitle == "" {
		displayTitle = strings.TrimSpace(details.Name)
	}
	originalTitle := strings.TrimSpace(details.OriginalTitle)
	if originalTitle == "" {
		originalTitle = strings.TrimSpace(details.OriginalName)
	}
	if originalTitle == "" {
		originalTitle = displayTitle
	}

	year := yearFromISODate(details.ReleaseDate)
	if year == 0 {
		year = yearFromISODate(details.FirstAirDate)
	}

	originCountries := append([]string(nil), details.OriginCountry...)
	if len(originCountries) == 0 {
		for _, country := range details.ProductionCountries {
			code := strings.TrimSpace(country.ISO3166_1)
			if code != "" {
				originCountries = append(originCountries, code)
			}
		}
	}

	isAnimation := false
	for _, genre := range details.Genres {
		if genre.ID == 16 {
			isAnimation = true
			break
		}
	}

	counts := make(map[int]int)
	for _, season := range details.Seasons {
		counts[season.SeasonNumber] = season.EpisodeCount
	}

	alternativeTitles := make([]tmdbAlternativeTitle, 0, len(details.AlternativeTitles.Titles)+len(details.AlternativeTitles.Results))
	for _, item := range details.AlternativeTitles.Titles {
		alternativeTitles = append(alternativeTitles, tmdbAlternativeTitle{
			Country: strings.ToUpper(strings.TrimSpace(item.ISO3166_1)),
			Title:   strings.TrimSpace(item.Title),
			Type:    strings.TrimSpace(item.Type),
		})
	}
	for _, item := range details.AlternativeTitles.Results {
		alternativeTitles = append(alternativeTitles, tmdbAlternativeTitle{
			Country: strings.ToUpper(strings.TrimSpace(item.ISO3166_1)),
			Title:   strings.TrimSpace(item.Title),
			Type:    strings.TrimSpace(item.Type),
		})
	}

	translatedTitles := make(map[string]string)
	for _, translation := range details.Translations.Translations {
		language := strings.ToLower(strings.TrimSpace(translation.ISO639_1))
		if language == "" {
			continue
		}
		translated := ""
		if isMovie {
			translated = strings.TrimSpace(translation.Data.Title)
		} else {
			translated = strings.TrimSpace(translation.Data.Name)
		}
		if translated == "" {
			continue
		}
		if _, exists := translatedTitles[language]; !exists {
			translatedTitles[language] = translated
		}
	}

	return tmdbDetails{
		DisplayTitle:        displayTitle,
		OriginalTitle:       originalTitle,
		Year:                year,
		OriginCountry:       originCountries,
		IsAnimation:         isAnimation,
		SeasonEpisodeCounts: counts,
		AlternativeTitles:   alternativeTitles,
		TranslatedTitles:    translatedTitles,
	}, nil
}

func getTMDBDetails(imdbID string) tmdbDetails {
	if !useTMDB.Load() {
		return tmdbDetails{}
	}

	if cached, ok := tmdbDetailsCache.Get(imdbID); ok {
		return cached
	}

	res, err, _ := tmdbDetailsSingleflight.Do(imdbID, func() (interface{}, error) {
		if cached, ok := tmdbDetailsCache.Get(imdbID); ok {
			return cached, nil
		}

		tmdbID, isMovie, _, err := resolveTMDBID(imdbID)
		if err != nil || tmdbID == 0 {
			return tmdbDetails{}, err
		}

		mediaPath := fmt.Sprintf("/3/tv/%d", tmdbID)
		if isMovie {
			mediaPath = fmt.Sprintf("/3/movie/%d", tmdbID)
		}
		query := url.Values{"append_to_response": []string{"alternative_titles,translations"}}

		ctx, cancel := context.WithTimeout(context.Background(), metaFetchTimeout)
		defer cancel()

		req, err := newTMDBRequest(ctx, mediaPath, query, "en-US")
		if err != nil {
			return tmdbDetails{}, err
		}

		resp, err := fetchWithRetry(ctx, metadataHTTPClient, req)
		if err != nil {
			return tmdbDetails{}, err
		}
		defer drainAndClose(resp.Body)

		if err := validateTMDBResponse(resp, "details"); err != nil {
			return tmdbDetails{}, err
		}

		value, err := decodeTMDBDetails(resp.Body, isMovie)
		if err != nil {
			return tmdbDetails{}, err
		}
		tmdbDetailsCache.Set(imdbID, value)
		return value, nil
	})

	if err != nil {
		return tmdbDetails{}
	}
	return res.(tmdbDetails)
}

func getTMDBOriginalTitle(imdbID string) string {
	return getTMDBDetails(imdbID).OriginalTitle
}

func filterTMDBAlternativeTitleItems(items []tmdbAlternativeTitle, originalLanguage, altTitleCountry string) []tmdbAlternativeTitle {
	langToCountry := map[string]string{
		"ko": "KR", "ja": "JP", "zh": "CN", "ru": "RU",
		"hi": "IN", "th": "TH", "vi": "VN", "tr": "TR",
		"ar": "SA", "he": "IL", "fa": "IR",
	}
	originalCountry := langToCountry[strings.ToLower(strings.TrimSpace(originalLanguage))]
	romanizedTypes := map[string]bool{
		"romaji": true, "pinyin": true, "transliteration": true,
		"modern title": true,
	}

	requestedCountry := strings.ToUpper(strings.TrimSpace(altTitleCountry))
	allowAll := strings.EqualFold(strings.TrimSpace(altTitleCountry), "all")
	seen := make(map[string]struct{}, len(items))
	out := make([]tmdbAlternativeTitle, 0, len(items))
	for _, item := range items {
		item.Title = strings.TrimSpace(item.Title)
		item.Country = strings.ToUpper(strings.TrimSpace(item.Country))
		item.Type = strings.TrimSpace(item.Type)
		if len(item.Title) <= 1 {
			continue
		}

		allowed := allowAll || item.Country == "US" || item.Country == "GB" || item.Country == "CA" || item.Country == ""
		if requestedCountry != "" && requestedCountry != "ALL" && item.Country == requestedCountry {
			allowed = true
		}
		if originalCountry != "" && item.Country == originalCountry {
			allowed = true
		}
		if romanizedTypes[strings.ToLower(item.Type)] {
			allowed = true
		}
		if !allowed {
			continue
		}

		key := strings.ToLower(item.Title)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func getTMDBAlternativeTitleItems(imdbID string, enableAltTitles bool, altTitleCountry string) ([]tmdbAlternativeTitle, error) {
	if !useTMDB.Load() || !enableAltTitles {
		return nil, nil
	}
	_, _, originalLanguage, err := resolveTMDBID(imdbID)
	if err != nil {
		return nil, err
	}
	details := getTMDBDetails(imdbID)
	return filterTMDBAlternativeTitleItems(details.AlternativeTitles, originalLanguage, altTitleCountry), nil
}

func getTMDBAlternativeTitles(imdbID string, enableAltTitles bool, altTitleCountry string) ([]string, error) {
	items, err := getTMDBAlternativeTitleItems(imdbID, enableAltTitles, altTitleCountry)
	if err != nil {
		return nil, err
	}
	titles := make([]string, 0, len(items))
	for _, item := range items {
		titles = append(titles, item.Title)
	}
	return titles, nil
}

func getTMDBTranslatedTitle(imdbID, preferredLanguage string) (string, error) {
	if !useTMDB.Load() || preferredLanguage == "" {
		return "", nil
	}
	tmdbLanguage := strings.ToLower(strings.TrimSpace(i18n.ConvertToTMDBLanguageCode(preferredLanguage)))
	if tmdbLanguage == "" {
		return "", nil
	}
	details := getTMDBDetails(imdbID)
	return strings.TrimSpace(details.TranslatedTitles[tmdbLanguage]), nil
}

func imdbMetaProvider(id, preferredLanguage string, enableAltTitles bool, altTitleCountry string) (MetaProviderResponse, error) {
	parts := strings.Split(id, ":")
	tt := parts[0]
	var season, episode string
	if len(parts) > 1 {
		season = parts[1]
	}
	if len(parts) > 2 {
		episode = parts[2]
	}

	metaLogger.Info("Meta: Querying IMDb Suggestions API for ID '%s'...", tt)

	ctx, cancel := context.WithTimeout(context.Background(), metaFetchTimeout)
	defer cancel()

	u := fmt.Sprintf("https://v2.sg.media-imdb.com/suggestion/t/%s.json", tt)
	req, err := newMetadataRequest(ctx, u, "")
	if err != nil {
		return MetaProviderResponse{}, err
	}

	resp, err := fetchWithRetry(ctx, metadataHTTPClient, req)
	if err != nil {
		metaLogger.Error("Meta: IMDb suggestion lookup failed for ID '%s': %v", tt, err)
		return MetaProviderResponse{}, err
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return MetaProviderResponse{}, fmt.Errorf("IMDb suggestion error: %d", resp.StatusCode)
	}

	var data struct {
		D []struct {
			ID string `json:"id"`
			L  string `json:"l"`
			Y  int    `json:"y"`
		} `json:"d"`
	}
	if err := sonic.ConfigStd.NewDecoder(resp.Body).Decode(&data); err != nil {
		return MetaProviderResponse{}, err
	}

	var item struct {
		L string
		Y int
	}
	found := false
	for _, d := range data.D {
		if d.ID == tt {
			item.L = d.L
			item.Y = d.Y
			found = true
			break
		}
	}
	if !found {
		metaLogger.Warn("Meta: No matching IMDb suggestion record found for ID '%s'", tt)
		return MetaProviderResponse{}, fmt.Errorf("no IMDb match for %s", tt)
	}

	originalName := item.L
	metaLogger.Info("Meta: IMDb suggestion resolved primary title: '%s' (Year: %d)", originalName, item.Y)
	alternatives := GetAlternativeTitles(originalName)

	var origLang string
	var airDate string
	var isAnimation bool
	var originCountries []string
	var seasonEpisodeCount int
	var counts map[int]int
	if useTMDB.Load() {
		altTitles, err := getTMDBAlternativeTitles(tt, enableAltTitles, altTitleCountry)
		if err == nil && len(altTitles) > 0 {
			for _, alt := range altTitles {
				isDup := false
				for _, existing := range alternatives {
					if strings.EqualFold(existing, alt) {
						isDup = true
						break
					}
				}
				if !isDup {
					alternatives = append(alternatives, alt)
				}
			}
		}

		tmDetails := getTMDBDetails(tt)
		origTitle := tmDetails.OriginalTitle
		isAnimation = tmDetails.IsAnimation
		originCountries = tmDetails.OriginCountry
		counts = tmDetails.SeasonEpisodeCounts

		if origTitle != "" {
			isDup := false
			for _, existing := range alternatives {
				if strings.EqualFold(existing, origTitle) {
					isDup = true
					break
				}
			}
			if !isDup {
				alternatives = append(alternatives, origTitle)
			}
		}

		_, _, origLang, _ = resolveTMDBID(tt)

		sInt, _ := strconv.Atoi(season)
		eInt, _ := strconv.Atoi(episode)
		if sInt > 0 && eInt > 0 {
			sDetails := getTMDBSeasonDetails(tt, sInt)
			airDates := sDetails.AirDates
			if len(airDates) > 0 {
				airDate = airDates[eInt]
			}
			seasonEpisodeCount = sDetails.EpisodeCount
		}
	}

	translitName := Transliterate(originalName)
	if translitName != originalName && translitName != "" {
		isDup := false
		for _, existing := range alternatives {
			if strings.EqualFold(existing, translitName) {
				isDup = true
				break
			}
		}
		if !isDup {
			alternatives = append(alternatives, translitName)
		}
	}
	for _, alt := range alternatives {
		tAlt := Transliterate(alt)
		if tAlt != alt && tAlt != "" {
			isDup := false
			for _, existing := range alternatives {
				if strings.EqualFold(existing, tAlt) {
					isDup = true
					break
				}
			}
			if !isDup {
				alternatives = append(alternatives, tAlt)
			}
		}
	}

	if preferredLanguage != "" {
		translated, err := getTMDBTranslatedTitle(tt, preferredLanguage)
		if err == nil && translated != "" {
			hasIt := false
			for _, a := range alternatives {
				if strings.EqualFold(a, translated) {
					hasIt = true
					break
				}
			}
			if !hasIt {
				alternatives = append(alternatives, translated)
			}
			sanitized := SanitizeTitle(translated)
			if sanitized != translated {
				hasSanitized := false
				for _, a := range alternatives {
					if strings.EqualFold(a, sanitized) {
						hasSanitized = true
						break
					}
				}
				if !hasSanitized {
					alternatives = append(alternatives, sanitized)
				}
			}
		}
	}

	return MetaProviderResponse{
		Name:                originalName,
		OriginalName:        originalName,
		AlternativeNames:    alternatives,
		Year:                item.Y,
		Season:              season,
		Episode:             episode,
		OriginalLanguage:    origLang,
		EpisodeAirDate:      airDate,
		IsAnimation:         isAnimation,
		OriginCountries:     originCountries,
		SeasonEpisodeCount:  seasonEpisodeCount,
		SeasonEpisodeCounts: counts,
	}, nil
}

func cinemetaMetaProvider(id, contentType, preferredLanguage string, enableAltTitles bool, altTitleCountry string) (MetaProviderResponse, error) {
	parts := strings.Split(id, ":")
	tt := parts[0]
	var season, episode string
	if len(parts) > 1 {
		season = parts[1]
	}
	if len(parts) > 2 {
		episode = parts[2]
	}

	metaLogger.Info("Meta: Querying Cinemeta API fallback for ID '%s' (type: '%s')...", tt, contentType)

	ctx, cancel := context.WithTimeout(context.Background(), metaFetchTimeout)
	defer cancel()

	u := fmt.Sprintf("https://v3-cinemeta.strem.io/meta/%s/%s.json", contentType, tt)
	req, err := newMetadataRequest(ctx, u, "")
	if err != nil {
		return MetaProviderResponse{}, err
	}

	resp, err := fetchWithRetry(ctx, metadataHTTPClient, req)
	if err != nil {
		metaLogger.Error("Meta: Cinemeta fallback lookup failed for ID '%s': %v", tt, err)
		return MetaProviderResponse{}, err
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return MetaProviderResponse{}, fmt.Errorf("Cinemeta metadata error: %d", resp.StatusCode)
	}

	var data struct {
		Meta struct {
			Name        string `json:"name"`
			Year        string `json:"year"`
			ReleaseInfo string `json:"releaseInfo"`
		} `json:"meta"`
	}
	if err := sonic.ConfigStd.NewDecoder(resp.Body).Decode(&data); err != nil {
		return MetaProviderResponse{}, err
	}

	name := data.Meta.Name
	year := ExtractDigits(data.Meta.Year)
	if year == nil {
		year = ExtractDigits(data.Meta.ReleaseInfo)
	}
	yearVal := 0
	if year != nil {
		yearVal = *year
	}

	metaLogger.Info("Meta: Cinemeta resolved fallback title: '%s' (Year: %d)", name, yearVal)
	alternatives := GetAlternativeTitles(name)

	var origLang string
	var airDate string
	var isAnimation bool
	var originCountries []string
	var seasonEpisodeCount int
	var counts map[int]int
	if useTMDB.Load() {
		altTitles, err := getTMDBAlternativeTitles(tt, enableAltTitles, altTitleCountry)
		if err == nil && len(altTitles) > 0 {
			for _, alt := range altTitles {
				isDup := false
				for _, existing := range alternatives {
					if strings.EqualFold(existing, alt) {
						isDup = true
						break
					}
				}
				if !isDup {
					alternatives = append(alternatives, alt)
				}
			}
		}

		tmDetails := getTMDBDetails(tt)
		origTitle := tmDetails.OriginalTitle
		isAnimation = tmDetails.IsAnimation
		originCountries = tmDetails.OriginCountry
		counts = tmDetails.SeasonEpisodeCounts

		if origTitle != "" {
			isDup := false
			for _, existing := range alternatives {
				if strings.EqualFold(existing, origTitle) {
					isDup = true
					break
				}
			}
			if !isDup {
				alternatives = append(alternatives, origTitle)
			}
		}

		_, _, origLang, _ = resolveTMDBID(tt)

		sInt, _ := strconv.Atoi(season)
		eInt, _ := strconv.Atoi(episode)
		if sInt > 0 && eInt > 0 {
			sDetails := getTMDBSeasonDetails(tt, sInt)
			airDates := sDetails.AirDates
			if len(airDates) > 0 {
				airDate = airDates[eInt]
			}
			seasonEpisodeCount = sDetails.EpisodeCount
		}
	}

	translitName := Transliterate(name)
	if translitName != name && translitName != "" {
		isDup := false
		for _, existing := range alternatives {
			if strings.EqualFold(existing, translitName) {
				isDup = true
				break
			}
		}
		if !isDup {
			alternatives = append(alternatives, translitName)
		}
	}
	for _, alt := range alternatives {
		tAlt := Transliterate(alt)
		if tAlt != alt && tAlt != "" {
			isDup := false
			for _, existing := range alternatives {
				if strings.EqualFold(existing, tAlt) {
					isDup = true
					break
				}
			}
			if !isDup {
				alternatives = append(alternatives, tAlt)
			}
		}
	}

	if preferredLanguage != "" {
		translated, err := getTMDBTranslatedTitle(tt, preferredLanguage)
		if err == nil && translated != "" {
			hasIt := false
			for _, a := range alternatives {
				if strings.EqualFold(a, translated) {
					hasIt = true
					break
				}
			}
			if !hasIt {
				alternatives = append(alternatives, translated)
			}
		}
	}

	return MetaProviderResponse{
		Name:                name,
		OriginalName:        name,
		AlternativeNames:    alternatives,
		Year:                yearVal,
		Season:              season,
		Episode:             episode,
		OriginalLanguage:    origLang,
		EpisodeAirDate:      airDate,
		IsAnimation:         isAnimation,
		OriginCountries:     originCountries,
		SeasonEpisodeCount:  seasonEpisodeCount,
		SeasonEpisodeCounts: counts,
	}, nil
}

func PublicMetaProvider(id, contentType, preferredLanguage string, enableAltTitles bool, altTitleCountry string) (MetaProviderResponse, error) {
	tt, season, episode := splitMetadataID(id)

	// The metadata schema/version is part of the key so provider-priority changes
	// cannot reuse stale in-process entries created by an older projection.
	cacheKey := fmt.Sprintf("%s:%s:%s:%s:%t:%s", canonicalMetadataVersion, id, contentType, preferredLanguage, enableAltTitles, altTitleCountry)

	if cached, ok := metaResponseCache.Get(cacheKey); ok {
		metaLogger.Info("Meta Cache HIT for core key '%s' (source=%s)", tt, cached.MetadataSource)
		cached.Season = season
		cached.Episode = episode
		return cached, nil
	}

	res, err, _ := metaSingleflight.Do(cacheKey, func() (interface{}, error) {
		if cached, ok := metaResponseCache.Get(cacheKey); ok {
			return cached, nil
		}

		metaLogger.Info("Meta Cache MISS: Resolving canonical metadata for core key '%s'", tt)

		canonical, err := resolveCanonicalMetadata(id, contentType, preferredLanguage, enableAltTitles, altTitleCountry)
		if err != nil {
			return MetaProviderResponse{}, fmt.Errorf("failed to find metadata for %s: %w", id, err)
		}
		projected := canonical.toMetaProviderResponse()
		if projected.Name == "" {
			return MetaProviderResponse{}, fmt.Errorf("failed to find metadata for %s: empty canonical title", id)
		}

		metaResponseCache.Set(cacheKey, projected)
		metaLogger.Info("Meta: Canonical metadata resolved for '%s' via %s (confidence=%.2f, variants=%d)",
			tt, projected.MetadataSource, projected.MetadataConfidence, len(projected.TitleVariants))
		return projected, nil
	})

	if err != nil {
		return MetaProviderResponse{}, err
	}

	finalMeta := res.(MetaProviderResponse)
	finalMeta.Season = season
	finalMeta.Episode = episode
	return finalMeta, nil
}
