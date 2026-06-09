package addon

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
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
// Thread-Safe Bounded Generic Cache Structure (With Double-Checked Locks)
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
		// Double-Checked Locking: Verify again under write lock to prevent race deletion of newly written fresh items
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

	// Memory Boundary Pruning: clear out oldest elements on capacity limits
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
	
	// Singleflight Groups (Locks parallel, duplicate queries into a single execution)
	tmdbIDSingleflight    singleflight.Group
	altTitlesSingleflight singleflight.Group
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
			if resp.StatusCode < 500 {
				return resp, nil
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

func resolveTMDBID(imdbID string) (int, bool, error) {
	if val, ok := imdbToTMDBIDCache.Get(imdbID); ok {
		return val.id, val.isMovie, nil
	}

	res, err, _ := tmdbIDSingleflight.Do(imdbID, func() (interface{}, error) {
		if val, ok := imdbToTMDBIDCache.Get(imdbID); ok {
			return val, nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), metaFetchTimeout)
		defer cancel()

		findURL := fmt.Sprintf("https://api.themoviedb.org/3/find/%s?api_key=%s&external_source=imdb_id", imdbID, tmdbAPIKey)
		req, _ := http.NewRequestWithContext(ctx, "GET", findURL, nil)
		
		resp, err := fetchWithRetry(ctx, http.DefaultClient, req)
		if err != nil {
			return tmdbIDMapping{}, err
		}
		defer func() {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}()

		if resp.StatusCode == 401 {
			useTMDB = false
			return tmdbIDMapping{}, fmt.Errorf("TMDB API key invalid")
		}
		if resp.StatusCode != 200 {
			return tmdbIDMapping{}, fmt.Errorf("TMDB find error: %d", resp.StatusCode)
		}

		var findData struct {
			MovieResults []struct {
				ID int `json:"id"`
			} `json:"movie_results"`
			TVResults []struct {
				ID int `json:"id"`
			} `json:"tv_results"`
		}
		if err := sonic.ConfigStd.NewDecoder(resp.Body).Decode(&findData); err != nil {
			return tmdbIDMapping{}, err
		}

		isMovie := len(findData.MovieResults) > 0
		isTV := len(findData.TVResults) > 0
		if !isMovie && !isTV {
			return tmdbIDMapping{}, nil
		}

		var tmdbID int
		if isMovie {
			tmdbID = findData.MovieResults[0].ID
		} else {
			tmdbID = findData.TVResults[0].ID
		}

		mapping := tmdbIDMapping{id: tmdbID, isMovie: isMovie}
		imdbToTMDBIDCache.Set(imdbID, mapping)
		return mapping, nil
	})

	if err != nil {
		return 0, false, err
	}
	mapping := res.(tmdbIDMapping)
	return mapping.id, mapping.isMovie, nil
}

// ---------------------------------------------------------------------------
// Dynamic Alternate Titles Resolution
// ---------------------------------------------------------------------------

