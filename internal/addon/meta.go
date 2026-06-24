package addon

import (
    "context"
    "fmt"
    "io"
    "net/http"
    "os"
    "strconv"
    "strings"
    "sync"
    "time"

    "github.com/bytedance/sonic"
    "github.com/kiskey/stremio-easynews-go/internal/i18n"
    "github.com/kiskey/stremio-easynews-go/internal/shared"
    "golang.org/x/sync/singleflight"
)

var metaLogger = shared.CreateLogger("Meta", "")

var (
    tmdbAPIKey = os.Getenv("TMDB_API_KEY")
    useTMDB    = tmdbAPIKey != ""
)

const metaFetchTimeout = 5000 * time.Millisecond

// ---------------------------------------------------------------------------
// Structural Cache Mappings
// ---------------------------------------------------------------------------

type tmdbIDMapping struct {
    id               int
    isMovie          bool
    originalLanguage string
}

// ---------------------------------------------------------------------------
// Thread-Safe Bounded Generic Cache Structure
// ---------------------------------------------------------------------------

type cacheEntry[V any] struct {
    value     V
    expiresAt int64
}

type BoundedCache[K comparable, V any] struct {
    mu         sync.RWMutex
    data       map[K]cacheEntry[V]
    maxEntries int
    ttl        time.Duration
}

func NewBoundedCache[K comparable, V any](maxEntries int, ttl time.Duration) *BoundedCache[K, V] {
    return &BoundedCache[K, V]{
        data:       make(map[K]cacheEntry[V]),
        maxEntries: maxEntries,
        ttl:        ttl,
    }
}

func (c *BoundedCache[K, V]) Get(key K) (V, bool) {
    c.mu.RLock()
    entry, ok := c.data[key]
    c.mu.RUnlock()
    if !ok {
        var zero V
        return zero, false
    }
    if time.Now().UnixNano() > entry.expiresAt {
        c.mu.Lock()
        entryCheck, okCheck := c.data[key]
        if okCheck && time.Now().UnixNano() > entryCheck.expiresAt {
            delete(c.data, key)
        }
        c.mu.Unlock()
        var zero V
        return zero, false
    }
    return entry.value, true
}

func (c *BoundedCache[K, V]) Set(key K, value V) {
    c.mu.Lock()
    defer c.mu.Unlock()

    c.data[key] = cacheEntry[V]{
        value:     value,
        expiresAt: time.Now().Add(c.ttl).UnixNano(),
    }

    if len(c.data) > c.maxEntries {
        count := 0
        for k := range c.data {
            if count >= c.maxEntries/2 {
                break
            }
            delete(c.data, k)
            count++
        }
    }
}

// Cache Instances
var (
    imdbToTMDBIDCache    = NewBoundedCache[string, tmdbIDMapping](2000, 48*time.Hour)
    tmdbAltTitlesCache   = NewBoundedCache[string, []string](2000, 24*time.Hour)
    tmdbOrigTitleCache   = NewBoundedCache[string, string](2000, 48*time.Hour)
    tmdbTransTitleCache  = NewBoundedCache[string, string](2000, 24*time.Hour)
    metaResponseCache    = NewBoundedCache[string, MetaProviderResponse](2000, 24*time.Hour)

    tmdbIDSingleflight    singleflight.Group
    altTitlesSingleflight singleflight.Group
    origTitleSingleflight singleflight.Group
    metaSingleflight      singleflight.Group
)

// ---------------------------------------------------------------------------
// Resilient API Network Fetcher with Exponential Backoff
// ---------------------------------------------------------------------------

func fetchWithRetry(ctx context.Context, client *http.Client, req *http.Request) (*http.Response, error) {
    var resp *http.Response
    var err error
    maxRetries := 3
    backoff := 500 * time.Millisecond

    for i := 0; i < maxRetries; i++ {
        reqWithCtx := req.WithContext(ctx)
        resp, err = client.Do(reqWithCtx)
        if err == nil {
            if resp.StatusCode < 500 && resp.StatusCode != 429 {
                return resp, nil
            }

            // Handle 429 Too Many Requests
            if resp.StatusCode == 429 {
                retryAfter := resp.Header.Get("Retry-After")
                resp.Body.Close()
                wait := backoff
                if retryAfter != "" {
                    if secs, parseErr := strconv.Atoi(retryAfter); parseErr == nil {
                        wait = time.Duration(secs) * time.Second
                    }
                }
                select {
                case <-ctx.Done():
                    return nil, ctx.Err()
                case <-time.After(wait):
                    backoff *= 2
                    continue
                }
            }

            resp.Body.Close()
            err = fmt.Errorf("TMDB downstream server error: %d", resp.StatusCode)
        }

        select {
        case <-ctx.Done():
            return nil, ctx.Err()
        case <-time.After(backoff):
            backoff *= 2
        }
    }
    return nil, err
}

