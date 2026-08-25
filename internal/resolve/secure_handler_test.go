package resolve

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type resolverRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f resolverRoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type countingReadCloser struct {
	reader *bytes.Reader
	read   int64
	closed atomic.Bool
}

func newCountingReadCloser(size int) *countingReadCloser {
	return &countingReadCloser{reader: bytes.NewReader(bytes.Repeat([]byte{'x'}, size))}
}

func (b *countingReadCloser) Read(p []byte) (int, error) {
	n, err := b.reader.Read(p)
	atomic.AddInt64(&b.read, int64(n))
	return n, err
}

func (b *countingReadCloser) Close() error {
	b.closed.Store(true)
	return nil
}

func testResolveClient(fn resolverRoundTripperFunc) *http.Client {
	return &http.Client{
		Transport: fn,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func testResolvedTarget() ResolvedTarget {
	return ResolvedTarget{
		CleanUrl:   "https://members.easynews.com/dl/file.mkv",
		AuthHeader: "Basic dXNlcjpwYXNz",
	}
}

func TestCloseProbeBodyIsBounded(t *testing.T) {
	body := newCountingReadCloser(1024 * 1024)
	closeProbeBody(body)
	if got := atomic.LoadInt64(&body.read); got > resolverProbeBodyLimit {
		t.Fatalf("probe body read %d bytes, limit is %d", got, resolverProbeBodyLimit)
	}
	if !body.closed.Load() {
		t.Fatal("probe body was not closed")
	}
}

func TestBuildResolverProbeRequestUsesAuthAndOneByteRange(t *testing.T) {
	req, err := buildResolverProbeRequest(context.Background(), testResolvedTarget())
	if err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Authorization"); got != "Basic dXNlcjpwYXNz" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := req.Header.Get("Range"); got != "bytes=0-0" {
		t.Fatalf("Range = %q", got)
	}
	if req.Method != http.MethodGet {
		t.Fatalf("probe method = %q", req.Method)
	}
}

func TestResolveEasynewsRedirectSuccess(t *testing.T) {
	client := testResolveClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{"https://cdn.example/video.mkv"}},
			Body:       io.NopCloser(strings.NewReader("redirect")),
			Request:    req,
		}, nil
	})

	result, err := resolveEasynewsRedirect(context.Background(), client, testResolvedTarget(), 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.TargetURL != "https://cdn.example/video.mkv" || result.StatusCode != http.StatusFound || result.Attempts != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestResolveEasynewsRedirectResolvesRelativeLocation(t *testing.T) {
	client := testResolveClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTemporaryRedirect,
			Header:     http.Header{"Location": []string{"/cdn/video.mkv"}},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	})

	result, err := resolveEasynewsRedirect(context.Background(), client, testResolvedTarget(), 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.TargetURL != "https://members.easynews.com/cdn/video.mkv" {
		t.Fatalf("relative redirect resolved to %q", result.TargetURL)
	}
}

func TestResolveEasynewsRedirectRetriesDirectContentThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	client := testResolveClient(func(req *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return &http.Response{
				StatusCode: http.StatusPartialContent,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("x")),
				Request:    req,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{"https://cdn.example/video.mkv"}},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	})

	result, err := resolveEasynewsRedirect(context.Background(), client, testResolvedTarget(), 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.Attempts != 2 || calls.Load() != 2 {
		t.Fatalf("expected internal retry, result=%+v calls=%d", result, calls.Load())
	}
}

func TestResolveEasynewsRedirectRetriesTransientServerFailure(t *testing.T) {
	var calls atomic.Int32
	client := testResolveClient(func(req *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("temporary")),
				Request:    req,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{"https://cdn.example/video.mkv"}},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	})

	result, err := resolveEasynewsRedirect(context.Background(), client, testResolvedTarget(), 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.Attempts != 2 {
		t.Fatalf("expected 2 attempts, got %+v", result)
	}
}

func TestResolveEasynewsRedirectAuthFailureDoesNotRetry(t *testing.T) {
	var calls atomic.Int32
	client := testResolveClient(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("unauthorized")),
			Request:    req,
		}, nil
	})

	_, err := resolveEasynewsRedirect(context.Background(), client, testResolvedTarget(), 3, 0)
	if err == nil {
		t.Fatal("expected authentication error")
	}
	var upstreamErr *upstreamResolveError
	if !errors.As(err, &upstreamErr) {
		t.Fatalf("unexpected error type: %T %v", err, err)
	}
	if upstreamErr.StatusCode != http.StatusUnauthorized || upstreamErr.Attempts != 1 || calls.Load() != 1 {
		t.Fatalf("auth failure should not retry: %+v calls=%d", upstreamErr, calls.Load())
	}
}

func TestResolveEasynewsRedirectDirectContentFailsClosed(t *testing.T) {
	client := testResolveClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("media")),
			Request:    req,
		}, nil
	})

	_, err := resolveEasynewsRedirect(context.Background(), client, testResolvedTarget(), 2, 0)
	if err == nil || !strings.Contains(err.Error(), "direct content") {
		t.Fatalf("expected fail-closed direct-content error, got %v", err)
	}
}

func TestNormalizeResolverRedirectRejectsHTTPDowngrade(t *testing.T) {
	if _, err := normalizeResolverRedirect("https://members.easynews.com/file", "http://cdn.example/file"); err == nil {
		t.Fatal("expected HTTP redirect downgrade to be rejected")
	}
}

func TestResolveEasynewsRedirectHonorsContextDeadline(t *testing.T) {
	client := testResolveClient(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	_, err := resolveEasynewsRedirect(ctx, client, testResolvedTarget(), 2, time.Second)
	if err == nil {
		t.Fatal("expected context deadline error")
	}
}
