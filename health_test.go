package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestLastSuccess_WriteRead roundtrips a timestamp file: write now,
// read back, verify the age is small (well under a second).
func TestLastSuccess_WriteRead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "last-success-ru")

	err := writeLastSuccess(path)
	if err != nil {
		t.Fatalf("writeLastSuccess: %v", err)
	}

	age, err := lastSuccessAge(path)
	if err != nil {
		t.Fatalf("lastSuccessAge: %v", err)
	}
	if age > 5*time.Second {
		t.Errorf("age too large for fresh file: %v", age)
	}
}

// TestLastSuccess_MissingFile — first-run case. The reader must
// distinguish "no successful run yet" from random I/O errors so the
// caller can decide whether to fail-closed (readyz=503) instead of
// papering over the situation.
func TestLastSuccess_MissingFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "missing")

	_, err := lastSuccessAge(path)
	if !errors.Is(err, errLastSuccessMissing) {
		t.Errorf("expected errLastSuccessMissing, got %v", err)
	}
}

// TestHealthServer_LiveAlways200 — /livez must NOT touch the file
// system. A failed downstream (RIPE outage) should not restart the
// pod, so /livez always reports 200 once the process is up.
func TestHealthServer_LiveAlways200(t *testing.T) {
	t.Parallel()
	hs := newTestHealthServer(t, filepath.Join(t.TempDir(), "no-such-file"), 24*time.Hour)

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/live", http.NoBody)
	hs.handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("/live = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
}

// TestHealthServer_ReadyBeforeFirstSuccess — no last-success file
// means the service has not yet completed a refresh; /ready must
// return 503 so an external load balancer keeps siblings in rotation
// and not this freshly-booted instance.
func TestHealthServer_ReadyBeforeFirstSuccess(t *testing.T) {
	t.Parallel()
	hs := newTestHealthServer(t, filepath.Join(t.TempDir(), "missing"), 24*time.Hour)

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/ready", http.NoBody)
	hs.handler().ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Errorf("/ready returned 200 before first success — should be 5xx (body: %s)", rec.Body.String())
	}
}

// TestHealthServer_ReadyAfterFreshSuccess — last-success file
// written just now, max age generous; /ready must return 200.
func TestHealthServer_ReadyAfterFreshSuccess(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "last-success-ru")
	err := writeLastSuccess(path)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	hs := newTestHealthServer(t, path, 24*time.Hour)

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/ready", http.NoBody)
	hs.handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("/ready = %d after fresh success, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
}

// TestHealthServer_ReadyStaleSuccess — last-success file present but
// older than max age; the operator clearly missed a refresh window
// and the LB must drain this node.
func TestHealthServer_ReadyStaleSuccess(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "last-success-ru")
	err := writeLastSuccess(path)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Backdate to 25h ago vs 24h max.
	old := time.Now().Add(-25 * time.Hour)
	err = os.Chtimes(path, old, old)
	if err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	hs := newTestHealthServer(t, path, 24*time.Hour)

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/ready", http.NoBody)
	hs.handler().ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Errorf("/ready = 200 with stale last-success — should be 5xx (body: %s)", rec.Body.String())
	}
}

// TestHealthServer_PProfReachable — the pprof debug surface mounted
// under /debug/ must be served. Operators rely on it for goroutine
// dumps during incidents.
func TestHealthServer_PProfReachable(t *testing.T) {
	t.Parallel()
	hs := newTestHealthServer(t, filepath.Join(t.TempDir(), "missing"), 24*time.Hour)

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/debug/pprof/", http.NoBody)
	hs.handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("/debug/pprof/ = %d, want 200", rec.Code)
	}
}

// TestHealthServer_StartShutdownLifecycle — Start spins up the real
// *http.Server in a goroutine; Shutdown gracefully stops it within
// the deadline. Verifies no listen-leak under repeated start/stop.
func TestHealthServer_StartShutdownLifecycle(t *testing.T) {
	t.Parallel()
	hs := newTestHealthServer(t, filepath.Join(t.TempDir(), "missing"), 24*time.Hour)

	lc := &net.ListenConfig{}
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listener: %v", err)
	}
	addr := "http://" + ln.Addr().String()

	doneCh := make(chan error, 1)
	go func() {
		doneCh <- hs.serve(ln)
	}()

	// Wait for a successful response so we know the server is up.
	client := &http.Client{Timeout: 200 * time.Millisecond}
	deadline := time.Now().Add(2 * time.Second)
	var resp *http.Response
	for time.Now().Before(deadline) {
		req, reqErr := http.NewRequestWithContext(context.Background(), http.MethodGet, addr+"/live", http.NoBody)
		if reqErr != nil {
			t.Fatalf("build request: %v", reqErr)
		}
		resp, err = client.Do(req)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("server never came up: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/live HTTP %d", resp.StatusCode)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err = hs.shutdown(shutdownCtx)
	if err != nil {
		t.Errorf("shutdown: %v", err)
	}
	err = <-doneCh
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		t.Errorf("serve returned %v, want nil or ErrServerClosed", err)
	}
}

// newTestHealthServer builds a fresh healthServer wired for tests; the
// real constructor accepts the same args plus a real listen address.
//
//nolint:unparam // maxAge varies across future tests; keep the signature flexible
func newTestHealthServer(t *testing.T, lastSuccessPath string, maxAge time.Duration) *healthServer {
	t.Helper()
	hs, err := newHealthServer(":0", lastSuccessPath, "ru", maxAge)
	if err != nil {
		t.Fatalf("newHealthServer: %v", err)
	}
	t.Cleanup(func() {
		// Background loop may have been started; make sure it stops.
		hs.stopBackgroundLoop()
	})

	return hs
}
