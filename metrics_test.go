package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestMetrics_NewRegistersAll — fresh registry, newMetrics should
// register every collector without panicking.
func TestMetrics_NewRegistersAll(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	m := newMetrics(reg, "ru")
	if m == nil {
		t.Fatal("newMetrics returned nil")
	}

	// CounterVec/GaugeVec/HistogramVec only show up in Gather() once
	// they have at least one observed label set. Stimulate every
	// metric so the post-condition is observable.
	m.observeRun("success", time.Second)
	m.observeFetch("fresh", "success", time.Second)
	m.observeNftApply("success", time.Second)
	m.observeFrrReload("success", time.Second)
	m.setPrefixCount("v4", "merged", 1)
	m.setLastSuccess(time.Now())
	m.setCacheAge(time.Second)

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}

	got := map[string]bool{}
	for _, f := range families {
		got[f.GetName()] = true
	}
	for _, name := range []string{
		metricRunsTotal,
		"georoute_fetches_total",
		"georoute_nft_applies_total",
		"georoute_frr_reloads_total",
		"georoute_prefixes",
		"georoute_last_success_unixtime",
		"georoute_cache_age_seconds",
		"georoute_fetch_duration_seconds",
		"georoute_nft_apply_duration_seconds",
		"georoute_frr_reload_duration_seconds",
	} {
		if !got[name] {
			t.Errorf("metric %q not registered", name)
		}
	}
}

// TestMetrics_DuplicateRegistrationFails — calling newMetrics twice on
// the same registry must panic so a programmer-error misuse surfaces
// immediately instead of silently shadowing collectors.
func TestMetrics_DuplicateRegistrationFails(t *testing.T) {
	t.Parallel()
	reg := prometheus.NewRegistry()
	_ = newMetrics(reg, "ru")

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate registration")
		}
	}()
	_ = newMetrics(reg, "ru")
}

// TestMetrics_CountersIncrement — observe* helpers must bump the right
// labeled series.
func TestMetrics_CountersIncrement(t *testing.T) {
	t.Parallel()
	reg := prometheus.NewRegistry()
	m := newMetrics(reg, "ru")

	m.observeRun("success", 5*time.Second)
	m.observeFetch("fresh", "success", 200*time.Millisecond)
	m.observeNftApply("success", 50*time.Millisecond)
	m.observeFrrReload("success", time.Second)

	if v := testutil.ToFloat64(m.runsTotal.WithLabelValues("ru", "success")); v != 1 {
		t.Errorf("runsTotal = %v, want 1", v)
	}
	if v := testutil.ToFloat64(m.fetchesTotal.WithLabelValues("ru", "fresh", "success")); v != 1 {
		t.Errorf("fetchesTotal = %v, want 1", v)
	}
	if v := testutil.ToFloat64(m.nftAppliesTotal.WithLabelValues("ru", "success")); v != 1 {
		t.Errorf("nftAppliesTotal = %v, want 1", v)
	}
	if v := testutil.ToFloat64(m.frrReloadsTotal.WithLabelValues("ru", "success")); v != 1 {
		t.Errorf("frrReloadsTotal = %v, want 1", v)
	}
}

// TestMetrics_GaugesSet — setters write the values; gather reads them
// back via testutil.ToFloat64.
func TestMetrics_GaugesSet(t *testing.T) {
	t.Parallel()
	reg := prometheus.NewRegistry()
	m := newMetrics(reg, "ru")

	m.setPrefixCount("v4", "merged", 8626)
	m.setLastSuccess(time.Unix(1_700_000_000, 0))
	m.setCacheAge(45 * time.Second)

	if v := testutil.ToFloat64(m.prefixesCount.WithLabelValues("ru", "v4", "merged")); v != 8626 {
		t.Errorf("prefixesCount = %v, want 8626", v)
	}
	if v := testutil.ToFloat64(m.lastSuccessUnix.WithLabelValues("ru")); v != 1_700_000_000 {
		t.Errorf("lastSuccessUnix = %v, want 1.7e9", v)
	}
	if v := testutil.ToFloat64(m.cacheAge.WithLabelValues("ru")); v != 45 {
		t.Errorf("cacheAge = %v, want 45", v)
	}
}

// TestMetrics_HTTPEndpoint — the /metrics handler returns text/plain
// Prometheus-format output containing our series names.
func TestMetrics_HTTPEndpoint(t *testing.T) {
	t.Parallel()

	hs := newTestHealthServer(t, filepath.Join(t.TempDir(), "missing"), 24*time.Hour)
	hs.metrics.observeRun("success", time.Second)

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", http.NoBody)
	hs.handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics = %d, want 200", rec.Code)
	}
	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	bodyStr := string(body)
	for _, want := range []string{
		`georoute_runs_total`,
		`country="ru"`,
		`result="success"`,
	} {
		if !strings.Contains(bodyStr, want) {
			t.Errorf("/metrics body missing %q", want)
		}
	}
}

// TestMetrics_HealthcheckSharedRegistry — the healthcheck library's
// check Gauge/Histogram must register into the same registry as our
// metrics, so a single /metrics scrape exposes both.
func TestMetrics_HealthcheckSharedRegistry(t *testing.T) {
	t.Parallel()
	hs := newTestHealthServer(t, filepath.Join(t.TempDir(), "missing"), 24*time.Hour)

	// Hit /ready once so the healthcheck library has at least observed
	// a check and pushed its metrics.
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/ready", http.NoBody)
	hs.handler().ServeHTTP(rec, req)

	rec = httptest.NewRecorder()
	req = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", http.NoBody)
	hs.handler().ServeHTTP(rec, req)
	body, _ := io.ReadAll(rec.Body)

	// healthcheck library prefixes its series healthcheck_metrics_<name>.
	if !strings.Contains(string(body), "healthcheck_metrics_") {
		t.Errorf("expected healthcheck_metrics_* in /metrics body (body: %s)", body)
	}
}
