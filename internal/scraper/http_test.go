package scraper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// get() must refuse a response over its configured cap instead of
// buffering it fully.
func TestHTTPFetcher_Get_RejectsOversizedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strings.Repeat("x", 101)))
	}))
	defer srv.Close()

	f := &httpFetcher{httpClient: srv.Client(), userAgent: "test", maxBody: 100}
	if _, err := f.get(context.Background(), srv.URL); err == nil {
		t.Fatal("expected error for a body exceeding maxBody, got nil")
	}
}

// The cap is inclusive: exactly maxBody bytes must not be rejected.
func TestHTTPFetcher_Get_AllowsBodyAtExactCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strings.Repeat("x", 100)))
	}))
	defer srv.Close()

	f := &httpFetcher{httpClient: srv.Client(), userAgent: "test", maxBody: 100}
	body, err := f.get(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(body) != 100 {
		t.Errorf("len(body) = %d, want 100", len(body))
	}
}

// get() must return promptly on an already-cancelled ctx instead of
// waiting out its delay first.
func TestHTTPFetcher_Get_RespectsCancelledContext(t *testing.T) {
	f := &httpFetcher{
		httpClient: http.DefaultClient,
		userAgent:  "test",
		delay:      time.Second,
	}
	f.requested = true // simulate "not the first request", so the delay applies

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	_, err := f.get(ctx, "http://127.0.0.1:1/unused") // address is never dialed
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("get took %v to return after ctx was already cancelled, want well under its 1s delay", elapsed)
	}
}

// Same guarantee as above, at the sleepCtx level.
func TestSleepCtx_ReturnsEarlyOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	err := sleepCtx(ctx, time.Second)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected context.Canceled, got nil")
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("sleepCtx took %v to return, want well under its 1s delay", elapsed)
	}
}

func retryFetcher(srv *httptest.Server, retries int) *httpFetcher {
	return &httpFetcher{httpClient: srv.Client(), userAgent: "test", retries: retries, retryBackoff: time.Millisecond}
}

// A response cut off mid-body ("unexpected EOF", as seen from plamodx.nl)
// must be retried and succeed once the server behaves.
func TestHTTPFetcher_Get_RetriesTruncatedBody(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Content-Length", "10")
			w.Write([]byte("abc")) // promise 10 bytes, send 3, then drop the connection
			return
		}
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	body, err := retryFetcher(srv, 2).get(context.Background(), srv.URL)
	if err != nil || string(body) != "ok" {
		t.Fatalf("get = %q, %v; want ok after retry", body, err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
}

func TestHTTPFetcher_Get_RetriesServerErrorThenGivesUp(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	if _, err := retryFetcher(srv, 2).get(context.Background(), srv.URL); err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3 (1 attempt + 2 retries)", calls)
	}
}

// Permanent failures must not be retried.
func TestHTTPFetcher_Get_DoesNotRetryClientError(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	if _, err := retryFetcher(srv, 3).get(context.Background(), srv.URL); err == nil {
		t.Fatal("expected error for 404")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}
