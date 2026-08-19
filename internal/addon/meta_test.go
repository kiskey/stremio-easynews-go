package addon

import (
	"bytes"
	"context"
	"io"
	"net/http"
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
