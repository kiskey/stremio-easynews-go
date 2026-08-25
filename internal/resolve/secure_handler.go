package resolve

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kiskey/stremio-easynews-go/internal/shared"
)

const (
	resolverProbeBodyLimit = 4 * 1024
	resolverUserAgent      = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

type upstreamResolveResult struct {
	TargetURL   string
	StatusCode  int
	StatusCodes []int
	Attempts    int
	Duration    time.Duration
}

type upstreamResolveError struct {
	StatusCode  int
	StatusCodes []int
	Kind        string
	Attempts    int
	Duration    time.Duration
	Err         error
}

func (e *upstreamResolveError) Error() string {
	if e == nil {
		return "resolver upstream error"
	}
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Kind, e.Err)
	}
	if e.StatusCode != 0 {
		return fmt.Sprintf("%s: upstream status %d", e.Kind, e.StatusCode)
	}
	return e.Kind
}

func (e *upstreamResolveError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func resolverPayloadFingerprint(payload string) string {
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:6])
}

func preview(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}

func hostnameForLog(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "invalid"
	}
	return u.Hostname()
}

func positiveIntEnv(name string, fallback, maxValue int) int {
	value := shared.ParseIntEnv(name, fallback)
	if value < 1 {
		return fallback
	}
	if maxValue > 0 && value > maxValue {
		return maxValue
	}
	return value
}

func resolverRetryDelay() time.Duration {
	return time.Duration(positiveIntEnv("RESOLVE_RETRY_DELAY_MS", 250, 5000)) * time.Millisecond
}

func isResolverRedirectStatus(status int) bool {
	switch status {
	case http.StatusMovedPermanently,
		http.StatusFound,
		http.StatusSeeOther,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect:
		return true
	default:
		return false
	}
}

func normalizeResolverRedirect(baseRawURL, location string) (string, error) {
	location = strings.TrimSpace(location)
	if location == "" {
		return "", errors.New("redirect location is empty")
	}
	baseURL, err := url.Parse(baseRawURL)
	if err != nil {
		return "", fmt.Errorf("invalid upstream base URL: %w", err)
	}
	locationURL, err := url.Parse(location)
	if err != nil {
		return "", fmt.Errorf("invalid redirect location: %w", err)
	}
	resolved := baseURL.ResolveReference(locationURL)
	if !strings.EqualFold(resolved.Scheme, "https") || resolved.Hostname() == "" {
		return "", errors.New("redirect target must use HTTPS")
	}
	if resolved.User != nil {
		return "", errors.New("redirect target must not contain embedded credentials")
	}
	return resolved.String(), nil
}

// closeProbeBody intentionally reads only a tiny bounded prefix. The historical
// resolver drained the entire response body so a connection could be reused.
// If Easynews ignored Range: bytes=0-0 and returned the media file directly,
// that could make the addon download the movie before responding to the player.
func closeProbeBody(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.CopyN(io.Discard, body, resolverProbeBodyLimit)
	_ = body.Close()
}

func buildResolverProbeRequest(ctx context.Context, target ResolvedTarget) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.CleanUrl, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", target.AuthHeader)
	req.Header.Set("Range", "bytes=0-0")
	req.Header.Set("User-Agent", resolverUserAgent)
	req.Header.Set("Accept", "*/*")
	return req, nil
}

func shouldRetryResolverStatus(status int, hasLocation bool) bool {
	if status == http.StatusTooManyRequests || status >= http.StatusInternalServerError {
		return true
	}
	if isResolverRedirectStatus(status) && !hasLocation {
		return true
	}
	// A direct 2xx media response is not safe to hand to the player because the
	// Authorization header exists only on the server-side probe. Retry once in
	// case the upstream/CDN assignment is still warming, then fail explicitly.
	return status >= 200 && status < 300 && !hasLocation
}

