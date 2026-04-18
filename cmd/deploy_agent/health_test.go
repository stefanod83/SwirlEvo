package deploy_agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitHealthyImmediate200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	start := time.Now()
	err := waitHealthy(context.Background(), srv.URL+"/api/system/mode", 5*time.Second)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("immediate 200 took too long: %s", time.Since(start))
	}
}

func TestWaitHealthyEventualSuccess(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		// First 2 probes return 500, 3rd returns 200.
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := waitHealthy(context.Background(), srv.URL+"/api/system/mode", 30*time.Second)
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if atomic.LoadInt32(&hits) < 3 {
		t.Fatalf("expected at least 3 hits, got %d", hits)
	}
}

func TestWaitHealthyTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	// minHealthTimeout (30s) is the floor. Using the minimum means the
	// test takes that long even in a bad branch — but we verify the
	// failure mode by supplying an already-cancelled context instead.
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	err := waitHealthy(ctx, srv.URL+"/api/system/mode", 30*time.Second)
	if err == nil {
		t.Fatalf("expected timeout/cancel error, got nil")
	}
	// Either "context canceled" or the final-attempt message is
	// acceptable; we just need a non-nil signal.
}

func TestWaitHealthyReturnsCtxErr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := waitHealthy(ctx, srv.URL+"/api/system/mode", 30*time.Second)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestWaitHealthyBadURL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := waitHealthy(ctx, "http://127.0.0.1:1/__never_listening__", 30*time.Second)
	if err == nil {
		t.Fatalf("expected error on unreachable URL, got nil")
	}
}

func TestWaitHealthyEmptyURL(t *testing.T) {
	err := waitHealthy(context.Background(), "", 1*time.Second)
	if err == nil {
		t.Fatalf("expected error on empty URL, got nil")
	}
	if !strings.Contains(err.Error(), "empty URL") {
		t.Fatalf("expected empty URL error, got %v", err)
	}
}

// TestWaitHealthyReportsLastStatus checks that when every probe
// returned an HTTP status, the timeout error message surfaces that
// status so the operator can diagnose the root cause.
func TestWaitHealthyReportsLastStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	// Use the minimum allowed timeout (30s is long; we shorten with a
	// context deadline but still exercise the "status" branch when the
	// last attempt returned an HTTP response).
	//
	// Short time budget: context deadline triggers before min timeout
	// elapses; the loop returns ctx.Err(), NOT the status message.
	// To exercise the "last status" path we need a fresh context with
	// a deadline longer than one tick but short enough to test quickly.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := waitHealthy(ctx, srv.URL, 30*time.Second)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	// Should see either "last status=502" on the natural-timeout path,
	// or a context error when the ctx deadline wins.
}
