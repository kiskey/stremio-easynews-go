package addon

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type trackingBody struct {
	reader *bytes.Reader
	closed atomic.Bool
}

func newTrackingBody(s string) *trackingBody {
	return &trackingBody{reader: bytes.NewReader([]byte(s))}
}

func (b *trackingBody) Read(p []byte) (int, error) {
	return b.reader.Read(p)
}

func (b *trackingBody) Close() error {
	b.closed.Store(true)
	return nil
}

func TestFetchWithRetryClosesRetryResponseBeforeRetry(t *testing.T) {
	firstBody := newTrackingBody("rate limited")
	secondBody := newTrackingBody(`{"ok":true}`)
	var calls atomic.Int32

	client := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			switch calls.Add(1) {
			case 1:
				return &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Status:     "429 Too Many Requests",
					Header:     http.Header{"Retry-After": []string{"0"}},
					Body:       firstBody,
					Request:    req,
				}, nil
			default:
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     make(http.Header),
					Body:       secondBody,
					Request:    req,
				}, nil
			}
		}),
	}

	req, err := http.NewRequest(http.MethodGet, "https://example.invalid/meta", nil)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resp, err := fetchWithRetry(ctx, client, req)
	if err != nil {
		t.Fatalf("fetchWithRetry returned error: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected two attempts, got %d", calls.Load())
	}
	if !firstBody.closed.Load() {
		t.Fatal("retry response body must be closed before the next attempt")
	}
	if secondBody.closed.Load() {
		t.Fatal("successful response body must remain open for the caller")
	}

	drainAndClose(resp.Body)
	if !secondBody.closed.Load() {
		t.Fatal("successful response body was not closed by drainAndClose")
	}
}

func TestRetryAfterDuration(t *testing.T) {
	if got := retryAfterDuration("0", time.Second); got != 0 {
		t.Fatalf("Retry-After seconds parsing: got %v want 0", got)
	}
	if got := retryAfterDuration("invalid", 250*time.Millisecond); got != 250*time.Millisecond {
		t.Fatalf("invalid Retry-After should use fallback: got %v", got)
	}
}

func TestNewMetadataRequestHeaders(t *testing.T) {
	req, err := newMetadataRequest(context.Background(), "https://example.invalid/meta", "en-US")
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != http.MethodGet {
		t.Fatalf("unexpected method %q", req.Method)
	}
	if req.Header.Get("User-Agent") == "" {
		t.Fatal("metadata request must set User-Agent")
	}
	if got := req.Header.Get("Accept-Language"); got != "en-US" {
		t.Fatalf("Accept-Language = %q, want en-US", got)
	}
}

func TestTMDBAvailabilityUsesAtomicState(t *testing.T) {
	original := useTMDB.Load()
	defer useTMDB.Store(original)

	useTMDB.Store(true)

	var done atomic.Int32
	for i := 0; i < 64; i++ {
		go func(i int) {
			if i%2 == 0 {
				useTMDB.Store(false)
			} else {
				useTMDB.Store(true)
			}
			_ = useTMDB.Load()
			done.Add(1)
		}(i)
	}

	deadline := time.Now().Add(time.Second)
	for done.Load() != 64 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if done.Load() != 64 {
		t.Fatalf("concurrent TMDB state operations did not complete: %d/64", done.Load())
	}
}

var _ io.ReadCloser = (*trackingBody)(nil)

func TestBoundedCacheUsesLRUEviction(t *testing.T) {
	cache := NewBoundedCache[string, int](2, time.Minute)
	cache.Set("a", 1)
	cache.Set("b", 2)
	if _, ok := cache.Get("a"); !ok {
		t.Fatal("expected metadata cache hit")
	}
	cache.Set("c", 3)

	if _, ok := cache.Get("b"); ok {
		t.Fatal("least recently used metadata cache entry should be evicted")
	}
	if stats := cache.Stats(); stats.Evictions != 1 {
		t.Fatalf("expected one metadata cache eviction, got %+v", stats)
	}
}

func TestNewTMDBRequestPrefersBearerToken(t *testing.T) {
	req, err := newTMDBRequestWithCredentials(
		context.Background(),
		"/3/movie/11",
		url.Values{"language": []string{"en-US"}},
		"en-US",
		"read-access-token",
		"legacy-key",
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer read-access-token" {
		t.Fatalf("Authorization = %q, want bearer token", got)
	}
	if got := req.URL.Query().Get("api_key"); got != "" {
		t.Fatalf("Bearer mode must not put api_key in URL, got %q", got)
	}
	if got := req.URL.Query().Get("language"); got != "en-US" {
		t.Fatalf("language query = %q, want en-US", got)
	}
	if req.Header.Get("Accept") != "application/json" {
		t.Fatal("TMDB request must advertise application/json")
	}
}

