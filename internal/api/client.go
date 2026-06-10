package api

import (
	"context"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/bytedance/sonic"
	"github.com/kiskey/stremio-easynews-go/internal/shared"
)

var apiLogger = shared.CreateLogger("API", "")

// ---------------------------------------------------------------------------
// Shared Network Connection Pooling (Reused across dynamic API instances)
// ---------------------------------------------------------------------------

var sharedTransport = &http.Transport{
	MaxIdleConns:        100,
	MaxIdleConnsPerHost: 100,
	IdleConnTimeout:     90 * time.Second,
}

// ---------------------------------------------------------------------------
// Process-level shared cache (Optimized with pointers to bypass GC heap escapes)
// ---------------------------------------------------------------------------

var (
	sharedCache     = make(map[string]*cacheEntry)
	sharedCacheMu   sync.RWMutex
	maxCacheEntries = shared.ParseIntEnv("MAX_CACHE_ENTRIES", 1000)
	cacheTTL        = time.Duration(shared.ParseIntEnv("CACHE_TTL", 24)) * time.Hour
)

type cacheEntry struct {
	data      EasynewsSearchResponse
	timestamp int64 // UnixNano
}

// ---------------------------------------------------------------------------
// EasynewsAPI Client Definition
// ---------------------------------------------------------------------------

type EasynewsAPI struct {
	baseURL  string
	username string
	password string
	credKey  string
	client   *http.Client
}

// NewEasynewsAPI instantiates a thread-safe Easynews API client.
func NewEasynewsAPI(username, password string) (*EasynewsAPI, error) {
	if username == "" || password == "" {
		return nil, fmt.Errorf("username and password are required")
	}
	return &EasynewsAPI{
		baseURL:  "https://members.easynews.com",
		username: username,
		password: password,
		credKey:  credFingerprint(username, password),
		client: &http.Client{
			Timeout:   20 * time.Second,
			Transport: sharedTransport, // Reuses global TCP pool
		},
	}, nil
}

// credFingerprint computes an FNV-1a hash representing credentials to safely scope the cache.
func credFingerprint(username, password string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(username + ":" + password))
	return fmt.Sprintf("%x", h.Sum32())
}

// ClearCache resets the shared in-memory search cache.
func ClearCache() {
	sharedCacheMu.Lock()
	sharedCache = make(map[string]*cacheEntry)
	sharedCacheMu.Unlock()
}

// ---------------------------------------------------------------------------
// Cache Management Helpers
// ---------------------------------------------------------------------------

func (api *EasynewsAPI) cacheKey(opts SearchOptions) string {
	maxResults := opts.MaxResults
	if maxResults == 0 {
		maxResults = shared.ParseIntEnv("MAX_RESULTS_PER_PAGE", 250)
	}

	return api.credKey + "|q=" + opts.Query +
		"|p=" + strconv.Itoa(opts.PageNr) +
		"|m=" + strconv.Itoa(maxResults) +
		"|s1=" + opts.Sort1 + opts.Sort1Direction +
		"|s2=" + opts.Sort2 + opts.Sort2Direction +
		"|s3=" + opts.Sort3 + opts.Sort3Direction
}

func (api *EasynewsAPI) getFromCache(key string) *EasynewsSearchResponse {
	sharedCacheMu.RLock()
	entry, ok := sharedCache[key]
	sharedCacheMu.RUnlock()
	if !ok {
		return nil
	}

	if time.Now().UnixNano()-entry.timestamp > cacheTTL.Nanoseconds() {
		sharedCacheMu.Lock()
		delete(sharedCache, key)
		sharedCacheMu.Unlock()
		return nil
	}
	return &entry.data
}