func getTMDBAlternativeTitles(imdbID string) ([]string, error) {
	if !useTMDB {
		return nil, nil
	}

	if cached, ok := tmdbAltTitlesCache.Get(imdbID); ok {
		return cached, nil
	}

	res, err, _ := altTitlesSingleflight.Do(imdbID, func() (interface{}, error) {
		if cached, ok := tmdbAltTitlesCache.Get(imdbID); ok {
			return cached, nil
		}

		tmdbID, isMovie, err := resolveTMDBID(imdbID)
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
		
		resp, err := fetchWithRetry(ctx, http.DefaultClient, req)
		if err != nil {
			return nil, err
		}
		defer func() {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}()

		if resp.StatusCode != 200 {
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

		var rawList []string
		if len(data.Titles) > 0 {
			for _, item := range data.Titles {
				rawList = append(rawList, item.Title)
			}
		} else if len(data.Results) > 0 {
			for _, item := range data.Results {
				rawList = append(rawList, item.Title)
			}
		}

		var cleanList []string
		seen := make(map[string]bool)
		for _, t := range rawList {
			t = strings.TrimSpace(t)
			if t == "" || len(t) <= 1 {
				continue
			}
			if seen[t] {
				continue
			}
			if IsLatinString(t) {
				seen[t] = true
				cleanList = append(cleanList, t)
			}
		}

		tmdbAltTitlesCache.Set(imdbID, cleanList)
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

	tmdbLang := i18n.ConvertToTMDBLanguageCode(preferredLanguage)

	tmdbID, isMovie, err := resolveTMDBID(imdbID)
	if err != nil || tmdbID == 0 {
		return "", err
	}

	var detailsURL string
	if isMovie {
		detailsURL = fmt.Sprintf("https://api.themoviedb.org/3/movie/%d?api_key=%s&language=%s", tmdbID, tmdbAPIKey, tmdbLang)
	} else {
		detailsURL = fmt.Sprintf("https://api.themoviedb.org/3/tv/%d?api_key=%s&language=%s", tmdbID, tmdbAPIKey, tmdbLang)
	}

	ctx, cancel := context.WithTimeout(context.Background(), metaFetchTimeout)
	defer cancel()

	req2, _ := http.NewRequestWithContext(ctx, "GET", detailsURL, nil)
	resp2, err := fetchWithRetry(ctx, http.DefaultClient, req2)
	if err != nil {
		return "", err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp2.Body)
		resp2.Body.Close()
	}()

	if resp2.StatusCode != 200 {
		return "", fmt.Errorf("TMDB details error: %d", resp2.StatusCode)
	}

	var details struct {
		Title string `json:"title"`
		Name  string `json:"name"`
	}
	if err := sonic.ConfigStd.NewDecoder(resp2.Body).Decode(&details); err != nil {
		return "", err
	}

	if details.Title != "" {
		return details.Title, nil
	}
	if details.Name != "" {
		return details.Name, nil
	}

	var transURL string
	if isMovie {
		transURL = fmt.Sprintf("https://api.themoviedb.org/3/movie/%d/translations?api_key=%s", tmdbID, tmdbAPIKey)
	} else {
		transURL = fmt.Sprintf("https://api.themoviedb.org/3/tv/%d/translations?api_key=%s", tmdbID, tmdbAPIKey)
	}

	req3, _ := http.NewRequestWithContext(ctx, "GET", transURL, nil)
	resp3, err := fetchWithRetry(ctx, http.DefaultClient, req3)
	if err != nil {
		return "", err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp3.Body)
		resp3.Body.Close()
	}()

	var transData struct {
		Translations []struct {
			ISO639_1 string `json:"iso_639_1"`
			Data     struct {
				Title string `json:"title"`
				Name  string `json:"name"`
			} `json:"data"`
		} `json:"translations"`
	}
	if err := sonic.ConfigStd.NewDecoder(resp3.Body).Decode(&transData); err != nil {
		return "", err
	}

	for _, t := range transData.Translations {
		if t.ISO639_1 == tmdbLang {
			if isMovie && t.Data.Title != "" {
				return t.Data.Title, nil
			}
			if !isMovie && t.Data.Name != "" {
				return t.Data.Name, nil
			}
		}
	}

	return "", nil
}

// ---------------------------------------------------------------------------
// IMDb Metadata Lookup Provider
// ---------------------------------------------------------------------------

func imdbMetaProvider(id, preferredLanguage string) (MetaProviderResponse, error) {
	parts := strings.Split(id, ":")
	tt := parts[0]
	var season, episode string
	if len(parts) > 1 {
		season = parts[1]
	}
	if len(parts) > 2 {
		episode = parts[2]
	}

	ctx, cancel := context.WithTimeout(context.Background(), metaFetchTimeout)
	defer cancel()

	url := fmt.Sprintf("https://v2.sg.media-imdb.com/suggestion/t/%s.json", tt)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	
	resp, err := fetchWithRetry(ctx, http.DefaultClient, req)
	if err != nil {
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
		return MetaProviderResponse{}, fmt.Errorf("no IMDb match for %s", tt)
	}

	originalName := item.L
	alternatives := GetAlternativeTitles(originalName)

	if useTMDB {
		altTitles, err := getTMDBAlternativeTitles(tt)
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
	}, nil
}

// ---------------------------------------------------------------------------
// Cinemeta Metadata Lookup Provider (Fallback)
// ---------------------------------------------------------------------------

func cinemetaMetaProvider(id, contentType, preferredLanguage string) (MetaProviderResponse, error) {
	parts := strings.Split(id, ":")
	tt := parts[0]
	var season, episode string
	if len(parts) > 1 {
		season = parts[1]
	}
	if len(parts) > 2 {
		episode = parts[2]
	}

	ctx, cancel := context.WithTimeout(context.Background(), metaFetchTimeout)
	defer cancel()

	url := fmt.Sprintf("https://v3-cinemeta.strem.io/meta/%s/%s.json", contentType, tt)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	
	resp, err := fetchWithRetry(ctx, http.DefaultClient, req)
	if err != nil {
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

	alternatives := GetAlternativeTitles(name)

	if useTMDB {
		altTitles, err := getTMDBAlternativeTitles(tt)
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
	}, nil
}

// ---------------------------------------------------------------------------
// Public Metadata Gateway Interface
// ---------------------------------------------------------------------------

func PublicMetaProvider(id, contentType, preferredLanguage string) (MetaProviderResponse, error) {
	meta, err := imdbMetaProvider(id, preferredLanguage)
	if err == nil && meta.Name != "" {
		return meta, nil
	}

	metaLogger.Debug("IMDb metadata lookup failed, falling back to Cinemeta: %v", err)

	meta, err = cinemetaMetaProvider(id, contentType, preferredLanguage)
	if err == nil && meta.Name != "" {
		return meta, nil
	}

	return MetaProviderResponse{}, fmt.Errorf("failed to find metadata for %s", id)
}
