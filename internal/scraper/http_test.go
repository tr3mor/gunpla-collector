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