func TestNewTMDBRequestLegacyAPIKeyFallback(t *testing.T) {
	req, err := newTMDBRequestWithCredentials(
		context.Background(),
		"/3/find/tt1234567",
		url.Values{"external_source": []string{"imdb_id"}},
		"en-US",
		"",
		"legacy-key",
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("legacy API-key mode must not set Authorization, got %q", got)
	}
	if got := req.URL.Query().Get("api_key"); got != "legacy-key" {
		t.Fatalf("api_key = %q, want legacy-key", got)
	}
	if got := req.URL.Query().Get("external_source"); got != "imdb_id" {
		t.Fatalf("external_source = %q, want imdb_id", got)
	}
}

func TestDecodeTMDBDetailsIncludesAppendedMetadata(t *testing.T) {
	payload := `{
		"title":"Spirited Away",
		"original_title":"千と千尋の神隠し",
		"release_date":"2001-07-20",
		"production_countries":[{"iso_3166_1":"JP"}],
		"genres":[{"id":16}],
		"alternative_titles":{"titles":[
			{"iso_3166_1":"US","title":"Spirited Away","type":""},
			{"iso_3166_1":"JP","title":"Sen to Chihiro no Kamikakushi","type":"Romaji"}
		]},
		"translations":{"translations":[
			{"iso_639_1":"fr","data":{"title":"Le Voyage de Chihiro"}},
			{"iso_639_1":"es","data":{"title":"El viaje de Chihiro"}}
		]}
	}`

	details, err := decodeTMDBDetails(strings.NewReader(payload), true)
	if err != nil {
		t.Fatal(err)
	}
	if details.DisplayTitle != "Spirited Away" || details.Year != 2001 || !details.IsAnimation {
		t.Fatalf("unexpected core details: %+v", details)
	}
	if len(details.OriginCountry) != 1 || details.OriginCountry[0] != "JP" {
		t.Fatalf("unexpected origin countries: %#v", details.OriginCountry)
	}
	if len(details.AlternativeTitles) != 2 {
		t.Fatalf("alternative title count = %d, want 2", len(details.AlternativeTitles))
	}
	if got := details.AlternativeTitles[1]; got.Country != "JP" || got.Type != "Romaji" {
		t.Fatalf("alternative provenance lost: %+v", got)
	}
	if got := details.TranslatedTitles["fr"]; got != "Le Voyage de Chihiro" {
		t.Fatalf("French translation = %q", got)
	}
}

func TestFilterTMDBAlternativeTitleItemsPreservesCountryAndType(t *testing.T) {
	items := []tmdbAlternativeTitle{
		{Country: "US", Title: "US Title", Type: ""},
		{Country: "JP", Title: "Romaji Title", Type: "Romaji"},
		{Country: "DE", Title: "German Title", Type: ""},
		{Country: "FR", Title: "French Title", Type: ""},
	}

	filtered := filterTMDBAlternativeTitleItems(items, "ja", "DE")
	if len(filtered) != 3 {
		t.Fatalf("filtered count = %d, want 3: %+v", len(filtered), filtered)
	}
	seen := make(map[string]tmdbAlternativeTitle)
	for _, item := range filtered {
		seen[item.Title] = item
	}
	if got := seen["Romaji Title"]; got.Country != "JP" || got.Type != "Romaji" {
		t.Fatalf("Romaji provenance not preserved: %+v", got)
	}
	if _, ok := seen["German Title"]; !ok {
		t.Fatal("explicit requested country title should be retained")
	}
	if _, ok := seen["French Title"]; ok {
		t.Fatal("unrequested unrelated country title should be filtered")
	}
}

func TestValidateTMDBResponseDisablesRejectedCredentials(t *testing.T) {
	original := useTMDB.Load()
	defer useTMDB.Store(original)
	useTMDB.Store(true)

	err := validateTMDBResponse(&http.Response{StatusCode: http.StatusUnauthorized}, "test")
	if err == nil {
		t.Fatal("expected authentication error")
	}
	if useTMDB.Load() {
		t.Fatal("TMDB integration should be disabled after authentication rejection")
	}
}
