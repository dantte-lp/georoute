package main

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Reusable label / result constants to satisfy goconst and to keep
// scrape-config writers honest about the cardinality each label
// introduces.
const (
	metricResultSuccess = "success"
	metricResultError   = "error"

	metricLabelCountry = "country"
	metricLabelResult  = "result"
	metricLabelSource  = "source"
	metricLabelFamily  = "family"

	// Series names — repeated by test assertions and help text. Keep
	// canonical here.
	metricRunsTotal = "georoute_runs_total"
)

// metrics owns every Prometheus collector the binary exposes. The fields
// are exported for tests (testutil.ToFloat64 takes the labeled child)
// but business code should use the observe*/set* helpers — they keep
// the country label centralized in one place.
type metrics struct {
	runsTotal       *prometheus.CounterVec
	fetchesTotal    *prometheus.CounterVec
	nftAppliesTotal *prometheus.CounterVec
	frrReloadsTotal *prometheus.CounterVec

	prefixesCount   *prometheus.GaugeVec
	lastSuccessUnix *prometheus.GaugeVec
	cacheAge        *prometheus.GaugeVec

	fetchDuration     *prometheus.HistogramVec
	nftApplyDuration  *prometheus.HistogramVec
	frrReloadDuration *prometheus.HistogramVec

	country string
}

// newMetrics constructs every collector and registers it with reg. The
// country label is curried in via observeRun etc. so calling code does
// not have to thread it through every measurement.
//
// Panics on duplicate registration (programmer error: a second call on
// the same registry would silently shadow the first set of
// collectors). Callers that need to recover from this should pass a
// fresh registry.
func newMetrics(reg prometheus.Registerer, country string) *metrics {
	m := &metrics{
		country: country,
		runsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: metricRunsTotal,
			Help: "Number of full georoute runs by result.",
		}, []string{metricLabelCountry, metricLabelResult}),
		fetchesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "georoute_fetches_total",
			Help: "RIPE Stat fetch attempts by source (fresh|cache) and result (success|error).",
		}, []string{metricLabelCountry, metricLabelSource, metricLabelResult}),
		nftAppliesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "georoute_nft_applies_total",
			Help: "nft set replacement attempts by result.",
		}, []string{metricLabelCountry, metricLabelResult}),
		frrReloadsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "georoute_frr_reloads_total",
			Help: "frr-reload.py invocations by result (success|error|rolled_back).",
		}, []string{metricLabelCountry, metricLabelResult}),
		prefixesCount: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "georoute_prefixes",
			Help: "Number of prefixes in the last aggregated set by family and source.",
		}, []string{metricLabelCountry, metricLabelFamily, metricLabelSource}),
		lastSuccessUnix: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "georoute_last_success_unixtime",
			Help: "Unix timestamp of the last successful run.",
		}, []string{metricLabelCountry}),
		cacheAge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "georoute_cache_age_seconds",
			Help: "Age of the on-disk RIPE feed cache in seconds.",
		}, []string{metricLabelCountry}),
		fetchDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "georoute_fetch_duration_seconds",
			Help:    "Duration of the RIPE Stat fetch (including retries and cache fallback).",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120},
		}, []string{metricLabelCountry, metricLabelSource}),
		nftApplyDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "georoute_nft_apply_duration_seconds",
			Help:    "Duration of the nft set replacement.",
			Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 5, 30},
		}, []string{metricLabelCountry}),
		frrReloadDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "georoute_frr_reload_duration_seconds",
			Help:    "Duration of frr-reload.py invocation.",
			Buckets: []float64{0.5, 1, 2, 5, 10, 30, 60, 120, 180},
		}, []string{metricLabelCountry}),
	}

	for _, c := range []prometheus.Collector{
		m.runsTotal, m.fetchesTotal, m.nftAppliesTotal, m.frrReloadsTotal,
		m.prefixesCount, m.lastSuccessUnix, m.cacheAge,
		m.fetchDuration, m.nftApplyDuration, m.frrReloadDuration,
	} {
		reg.MustRegister(c)
	}

	return m
}

// observeRun records a completed georoute run.
func (m *metrics) observeRun(result string, dur time.Duration) {
	m.runsTotal.WithLabelValues(m.country, result).Inc()
	_ = dur // duration captured per-phase below; full run latency is
	// reconstructible from fetch+nft+frr histograms.
}

// observeFetch records a RIPE Stat fetch attempt. source is "fresh"
// or "cache"; result is "success" or "error".
func (m *metrics) observeFetch(source, result string, dur time.Duration) {
	m.fetchesTotal.WithLabelValues(m.country, source, result).Inc()
	m.fetchDuration.WithLabelValues(m.country, source).Observe(dur.Seconds())
}

// observeNftApply records an nft set replacement attempt.
func (m *metrics) observeNftApply(result string, dur time.Duration) {
	m.nftAppliesTotal.WithLabelValues(m.country, result).Inc()
	m.nftApplyDuration.WithLabelValues(m.country).Observe(dur.Seconds())
}

// observeFrrReload records a frr-reload.py invocation.
func (m *metrics) observeFrrReload(result string, dur time.Duration) {
	m.frrReloadsTotal.WithLabelValues(m.country, result).Inc()
	m.frrReloadDuration.WithLabelValues(m.country).Observe(dur.Seconds())
}

// setPrefixCount sets the current number of prefixes for a family.
// family is "v4" or "v6"; source is "ripe", "extras", or "merged".
func (m *metrics) setPrefixCount(family, source string, count int) {
	m.prefixesCount.WithLabelValues(m.country, family, source).Set(float64(count))
}

// setLastSuccess records the unix timestamp of the last successful run.
func (m *metrics) setLastSuccess(t time.Time) {
	m.lastSuccessUnix.WithLabelValues(m.country).Set(float64(t.Unix()))
}

// setCacheAge records the current cache file age in seconds. A zero
// value means "no cache" — sample as Inf would be more honest but
// dashboards usually treat 0 as a clear signal too.
func (m *metrics) setCacheAge(age time.Duration) {
	m.cacheAge.WithLabelValues(m.country).Set(age.Seconds())
}
