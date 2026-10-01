package scraper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"time"
)

// defaultUserAgent identifies this project (and how to reach us) to shops
// whose storefront JSON endpoints we read directly instead of driving a
// headless browser.
const defaultUserAgent = "gunpla-collector/1.0 (+https://github.com/; contact via shop enquiry form)"

// defaultMaxBody caps a single response body. Every endpoint we read is a
// paginated listing that should be at most a few hundred KB; much bigger
// than that means something's wrong (an error page, a bad redirect).
const defaultMaxBody = 8 << 20 // 8 MiB

// httpFetcher is the HTTP plumbing shared by every shop scraper: a
// rate-limited, size-capped JSON GET. Each scraper embeds one and adds
// only the URL construction and response-shape decoding for its own API.
type httpFetcher struct {
	httpClient *http.Client
	userAgent  string
	delay      time.Duration // minimum wait between requests
	jitter     time.Duration // random extra wait added on top, 0..jitter
	maxBody    int64         // response body cap in bytes; <=0 means defaultMaxBody
	accept     string        // Accept header; empty means application/json
	retries    int           // extra attempts after a transient failure; 0 disables retrying
	// retryBackoff is the wait before the first retry; it doubles for each
	// further one.
	retryBackoff time.Duration

	requested bool // whether get has been called yet — no delay before the first request
}

// Retry defaults for the real scrapers (tests build httpFetcher directly
// and leave retrying off unless they're testing it). A daily batch job can
// afford to wait: 3 retries back off 2s, 4s, 8s.
const (
	defaultRetries      = 3
	defaultRetryBackoff = 2 * time.Second
)

// transientError marks a failure worth retrying: a dropped connection or
// truncated body (e.g. "unexpected EOF"), a timeout, or a 429/5xx response.
// Anything else (404, oversized body, bad request) fails immediately.
type transientError struct{ err error }

func (e *transientError) Error() string { return e.err.Error() }
func (e *transientError) Unwrap() error { return e.err }

// get performs one rate-limited GET: delay+jitter before every request
// after the first, then a 200 check and a body size cap. Transient
// failures are retried with exponential backoff, so one dropped connection
// in the middle of a many-page scrape doesn't fail the whole run.
func (f *httpFetcher) get(ctx context.Context, url string) ([]byte, error) {
	if f.requested {
		if err := sleepCtx(ctx, f.delay+randJitter(f.jitter)); err != nil {
			return nil, err
		}
	}
	f.requested = true

	backoff := f.retryBackoff
	for attempt := 0; ; attempt++ {
		body, err := f.getOnce(ctx, url)
		if err == nil {
			return body, nil
		}
		var tr *transientError
		if attempt >= f.retries || ctx.Err() != nil || !errors.As(err, &tr) {
			return nil, err
		}
		if err := sleepCtx(ctx, backoff); err != nil {
			return nil, err
		}
		backoff *= 2
	}
}

func (f *httpFetcher) getOnce(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", f.userAgent)
	accept := f.accept
	if accept == "" {
		accept = "application/json"
	}
	req.Header.Set("Accept", accept)

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, &transientError{err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("unexpected status %d from %s", resp.StatusCode, url)
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			return nil, &transientError{err}
		}
		return nil, err
	}

	maxBody := f.maxBody
	if maxBody <= 0 {
		maxBody = defaultMaxBody
	}
	// Read one byte past the cap to tell "at the limit" from "truncated"
	// without buffering a potentially huge body first.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, &transientError{fmt.Errorf("read response from %s: %w", url, err)}
	}
	if int64(len(body)) > maxBody {
		return nil, fmt.Errorf("response from %s exceeds %d byte limit", url, maxBody)
	}
	return body, nil
}

// getJSON fetches url via get and decodes the body as JSON into v.
func (f *httpFetcher) getJSON(ctx context.Context, url string, v any) error {
	body, err := f.get(ctx, url)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", url, err)
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("parse %s: %w", url, err)
	}
	return nil
}

func randJitter(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(int64(max)))
}

// sleepCtx waits out d, returning early with ctx's error if ctx is
// cancelled first — so SIGTERM or a timeout is noticed right away instead
// of only after the next request.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
