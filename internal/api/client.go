package api

import (
	"context"
	"fmt"
	"hash/fnv"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/bytedance/sonic"
	"github.com/kiskey/stremio-easynews-go/internal/shared"
)

// ---------------------------------------------------------------------------
// Process-level shared cache (Shared across all EasynewsAPI client instances)
// ---------------------------------------------------------------------------

var (
	sharedCache     = make(map[string]cacheEntry)
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
			Timeout: 20 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
			},
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
	sharedCache = make(map[string]cacheEntry)
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

	// High-performance direct string construction to bypass reflection-based serialization
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

	sharedCache[key] = cacheEntry{
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

	// Apply default Solr sorts if none provided
	if opts.Sort1 == "" {
		opts.Sort1 = "dsize"
		opts.Sort1Direction = "-"
	}
	if opts.Sort2 == "" {
		opts.Sort2 = "relevance"
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
	q.Set("sS", "3")
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

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return EasynewsSearchResponse{}, err
	}
	req.Header.Set("Authorization", CreateBasic(api.username, api.password))

	resp, err := api.client.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return EasynewsSearchResponse{}, fmt.Errorf("search request for '%s' timed out after 20 seconds", opts.Query)
		}
		return EasynewsSearchResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return EasynewsSearchResponse{}, fmt.Errorf("authentication failed: invalid username or password")
	}
	if resp.StatusCode != http.StatusOK {
		return EasynewsSearchResponse{}, fmt.Errorf("failed to fetch search results of query '%s': %d %s", opts.Query, resp.StatusCode, resp.Status)
	}

	var result EasynewsSearchResponse
	if err := sonic.ConfigStd.NewDecoder(resp.Body).Decode(&result); err != nil {
		return EasynewsSearchResponse{}, fmt.Errorf("failed to decode response: %w", err)
	}

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

	for pageCount < maxPages {
		remaining := totalMaxResults - len(allData)
		if remaining <= 0 {
			break
		}

		optimalPageSize := remaining
		if optimalPageSize > maxResultsPerPage {
			optimalPageSize = maxResultsPerPage
		}

		pageOpts := opts
		pageOpts.PageNr = pageNr
		pageOpts.MaxResults = optimalPageSize

		pageResult, err := api.Search(pageOpts)
		if err != nil {
			if len(allData) > 0 {
				res.Data = allData
				return res, nil
			}
			return res, err
		}

		res = pageResult
		pageCount++
		newData := pageResult.Data

		if len(newData) == 0 {
			break
		}

		// Exact duplicate protection: exit pagination loop if first item matches the previous page
		if previousFirstHash != "" && newData[0].Zero == previousFirstHash {
			break
		}
		previousFirstHash = newData[0].Zero

		allData = append(allData, newData...)

		if len(allData) >= totalMaxResults {
			if len(allData) > totalMaxResults {
				allData = allData[:totalMaxResults]
			}
			break
		}

		pageNr++
	}

	res.Data = allData
	return res, nil
}