// ---------------------------------------------------------------------------
// ID Resolution Helper (IMDb ID -> TMDB ID)
// ---------------------------------------------------------------------------

func resolveTMDBID(imdbID string) (int, bool, string, error) {
    if val, ok := imdbToTMDBIDCache.Get(imdbID); ok {
        return val.id, val.isMovie, val.originalLanguage, nil
    }

    res, err, _ := tmdbIDSingleflight.Do(imdbID, func() (interface{}, error) {
        if val, ok := imdbToTMDBIDCache.Get(imdbID); ok {
            return val, nil
        }

        metaLogger.Info("TMDB: Resolving TMDB ID from IMDb ID '%s'...", imdbID)

        ctx, cancel := context.WithTimeout(context.Background(), metaFetchTimeout)
        defer cancel()

        findURL := fmt.Sprintf("https://api.themoviedb.org/3/find/%s?api_key=%s&external_source=imdb_id", imdbID, tmdbAPIKey)
        req, _ := http.NewRequestWithContext(ctx, "GET", findURL, nil)
        req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
        req.Header.Set("Accept-Language", "en-US")

        resp, err := fetchWithRetry(ctx, http.DefaultClient, req)
        if err != nil {
            metaLogger.Error("TMDB: Request failed to find TMDB mapping for IMDb ID '%s': %v", imdbID, err)
            return tmdbIDMapping{}, err
        }
        defer func() {
            _, _ = io.Copy(io.Discard, resp.Body)
            resp.Body.Close()
        }()

        if resp.StatusCode == 401 {
            useTMDB = false
            metaLogger.Error("TMDB: Invalid API Key provided. Disabling TMDB translations globally.")
            return tmdbIDMapping{}, fmt.Errorf("TMDB API key invalid")
        }
        if resp.StatusCode != 200 {
            metaLogger.Error("TMDB: Upstream find returned status code: %d for IMDb ID '%s'", resp.StatusCode, imdbID)
            return tmdbIDMapping{}, fmt.Errorf("TMDB find error: %d", resp.StatusCode)
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

// ---------------------------------------------------------------------------
// TMDB Original Title Resolution
// ---------------------------------------------------------------------------

func getTMDBOriginalTitle(imdbID string) string {
    if !useTMDB {
        return ""
    }

    if cached, ok := tmdbOrigTitleCache.Get(imdbID); ok {
        return cached
    }

    res, err, _ := origTitleSingleflight.Do(imdbID, func() (interface{}, error) {
        if cached, ok := tmdbOrigTitleCache.Get(imdbID); ok {
            return cached, nil
        }

        tmdbID, isMovie, _, err := resolveTMDBID(imdbID)
        if err != nil || tmdbID == 0 {
            return "", err
        }

        var u string
        if isMovie {
            u = fmt.Sprintf("https://api.themoviedb.org/3/movie/%d?api_key=%s", tmdbID, tmdbAPIKey)
        } else {
            u = fmt.Sprintf("https://api.themoviedb.org/3/tv/%d?api_key=%s", tmdbID, tmdbAPIKey)
        }

        ctx, cancel := context.WithTimeout(context.Background(), metaFetchTimeout)
        defer cancel()

        req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
        req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
        req.Header.Set("Accept-Language", "en-US")

        resp, err := fetchWithRetry(ctx, http.DefaultClient, req)
        if err != nil || resp.StatusCode != 200 {
            return "", err
        }
        defer func() {
            _, _ = io.Copy(io.Discard, resp.Body)
            resp.Body.Close()
        }()

        var details struct {
            OriginalTitle string `json:"original_title"`
            OriginalName  string `json:"original_name"`
        }
        if err := sonic.ConfigStd.NewDecoder(resp.Body).Decode(&details); err != nil {
            return "", err
        }

        title := details.OriginalTitle
        if title == "" {
            title = details.OriginalName
        }

        if title != "" {
            tmdbOrigTitleCache.Set(imdbID, title)
        }
        return title, nil
    })

    if err != nil {
        return ""
    }
    return res.(string)
}

// ---------------------------------------------------------------------------
// Dynamic Alternate Titles Resolution
// ---------------------------------------------------------------------------

func getTMDBAlternativeTitles(imdbID string, enableAltTitles bool, altTitleCountry string) ([]string, error) {
    if !useTMDB || !enableAltTitles {
        return nil, nil
    }

    cacheKey := fmt.Sprintf("%s:%s", imdbID, altTitleCountry)

    if cached, ok := tmdbAltTitlesCache.Get(cacheKey); ok {
        return cached, nil
    }

    res, err, _ := altTitlesSingleflight.Do(cacheKey, func() (interface{}, error) {
        if cached, ok := tmdbAltTitlesCache.Get(cacheKey); ok {
            return cached, nil
        }

        metaLogger.Info("TMDB: Fetching alternative titles for IMDb ID '%s' (filter: '%s')...", imdbID, altTitleCountry)

        tmdbID, isMovie, origLang, err := resolveTMDBID(imdbID)
        if err != nil || tmdbID == 0 {
            return nil, err
        }

        var u string
        if isMovie {
            u = fmt.Sprintf("https://api.themoviedb.org/3/movie/%d/alternative_titles?api_key=%s", tmdbID, tmdbAPIKey)
        } else {
            u = fmt.Sprintf("https://api.themoviedb.org/3/tv/%d/alternative_titles?api_key=%s", tmdbID, tmdbAPIKey)
        }

        ctx, cancel := context.WithTimeout(context.Background(), metaFetchTimeout)
        defer cancel()

        req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
        req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
        req.Header.Set("Accept-Language", "en-US")

        resp, err := fetchWithRetry(ctx, http.DefaultClient, req)
        if err != nil {
            metaLogger.Error("TMDB: Failed to fetch alternative titles from endpoint: %v", err)
            return nil, err
        }
        defer func() {
            _, _ = io.Copy(io.Discard, resp.Body)
            resp.Body.Close()
        }()

        if resp.StatusCode != 200 {
            metaLogger.Error("TMDB: Upstream alternative titles returned status code: %d", resp.StatusCode)
            return nil, fmt.Errorf("TMDB alt titles error: %d", resp.StatusCode)
        }

        var data struct {
            ID      int `json:"id"`
            Titles  []struct {
                ISO3166_1 string `json:"iso_3166_1"`
                Title     string `json:"title"`
                Type      string `json:"type"`
            } `json:"titles"`
            Results []struct {
                ISO3166_1 string `json:"iso_3166_1"`
                Title     string `json:"title"`
                Type      string `json:"type"`
            } `json:"results"`
        }

        if err := sonic.ConfigStd.NewDecoder(resp.Body).Decode(&data); err != nil {
            return nil, err
        }

        type altTitleItem struct {
            ISO3166_1 string
            Title     string
            Type      string
        }

        var rawItems []altTitleItem
        if len(data.Titles) > 0 {
            for _, item := range data.Titles {
                rawItems = append(rawItems, altTitleItem{ISO3166_1: item.ISO3166_1, Title: item.Title, Type: item.Type})
            }
        } else if len(data.Results) > 0 {
            for _, item := range data.Results {
                rawItems = append(rawItems, altTitleItem{ISO3166_1: item.ISO3166_1, Title: item.Title, Type: item.Type})
            }
        }

        langToCountry := map[string]string{
            "ko": "KR", "ja": "JP", "zh": "CN", "ru": "RU",
            "hi": "IN", "th": "TH", "vi": "VN", "tr": "TR",
            "ar": "SA", "he": "IL", "fa": "IR",
        }
        originalCountry := langToCountry[origLang]

        romanizedTypes := map[string]bool{
            "Romaji": true, "Pinyin": true, "Transliteration": true,
            "Modern Title": true,
        }

        var cleanList []string
        seen := make(map[string]bool)
        for _, item := range rawItems {
            t := strings.TrimSpace(item.Title)
            if t == "" || len(t) <= 1 {
                continue
            }
            if seen[t] {
                continue
            }

            iso := strings.ToUpper(item.ISO3166_1)
            isAllowed := false

            if altTitleCountry == "all" {
                isAllowed = true
            } else {
                if iso == "US" || iso == "GB" || iso == "CA" || iso == "" {
                    isAllowed = true
                }
                if altTitleCountry != "" && iso == strings.ToUpper(altTitleCountry) {
                    isAllowed = true
                }
                if originalCountry != "" && iso == originalCountry {
                    isAllowed = true
                }
                if romanizedTypes[item.Type] {
                    isAllowed = true
                }
            }

            if isAllowed {
                seen[t] = true
                cleanList = append(cleanList, t)
            }
        }

        tmdbAltTitlesCache.Set(cacheKey, cleanList)
        metaLogger.Info("TMDB: Successfully resolved %d filtered alternative titles for IMDb ID '%s': %v", len(cleanList), imdbID, cleanList)
        return cleanList, nil
    })

    if err != nil {
        return nil, err
    }
    return res.([]string), nil
}

// ---------------------------------------------------------------------------
// TMDB Title Translation Logic
// ---------------------------------------------------------------------------

func getTMDBTranslatedTitle(imdbID, preferredLanguage string) (string, error) {
    if !useTMDB || preferredLanguage == "" {
        return "", nil
    }

    cacheKey := fmt.Sprintf("%s:%s", imdbID, preferredLanguage)
    if cached, ok := tmdbTransTitleCache.Get(cacheKey); ok {
        return cached, nil
    }

    tmdbLang := i18n.ConvertToTMDBLanguageCode(preferredLanguage)

    tmdbID, isMovie, _, err := resolveTMDBID(imdbID)
    if err != nil || tmdbID == 0 {
        return "", err
    }

    metaLogger.Info("TMDB: Fetching translated title for IMDb ID '%s' in language '%s'...", imdbID, tmdbLang)

    var transURL string
    if isMovie {
        transURL = fmt.Sprintf("https://api.themoviedb.org/3/movie/%d/translations?api_key=%s", tmdbID, tmdbAPIKey)
    } else {
        transURL = fmt.Sprintf("https://api.themoviedb.org/3/tv/%d/translations?api_key=%s", tmdbID, tmdbAPIKey)
    }

    ctx, cancel := context.WithTimeout(context.Background(), metaFetchTimeout)
    defer cancel()

    req, _ := http.NewRequestWithContext(ctx, "GET", transURL, nil)
    req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
    req.Header.Set("Accept-Language", tmdbLang)

    resp, err := fetchWithRetry(ctx, http.DefaultClient, req)
    if err != nil {
        metaLogger.Error("TMDB: Failed to fetch translation catalog: %v", err)
        return "", err
    }
    defer func() {
        _, _ = io.Copy(io.Discard, resp.Body)
        resp.Body.Close()
    }()

    if resp.StatusCode != 200 {
        metaLogger.Error("TMDB: Upstream translations returned status code: %d", resp.StatusCode)
        return "", fmt.Errorf("TMDB translations error: %d", resp.StatusCode)
    }

    var transData struct {
        Translations []struct {
            ISO639_1 string `json:"iso_639_1"`
            Data     struct {
                Title string `json:"title"`
                Name  string `json:"name"`
            } `json:"data"`
        } `json:"translations"`
    }
    if err := sonic.ConfigStd.NewDecoder(resp.Body).Decode(&transData); err != nil {
        return "", err
    }

    for _, t := range transData.Translations {
        if t.ISO639_1 == tmdbLang {
            if isMovie && t.Data.Title != "" {
                metaLogger.Info("TMDB: Resolved translation title for '%s' in '%s': '%s'", imdbID, tmdbLang, t.Data.Title)
                tmdbTransTitleCache.Set(cacheKey, t.Data.Title)
                return t.Data.Title, nil
            }
            if !isMovie && t.Data.Name != "" {
                metaLogger.Info("TMDB: Resolved translation name for '%s' in '%s': '%s'", imdbID, tmdbLang, t.Data.Name)
                tmdbTransTitleCache.Set(cacheKey, t.Data.Name)
                return t.Data.Name, nil
            }
        }
    }

    metaLogger.Info("TMDB: No translation found for IMDb ID '%s' in language '%s'", imdbID, tmdbLang)
    return "", nil
}

// ---------------------------------------------------------------------------
// IMDb Metadata Lookup Provider
// ---------------------------------------------------------------------------

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

    url := fmt.Sprintf("https://v2.sg.media-imdb.com/suggestion/t/%s.json", tt)
    req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
    req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

    resp, err := fetchWithRetry(ctx, http.DefaultClient, req)
    if err != nil {
        metaLogger.Error("Meta: IMDb suggestion lookup failed for ID '%s': %v", tt, err)
        return MetaProviderResponse{}, err
    }
    defer func() {
        _, _ = io.Copy(io.Discard, resp.Body)
        resp.Body.Close()
    }()

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
    if useTMDB {
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

        origTitle := getTMDBOriginalTitle(tt)
        if origTitle != "" && IsLatinString(origTitle) {
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
    }

    // Inject Transliterations for non-Latin scripts
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
        Name:             originalName,
        OriginalName:     originalName,
        AlternativeNames: alternatives,
        Year:             item.Y,
        Season:           season,
        Episode:          episode,
        OriginalLanguage: origLang,
    }, nil
}

// ---------------------------------------------------------------------------
// Cinemeta Metadata Lookup Provider (Fallback)
// ---------------------------------------------------------------------------

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

    url := fmt.Sprintf("https://v3-cinemeta.strem.io/meta/%s/%s.json", contentType, tt)
    req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
    req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

    resp, err := fetchWithRetry(ctx, http.DefaultClient, req)
    if err != nil {
        metaLogger.Error("Meta: Cinemeta fallback lookup failed for ID '%s': %v", tt, err)
        return MetaProviderResponse{}, err
    }
    defer func() {
        _, _ = io.Copy(io.Discard, resp.Body)
        resp.Body.Close()
    }()

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
    if useTMDB {
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

        origTitle := getTMDBOriginalTitle(tt)
        if origTitle != "" && IsLatinString(origTitle) {
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
    }

    // Inject Transliterations
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
        Name:             name,
        OriginalName:     name,
        AlternativeNames: alternatives,
        Year:             yearVal,
        Season:           season,
        Episode:          episode,
        OriginalLanguage: origLang,
    }, nil
}

// ---------------------------------------------------------------------------
// Public Metadata Gateway Interface
// ---------------------------------------------------------------------------

func PublicMetaProvider(id, contentType, preferredLanguage string, enableAltTitles bool, altTitleCountry string) (MetaProviderResponse, error) {
    parts := strings.Split(id, ":")
    tt := parts[0]

    cacheKey := fmt.Sprintf("%s:%s:%s:%t:%s", tt, contentType, preferredLanguage, enableAltTitles, altTitleCountry)

    if cached, ok := metaResponseCache.Get(cacheKey); ok {
        metaLogger.Info("Meta Cache HIT for core key '%s'", tt)
        var season, episode string
        if len(parts) > 1 {
            season = parts[1]
        }
        if len(parts) > 2 {
            episode = parts[2]
        }
        cached.Season = season
        cached.Episode = episode
        return cached, nil
    }

    res, err, _ := metaSingleflight.Do(cacheKey, func() (interface{}, error) {
        if cached, ok := metaResponseCache.Get(cacheKey); ok {
            return cached, nil
        }

        metaLogger.Info("Meta Cache MISS: Resolving fresh metadata for core key '%s'", tt)

        meta, err := imdbMetaProvider(id, preferredLanguage, enableAltTitles, altTitleCountry)
        if err == nil && meta.Name != "" {
            metaResponseCache.Set(cacheKey, meta)
            return meta, nil
        }

        metaLogger.Debug("IMDb metadata lookup failed, falling back to Cinemeta: %v", err)

        meta, err = cinemetaMetaProvider(id, contentType, preferredLanguage, enableAltTitles, altTitleCountry)
        if err == nil && meta.Name != "" {
            metaResponseCache.Set(cacheKey, meta)
            return meta, nil
        }

        return MetaProviderResponse{}, fmt.Errorf("failed to find metadata for %s", id)
    })

    if err != nil {
        return MetaProviderResponse{}, err
    }

    finalMeta := res.(MetaProviderResponse)
    var season, episode string
    if len(parts) > 1 {
        season = parts[1]
    }
    if len(parts) > 2 {
        episode = parts[2]
    }
    finalMeta.Season = season
    finalMeta.Episode = episode

    return finalMeta, nil
}
