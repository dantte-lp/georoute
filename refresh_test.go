package main

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestRefreshLoop_IntervalZero — interval=0 means oneshot semantics:
// run the work exactly once and return nil. No ticker, no goroutine
// leak.
func TestRefreshLoop_IntervalZero(t *testing.T) {
	t.Parallel()
	var count atomic.Int32
	work := func(context.Context) { count.Add(1) }
	tf := func(time.Duration) (<-chan time.Time, func()) {
		t.Fatal("ticker factory must not be called when interval == 0")

		return nil, func() {}
	}

	err := refreshLoop(context.Background(), 0, work, tf, slogDiscard(), nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got := count.Load(); got != 1 {
		t.Errorf("work invocations = %d, want 1", got)
	}
}

// TestRefreshLoop_TicksFire — pushing N ticks runs the work N+1 times
// (one initial + N from ticks). Verifies the loop actually consumes
// the fake ticker channel.
func TestRefreshLoop_TicksFire(t *testing.T) {
	t.Parallel()
	var count atomic.Int32
	work := func(context.Context) { count.Add(1) }
	ch := make(chan time.Time, 4)
	tf := func(time.Duration) (<-chan time.Time, func()) {
		return ch, func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan error, 1)
	go func() {
		doneCh <- refreshLoop(ctx, time.Hour, work, tf, slogDiscard(), nil)
	}()

	for range 3 {
		ch <- time.Now()
	}
	// Wait briefly so the goroutine-dispatched work calls land.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && count.Load() < 4 {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-doneCh

	if got := count.Load(); got != 4 {
		t.Errorf("work invocations = %d, want 4 (1 initial + 3 ticks)", got)
	}
}

// TestRefreshLoop_OverlapSkipped — when a tick fires while the
// previous cycle is still running, the second cycle must be skipped
// and the skipped-overlap counter must increment.
func TestRefreshLoop_OverlapSkipped(t *testing.T) {
	t.Parallel()
	reg := prometheus.NewRegistry()
	m := newMetrics(reg, "ru")

	var workCount atomic.Int32
	release := make(chan struct{})
	work := func(context.Context) {
		workCount.Add(1)
		<-release
	}
	ch := make(chan time.Time, 2)
	tf := func(time.Duration) (<-chan time.Time, func()) {
		return ch, func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan error, 1)
	go func() {
		doneCh <- refreshLoop(ctx, time.Hour, work, tf, slogDiscard(), m)
	}()

	// Wait for the initial work to be running.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && workCount.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if workCount.Load() == 0 {
		t.Fatal("initial work never started")
	}

	// Tick while the initial cycle is blocked → must be skipped.
	ch <- time.Now()
	// Wait long enough for the skip to register.
	time.Sleep(50 * time.Millisecond)

	if v := testutil.ToFloat64(m.skippedOverlapTotal.WithLabelValues("ru")); v != 1 {
		t.Errorf("skippedOverlapTotal = %v, want 1", v)
	}

	close(release)
	cancel()
	<-doneCh
}

// TestRefreshLoop_ContextCancelStops — when ctx is cancelled, the
// loop must exit promptly (within ~100ms) and return nil.
func TestRefreshLoop_ContextCancelStops(t *testing.T) {
	t.Parallel()
	work := func(context.Context) {}
	ch := make(chan time.Time)
	tf := func(time.Duration) (<-chan time.Time, func()) {
		return ch, func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())

	doneCh := make(chan error, 1)
	go func() {
		doneCh <- refreshLoop(ctx, time.Hour, work, tf, slogDiscard(), nil)
	}()

	cancel()
	select {
	case err := <-doneCh:
		if err != nil {
			t.Errorf("err = %v, want nil", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("refreshLoop did not exit within 200ms after cancel")
	}
}

// TestRefreshLoop_TickerFactoryStopCalled — when the loop exits, the
// stop func returned by the ticker factory must be called so the real
// time.Ticker is reclaimed.
func TestRefreshLoop_TickerFactoryStopCalled(t *testing.T) {
	t.Parallel()
	var stopCalled atomic.Bool
	ch := make(chan time.Time)
	tf := func(time.Duration) (<-chan time.Time, func()) {
		return ch, func() { stopCalled.Store(true) }
	}
	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan error, 1)
	go func() {
		doneCh <- refreshLoop(ctx, time.Hour, func(context.Context) {}, tf, slogDiscard(), nil)
	}()
	cancel()
	<-doneCh
	if !stopCalled.Load() {
		t.Error("ticker stop func was not called on exit")
	}
}

// slogDiscard returns a no-op logger so tests don't emit noise.
func slogDiscard() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}
