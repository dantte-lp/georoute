package main

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// tickerFactory abstracts time.NewTicker so tests can drive the loop
// with a hand-pushed channel and avoid wall-clock waits. Returned
// stop func reclaims the ticker; callers must call it once.
type tickerFactory func(d time.Duration) (<-chan time.Time, func())

// defaultTickerFactory wraps time.NewTicker for production use.
var defaultTickerFactory tickerFactory = func(d time.Duration) (<-chan time.Time, func()) {
	t := time.NewTicker(d)

	return t.C, t.Stop
}

// refreshLoop runs the work function once immediately, then (if
// interval > 0) on every tick from tf until ctx is cancelled. A tick
// that arrives while a previous cycle is still running is dropped and
// counted via metrics.skippedOverlapTotal — sequential rather than
// queued semantics keep memory bounded under sustained slow runs.
//
// work is the per-cycle function (typically a closure that wires the
// real run() + observation calls). m may be nil; when non-nil, the
// skippedOverlapTotal counter is bumped on each dropped tick.
func refreshLoop(
	ctx context.Context,
	interval time.Duration,
	work func(context.Context),
	tf tickerFactory,
	logger *slog.Logger,
	m *metrics,
) error {
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	runOnce := func() {
		if !mu.TryLock() {
			if m != nil {
				m.skippedOverlapTotal.WithLabelValues(m.country).Inc()
			}
			logger.Warn("skip overlap — previous cycle still running")

			return
		}
		defer mu.Unlock()
		work(ctx)
	}
	if interval == 0 {
		// Oneshot semantics: caller wants a single synchronous run and
		// then a clean return. Used in the binary's existing
		// run-once-park-on-SIGTERM path.
		runOnce()

		return nil
	}
	// Daemon semantics: dispatch the initial run as a goroutine so
	// the ticker loop can start consuming ticks immediately. A
	// long-running cycle then naturally collides with the next tick
	// and gets surfaced via skippedOverlapTotal rather than queueing.
	// Every dispatched runOnce is tracked by wg so we can drain
	// in-flight cycles on shutdown — SIGTERM must not race against
	// an open nft/frr-reload transaction.
	dispatch := func() {
		wg.Go(runOnce)
	}
	dispatch()
	ch, stop := tf(interval)
	defer stop()
	for {
		select {
		case <-ctx.Done():
			// Drain in-flight cycle(s) before returning so the caller
			// can safely tear down the HTTP server / exit the process.
			wg.Wait()

			return nil
		case <-ch:
			dispatch()
		}
	}
}
