package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	testPrefix1 = "1.2.3.0/24"
	testPrefix2 = "1.2.4.0/24"
)

// minimalRipeBody is a syntactically-valid RIPE Stat response with two
// prefixes — enough to verify the fetch+cache+aggregate pipeline.
const minimalRipeBody = `{
  "status": "ok",
  "data": {
    "resources": {
      "ipv4": ["1.2.3.0/24", "1.2.4.0/24"],
      "ipv6": ["2001:db8::/32"]
    }
  }
}`

// TestCache_RoundTrip — write a cache file from a known RIPE response,
// read it back, verify the contents survive the gzip+JSON serialization.
func TestCache_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "feed-ru.json.gz")

	original := &ripeResp{Status: "ok"}
	original.Data.Resources.IPv4 = []string{testPrefix1}
	original.Data.Resources.IPv6 = []string{"2001:db8::/32"}

	err := writeCachedFeed(path, original)
	if err != nil {
		t.Fatalf("writeCachedFeed: %v", err)
	}

	got, age, err := readCachedFeed(path, time.Hour)
	if err != nil {
		t.Fatalf("readCachedFeed: %v", err)
	}
	if got.Status != "ok" || len(got.Data.Resources.IPv4) != 1 || got.Data.Resources.IPv4[0] != testPrefix1 {
		t.Errorf("roundtrip lost data: %+v", got)
	}
	// Just-written cache should be near-zero age.
	if age > 5*time.Second {
		t.Errorf("age too large for fresh cache: %v", age)
	}
}

// TestCache_StaleRefusal — cache older than the operator-configured max
// age must be refused with errCacheStale so the operator gets visibility
// instead of silent stale-state propagation.
func TestCache_StaleRefusal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "feed-ru.json.gz")

	resp := &ripeResp{Status: "ok"}
	resp.Data.Resources.IPv4 = []string{testPrefix1}
	err := writeCachedFeed(path, resp)
	if err != nil {
		t.Fatalf("writeCachedFeed: %v", err)
	}

	// Backdate by setting mtime to 14 days ago, then read with 7-day max.
	old := time.Now().Add(-14 * 24 * time.Hour)
	err = os.Chtimes(path, old, old)
	if err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	_, _, err = readCachedFeed(path, 7*24*time.Hour)
	if !errors.Is(err, errCacheStale) {
		t.Fatalf("expected errCacheStale, got %v", err)
	}
}

// TestCache_CorruptRefusal — a corrupt cache file must return an error
// rather than silently producing empty data downstream.
func TestCache_CorruptRefusal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "feed-ru.json.gz")

	err := os.WriteFile(path, []byte("not a gzip stream"), 0o600)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, _, err = readCachedFeed(path, time.Hour)
	if err == nil {
		t.Fatal("expected error on corrupt cache, got nil")
	}
	// Must not be the stale sentinel — that would mask the corruption.
	if errors.Is(err, errCacheStale) {
		t.Errorf("corrupt cache mis-tagged as stale: %v", err)
	}
}

// TestCache_MissingFile — readCachedFeed must distinguish "no cache yet"
// from real errors so first-run callers can proceed without complaint.
func TestCache_MissingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "feed-ru.json.gz")

	_, _, err := readCachedFeed(path, time.Hour)
	if !errors.Is(err, errCacheMissing) {
		t.Fatalf("expected errCacheMissing, got %v", err)
	}
}

// TestFetchWithCache_FreshSuccess — happy path: RIPE returns 200, cache
// is written, downstream gets the freshly-fetched feed.
func TestFetchWithCache_FreshSuccess(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, minimalRipeBody)
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	path := filepath.Join(dir, "feed-ru.json.gz")

	resp, source, err := fetchWithCache(t.Context(), srv.URL, path, time.Hour, 1, 10*time.Millisecond, 5*time.Second)
	if err != nil {
		t.Fatalf("fetchWithCache: %v", err)
	}
	if source != feedSourceFresh {
		t.Errorf("expected fresh source, got %v", source)
	}
	if len(resp.Data.Resources.IPv4) != 2 {
		t.Errorf("expected 2 v4 prefixes, got %d", len(resp.Data.Resources.IPv4))
	}

	// Cache should have been written.
	_, err = os.Stat(path)
	if err != nil {
		t.Errorf("cache not written: %v", err)
	}
}

// TestFetchWithCache_5xxFallsBackToCache — RIPE returns 503 and the
// retry pipeline exhausts; if a fresh cache exists, downstream gets it
// with source=cache instead of an error.
func TestFetchWithCache_5xxFallsBackToCache(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "upstream busy", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	path := filepath.Join(dir, "feed-ru.json.gz")

	// Seed a cache.
	seed := &ripeResp{Status: "ok"}
	seed.Data.Resources.IPv4 = []string{prefix10v24}
	err := writeCachedFeed(path, seed)
	if err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	resp, source, err := fetchWithCache(t.Context(), srv.URL, path, time.Hour, 1, 10*time.Millisecond, 5*time.Second)
	if err != nil {
		t.Fatalf("fetchWithCache: %v", err)
	}
	if source != feedSourceCache {
		t.Errorf("expected cache source, got %v", source)
	}
	if len(resp.Data.Resources.IPv4) != 1 || resp.Data.Resources.IPv4[0] != prefix10v24 {
		t.Errorf("unexpected cache payload: %+v", resp.Data.Resources)
	}
}

// TestFetchWithCache_4xxDoesNotFallBack — operator-error responses
// (400/404/etc) should bubble up. Falling back to cache would mask the
// real bug.
func TestFetchWithCache_4xxDoesNotFallBack(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	path := filepath.Join(dir, "feed-ru.json.gz")

	// Cache present but should be ignored on 4xx.
	seed := &ripeResp{Status: "ok"}
	seed.Data.Resources.IPv4 = []string{prefix10v24}
	err := writeCachedFeed(path, seed)
	if err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	_, _, err = fetchWithCache(t.Context(), srv.URL, path, time.Hour, 1, 10*time.Millisecond, 5*time.Second)
	if err == nil {
		t.Fatal("expected error on 4xx, got nil")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("expected 400 in error, got: %v", err)
	}
}

// TestFetchWithCache_5xxWithStaleCache — RIPE 5xx + cache too old =
// hard error. We refuse to push stale data forever; operator should be
// alerted.
func TestFetchWithCache_5xxWithStaleCache(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "upstream busy", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	path := filepath.Join(dir, "feed-ru.json.gz")

	seed := &ripeResp{Status: "ok"}
	seed.Data.Resources.IPv4 = []string{prefix10v24}
	err := writeCachedFeed(path, seed)
	if err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	old := time.Now().Add(-14 * 24 * time.Hour)
	err = os.Chtimes(path, old, old)
	if err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	_, _, err = fetchWithCache(t.Context(), srv.URL, path, 7*24*time.Hour, 1, 10*time.Millisecond, 5*time.Second)
	if err == nil {
		t.Fatal("expected error when fetch fails and cache is stale, got nil")
	}
}