func resolveEasynewsRedirect(ctx context.Context, client *http.Client, target ResolvedTarget, maxAttempts int, retryDelay time.Duration) (upstreamResolveResult, error) {
	if client == nil {
		return upstreamResolveResult{}, &upstreamResolveError{Kind: "resolver client unavailable"}
	}
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	started := time.Now()
	var lastStatus int
	var lastErr error
	var lastKind string
	attemptsUsed := 0
	statusCodes := make([]int, 0, maxAttempts)

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		attemptsUsed = attempt
		if err := ctx.Err(); err != nil {
			return upstreamResolveResult{}, &upstreamResolveError{
				StatusCode: lastStatus,
				Kind:       "resolver context ended",
				Attempts:   attempt - 1,
				Duration:   time.Since(started),
				Err:        err,
			}
		}

		req, err := buildResolverProbeRequest(ctx, target)
		if err != nil {
			return upstreamResolveResult{}, &upstreamResolveError{
				Kind:     "failed to build upstream request",
				Attempts: attempt - 1,
				Duration: time.Since(started),
				Err:      err,
			}
		}

		resp, err := client.Do(req)
		if err != nil {
			statusCodes = append(statusCodes, 0)
			lastErr = err
			lastKind = "upstream network failure"
			if attempt < maxAttempts {
				if err := waitResolverRetry(ctx, retryDelay); err != nil {
					return upstreamResolveResult{}, &upstreamResolveError{Kind: "resolver context ended", Attempts: attempt, Duration: time.Since(started), Err: err}
				}
				continue
			}
			break
		}

		lastStatus = resp.StatusCode
		statusCodes = append(statusCodes, resp.StatusCode)
		location := strings.TrimSpace(resp.Header.Get("Location"))
		closeProbeBody(resp.Body)

		if isResolverRedirectStatus(resp.StatusCode) && location != "" {
			resolvedLocation, err := normalizeResolverRedirect(target.CleanUrl, location)
			if err != nil {
				return upstreamResolveResult{}, &upstreamResolveError{
					StatusCode: resp.StatusCode,
					Kind:       "invalid upstream redirect",
					Attempts:   attempt,
					Duration:   time.Since(started),
					Err:        err,
				}
			}
			return upstreamResolveResult{
				TargetURL:   resolvedLocation,
				StatusCode:  resp.StatusCode,
				StatusCodes: append([]int(nil), statusCodes...),
				Attempts:    attempt,
				Duration:    time.Since(started),
			}, nil
		}

		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return upstreamResolveResult{}, &upstreamResolveError{
				StatusCode:  resp.StatusCode,
				StatusCodes: append([]int(nil), statusCodes...),
				Kind:        "Easynews authentication rejected",
				Attempts:    attempt,
				Duration:    time.Since(started),
			}
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 && location == "" {
			lastKind = "Easynews returned direct content without a redirect"
		} else if isResolverRedirectStatus(resp.StatusCode) && location == "" {
			lastKind = "Easynews redirect response omitted Location"
		} else {
			lastKind = fmt.Sprintf("unexpected Easynews status %d", resp.StatusCode)
		}

		if attempt < maxAttempts && shouldRetryResolverStatus(resp.StatusCode, location != "") {
			if err := waitResolverRetry(ctx, retryDelay); err != nil {
				return upstreamResolveResult{}, &upstreamResolveError{StatusCode: lastStatus, Kind: "resolver context ended", Attempts: attempt, Duration: time.Since(started), Err: err}
			}
			continue
		}
		break
	}

	return upstreamResolveResult{}, &upstreamResolveError{
		StatusCode:  lastStatus,
		StatusCodes: append([]int(nil), statusCodes...),
		Kind:        lastKind,
		Attempts:    attemptsUsed,
		Duration:    time.Since(started),
		Err:         lastErr,
	}
}

func waitResolverRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func redirectPlayer(c *gin.Context, targetURL string) {
	c.Header("Location", targetURL)
	c.Header("Cache-Control", "private, no-store")
	c.Header("Content-Length", "0")
	c.Status(http.StatusFound)
}

func resolverClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: timeout,
			ExpectContinueTimeout: 1 * time.Second,
			ForceAttemptHTTP2:     true,
		},
	}
}

// CreateSecureResolveHandler resolves the authenticated Easynews origin to a
// geography-aware CDN URL. Batch 9 makes cold-cache resolution player-friendly:
// bounded probe bodies, internal transient retry, explicit status handling,
// HEAD-safe 302 redirects, and non-secret timing/status diagnostics.
func CreateSecureResolveHandler(logger shared.Logger) gin.HandlerFunc {
	timeout := time.Duration(positiveIntEnv("RESOLVE_TIMEOUT_MS", 20000, 120000)) * time.Millisecond
	maxAttempts := positiveIntEnv("RESOLVE_ATTEMPTS", 2, 3)
	retryDelay := resolverRetryDelay()
	client := resolverClient(timeout)

	return func(c *gin.Context) {
		payload := c.Param("payload")
		filename := c.Param("filename")
		fingerprint := resolverPayloadFingerprint(payload)
		requestStarted := time.Now()

		c.Header("Cache-Control", "private, no-store")
		logger.Info("Resolve: method=%s token=%s filename=%q", c.Request.Method, fingerprint, preview(filename, 120))

		if cachedURL, ok := GetSecureCachedResolvedURL(payload); ok {
			logger.Info("Resolve cache HIT: method=%s token=%s targetHost=%s elapsedMs=%d",
				c.Request.Method, fingerprint, hostnameForLog(cachedURL), time.Since(requestStarted).Milliseconds())
			redirectPlayer(c, cachedURL)
			return
		}

		target, err := ParseSecureResolvePayload(payload)
		if err != nil {
			logger.Warn("Resolve: rejected token=%s: %v", fingerprint, err)
			if re, ok := err.(*ResolveError); ok {
				c.String(re.Status, re.Message)
			} else {
				c.String(http.StatusBadRequest, "Invalid request")
			}
			return
		}

		logger.Info("Resolve cache MISS: method=%s token=%s upstreamHost=%s", c.Request.Method, fingerprint, hostnameForLog(target.CleanUrl))

		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()

		result, err := resolveEasynewsRedirect(ctx, client, target, maxAttempts, retryDelay)
		if err != nil {
			var upstreamErr *upstreamResolveError
			if errors.As(err, &upstreamErr) {
				logger.Error("Resolve FAILED: token=%s upstreamHost=%s status=%d statusPath=%v attempts=%d elapsedMs=%d reason=%s",
					fingerprint, hostnameForLog(target.CleanUrl), upstreamErr.StatusCode, upstreamErr.StatusCodes, upstreamErr.Attempts,
					upstreamErr.Duration.Milliseconds(), upstreamErr.Kind)
				switch upstreamErr.StatusCode {
				case http.StatusUnauthorized, http.StatusForbidden:
					c.String(http.StatusBadGateway, "Easynews rejected stream authentication")
				case http.StatusTooManyRequests:
					c.String(http.StatusServiceUnavailable, "Easynews is temporarily rate limited")
				default:
					c.String(http.StatusBadGateway, "Unable to resolve Easynews stream")
				}
				return
			}
			logger.Error("Resolve FAILED: token=%s upstreamHost=%s elapsedMs=%d: %v",
				fingerprint, hostnameForLog(target.CleanUrl), time.Since(requestStarted).Milliseconds(), err)
			c.String(http.StatusBadGateway, "Unable to resolve Easynews stream")
			return
		}

		SetSecureCachedResolvedURL(payload, result.TargetURL)
		logger.Info("Resolve SUCCESS: method=%s token=%s upstreamStatus=%d statusPath=%v attempts=%d resolveMs=%d totalMs=%d targetHost=%s",
			c.Request.Method, fingerprint, result.StatusCode, result.StatusCodes, result.Attempts, result.Duration.Milliseconds(),
			time.Since(requestStarted).Milliseconds(), hostnameForLog(result.TargetURL))
		redirectPlayer(c, result.TargetURL)
	}
}
