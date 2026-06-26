package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Cache-related sentinel errors. errCacheMissing tells the caller to
// proceed without a cache (first run); errCacheStale tells it to fail
// loudly rather than push outdated state.
var (
	errCacheMissing = errors.New("cache file does not exist")
	errCacheStale   = errors.New("cache file exceeds max age")
)

// feedSource lets callers branch on where the data came from for logging
// and metrics. Untyped constants would be ambiguous against any other
// "fresh"/"cache" string in the codebase.
type feedSource int

const (
	feedSourceFresh feedSource = iota + 1
	feedSourceCache
)

func (s feedSource) String() string {
	switch s {
	case feedSourceFresh:
		return "fresh"
	case feedSourceCache:
		return "cache"
	default:
		return "unknown"
	}
}

// writeCachedFeed serializes resp to a gzip-compressed JSON file at path.
// Write is atomic (CreateTemp + rename) so a crash mid-write can't leave a
// truncated cache.
func writeCachedFeed(path string, resp *ripeResp) error {
	dir := filepath.Dir(path)
	err := os.MkdirAll(dir, 0o750)
	if err != nil {
		return fmt.Errorf("mkdir cache dir %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp cache: %w", err)
	}
	tmpName := tmp.Name()
	// On any error path we close the temp file and remove it.
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}

	gz := gzip.NewWriter(tmp)
	err = json.NewEncoder(gz).Encode(resp)
	if err != nil {
		cleanup()

		return fmt.Errorf("encode cache: %w", err)
	}
	err = gz.Close()
	if err != nil {
		cleanup()

		return fmt.Errorf("close gzip: %w", err)
	}
	err = tmp.Chmod(configFileMode)
	if err != nil {
		cleanup()

		return fmt.Errorf("chmod cache: %w", err)
	}
	err = tmp.Close()
	if err != nil {
		_ = os.Remove(tmpName)

		return fmt.Errorf("close cache: %w", err)
	}
	err = os.Rename(tmpName, path)
	if err != nil {
		_ = os.Remove(tmpName)

		return fmt.Errorf("rename cache: %w", err)
	}

	return nil
}

// readCachedFeed loads the cached RIPE response. Returns errCacheMissing
// when the file does not exist (first run) and errCacheStale when the
// file is older than maxAge (so callers can fail loudly).
func readCachedFeed(path string, maxAge time.Duration) (*ripeResp, time.Duration, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, 0, fmt.Errorf("%w: %s", errCacheMissing, path)
		}

		return nil, 0, fmt.Errorf("stat cache %s: %w", path, err)
	}
	age := time.Since(info.ModTime())
	if age > maxAge {
		return nil, age, fmt.Errorf("%w: %s age=%s max=%s",
			errCacheStale, path, age, maxAge)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, age, fmt.Errorf("open cache %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, age, fmt.Errorf("gzip read cache %s: %w", path, err)
	}
	defer func() { _ = gz.Close() }()

	var resp ripeResp
	err = json.NewDecoder(gz).Decode(&resp)
	if err != nil {
		return nil, age, fmt.Errorf("decode cache %s: %w", path, err)
	}

	return &resp, age, nil
}

// fetchWithCache tries the live RIPE fetch first; on 5xx exhaustion it
// falls back to the cached feed if one exists and is still fresh enough.
// 4xx errors are returned as-is — they signal operator misconfig (wrong
// URL, country code) that a stale cache would only mask.
//
// cachePath = "" disables the cache layer entirely (backward compat with
// callers that don't yet support it).
func fetchWithCache(ctx context.Context, url, cachePath string, cacheMaxAge time.Duration) (*ripeResp, feedSource, error) {
	resp, err := fetchWithRetry(ctx, url)
	if err == nil {
		if cachePath != "" {
			writeErr := writeCachedFeed(cachePath, resp)
			if writeErr != nil {
				// Cache write failure should not break the fresh-fetch
				// happy path; surface via context-aware log only.
				logf("warn: cache write failed: %v", writeErr)
			}
		}

		return resp, feedSourceFresh, nil
	}

	// Client errors (4xx) signal a bug that cache cannot fix; surface
	// directly. Anything else (network / 5xx) is eligible for fallback.
	if isClientError(err) {
		return nil, 0, err
	}
	if cachePath == "" {
		return nil, 0, err
	}

	cached, age, cacheErr := readCachedFeed(cachePath, cacheMaxAge)
	if cacheErr != nil {
		return nil, 0, fmt.Errorf("fetch failed and cache unusable: fetch=%w cache=%w", err, cacheErr)
	}
	logf("falling back to cache: source=%s age=%s (fetch error: %v)", cachePath, age, err)

	return cached, feedSourceCache, nil
}

// isClientError reports whether err originates from an HTTP 4xx — those
// are operator errors (bad URL, wrong country code) where cache fallback
// would silently mask a real bug.
func isClientError(err error) bool {
	if !errors.Is(err, errHTTP) {
		return false
	}
	msg := err.Error()
	for code := 400; code < 500; code++ {
		needle := fmt.Sprintf("status=%d", code)
		if containsStr(msg, needle) {
			return true
		}
	}

	return false
}

// containsStr is a tiny helper so isClientError doesn't pull in strings
// just to do prefix-aware substring scan.
func containsStr(s, sub string) bool {
	if len(sub) > len(s) {
		return false
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}

	return false
}

// logf is a thin wrapper around log.Printf used by cache.go so tests can
// stub it if they grow noisy. For now it just delegates; the indirection
// keeps the file from importing log at the top of every test.
var logf = func(format string, args ...any) {
	defaultLogf(format, args...)
}