func (api *EasynewsAPI) setCache(key string, data EasynewsSearchResponse) {
	sharedCacheMu.Lock()
	defer sharedCacheMu.Unlock()

	sharedCache[key] = &cacheEntry{
		data:      data,
		timestamp: time.Now().UnixNano(),
	}

	// Memory boundary defense: Evict oldest half when cache capacity is exceeded
	if len(sharedCache) > maxCacheEntries {
		type kv struct {
			k string
			t int64
		}
		entries := make([]kv, 0, len(sharedCache))
		for k, v := range sharedCache {
			entries = append(entries, kv{k, v.timestamp})
		}

		sort.Slice(entries, func(i, j int) bool {
			return entries[i].t < entries[j].t
		})

		half := len(entries) / 2
		for i := 0; i < half; i++ {
			delete(sharedCache, entries[i].k)
		}
	}
}

// ---------------------------------------------------------------------------
// Easynews API Execution Methods
// ---------------------------------------------------------------------------

// Search queries a single advanced page of results from Easynews.
func (api *EasynewsAPI) Search(opts SearchOptions) (EasynewsSearchResponse, error) {
	if opts.Query == "" {
		return EasynewsSearchResponse{}, fmt.Errorf("query parameter is required")
	}
	if opts.PageNr <= 0 {
		opts.PageNr = 1
	}
	if opts.MaxResults <= 0 {
		opts.MaxResults = shared.ParseIntEnv("MAX_RESULTS_PER_PAGE", 250)
	}

	// Apply default Solr sorts (RE-ALIGNED WITH THE ORIGINAL NODE.JS IMPLEMENTATION)
	if opts.Sort1 == "" {
		opts.Sort1 = "relevance"
		opts.Sort1Direction = "-"
	}
	if opts.Sort2 == "" {
		opts.Sort2 = "dsize"
		opts.Sort2Direction = "-"
	}
	if opts.Sort3 == "" {
		opts.Sort3 = "dtime"
		opts.Sort3Direction = "-"
	}

	cacheKey := api.cacheKey(opts)
	if cached := api.getFromCache(cacheKey); cached != nil {
		return *cached, nil
	}

	u, _ := url.Parse(api.baseURL + "/2.0/search/solr-search/advanced")
	q := u.Query()
	q.Set("st", "adv")
	q.Set("sb", "1")
	q.Set("fex", "m4v,3gp,mov,divx,xvid,wmv,avi,mpg,mpeg,mp4,mkv,avc,flv,webm")
	q.Set("fty[]", "VIDEO")
	q.Set("spamf", "1")
	q.Set("u", "1")
	q.Set("gx", "1")
	q.Set("pno", strconv.Itoa(opts.PageNr))
	q.Set("sS", "3") // Search Scope: 3 = Subject + Filename (Node.js reference parity)
	q.Set("s1", opts.Sort1)
	q.Set("s1d", opts.Sort1Direction)
	q.Set("s2", opts.Sort2)
	q.Set("s2d", opts.Sort2Direction)
	q.Set("s3", opts.Sort3)
	q.Set("s3d", opts.Sort3Direction)
	q.Set("pby", strconv.Itoa(opts.MaxResults))
	q.Set("safeO", "0")
	q.Set("gps", opts.Query)
	u.RawQuery = q.Encode()

	apiLogger.Info("Solr: Querying Easynews URL: '%s...'", u.String()[:100])

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return EasynewsSearchResponse{}, err
	}
	req.Header.Set("Authorization", CreateBasic(api.username, api.password))
	
	// Set premium User-Agent to prevent WAF blocks
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := api.client.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return EasynewsSearchResponse{}, fmt.Errorf("search request for '%s' timed out after 20 seconds", opts.Query)
		}
		return EasynewsSearchResponse{}, err
	}
	defer func() {
		// Drain body to make sure connection is available for reuse in transport pool
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusUnauthorized {
		apiLogger.Error("Solr: Request returned 401 Unauthorized for user: %s", api.username)
		return EasynewsSearchResponse{}, fmt.Errorf("authentication failed: invalid username or password")
	}
	if resp.StatusCode != http.StatusOK {
		apiLogger.Error("Solr: Request returned failed status code: %d (%s) for query: '%s'", resp.StatusCode, resp.Status, opts.Query)
		return EasynewsSearchResponse{}, fmt.Errorf("failed to fetch search results of query '%s': %d %s", opts.Query, resp.StatusCode, resp.Status)
	}

	var result EasynewsSearchResponse
	if err := sonic.ConfigStd.NewDecoder(resp.Body).Decode(&result); err != nil {
		return EasynewsSearchResponse{}, fmt.Errorf("failed to decode response: %w", err)
	}

	apiLogger.Info("Solr: Successfully returned %d files (out of %d total matched in Solr) for query '%s'", len(result.Data), result.Results, opts.Query)

	api.setCache(cacheKey, result)
	return result, nil
}

// SearchAll handles iterative pagination across Easynews search pages up to total thresholds.
func (api *EasynewsAPI) SearchAll(opts SearchOptions) (EasynewsSearchResponse, error) {
	totalMaxResults := shared.ParseIntEnv("TOTAL_MAX_RESULTS", 500)
	maxPages := shared.ParseIntEnv("MAX_PAGES", 10)
	maxResultsPerPage := shared.ParseIntEnv("MAX_RESULTS_PER_PAGE", 250)

	var allData []FileData
	var res EasynewsSearchResponse

	pageNr := 1
	pageCount := 0
	var previousFirstHash string

	apiLogger.Info("SearchAll: Executing fanned pagination searchAll for: '%s' (max results: %d, max pages: %d)", opts.Query, totalMaxResults, maxPages)

	for pageCount < maxPages {
		remaining := totalMaxResults - len(allData)
		if remaining <= 0 {
			apiLogger.Info("SearchAll: Reached max requested results limit (%d), stopping pagination.", totalMaxResults)
			break
		}

		// Keep page size constant to prevent Solr offset corruption (Offset = (PageNumber - 1) * PageSize)
		pageOpts := opts
		pageOpts.PageNr = pageNr
		pageOpts.MaxResults = maxResultsPerPage

		apiLogger.Info("SearchAll: Fetching page %d (fixed page size: %d) for query '%s'", pageNr, maxResultsPerPage, opts.Query)

		pageResult, err := api.Search(pageOpts)
		if err != nil {
			apiLogger.Error("SearchAll: Error fetching page %d: %v", pageNr, err)
			if len(allData) > 0 {
				apiLogger.Info("SearchAll: Returning %d partial results gathered before the error.", len(allData))
				res.Data = allData
				return res, nil
			}
			return res, err
		}

		res = pageResult
		pageCount++
		newData := pageResult.Data

		if len(newData) == 0 {
			apiLogger.Info("SearchAll: Received empty dataset on page %d, ending pagination.", pageNr)
			break
		}

		if previousFirstHash != "" && newData[0].Zero == previousFirstHash {
			apiLogger.Info("SearchAll: Duplicate data detected on page %d (first item hash matches previous page), stopping pagination.", pageNr)
			break
		}
		previousFirstHash = newData[0].Zero

		allData = append(allData, newData...)
		
		percent := (len(allData) * 100) / totalMaxResults
		apiLogger.Info("SearchAll: Progress - %d/%d unique files indexed (%d%%)", len(allData), totalMaxResults, percent)

		if len(allData) >= totalMaxResults {
			if len(allData) > totalMaxResults {
				allData = allData[:totalMaxResults]
			}
			apiLogger.Info("SearchAll: Reached max requested results limit (%d), stopping pagination.", totalMaxResults)
			break
		}

		// If we fetched fewer items than the constant page size, Solr has no more matching items
		if len(newData) < maxResultsPerPage {
			apiLogger.Info("SearchAll: Fetched %d items (less than page size %d), ending pagination.", len(newData), maxResultsPerPage)
			break
		}

		pageNr++
	}

	res.Data = allData
	apiLogger.Info("SearchAll complete: gathered %d total unique results across %d pages for query '%s'", len(allData), pageCount, opts.Query)
	return res, nil
}
