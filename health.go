package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/AOzhogin/healthcheck"
)

// errLastSuccessMissing distinguishes "no successful run yet" (first
// boot, fail-closed expected) from real I/O failures.
var errLastSuccessMissing = errors.New("last-success file missing")

// errLastSuccessStale signals the last-success marker exists but
// predates --ready-max-age. Wrapped by the readiness check so
// errors.Is can match without parsing the message.
var errLastSuccessStale = errors.New("last-success older than max age")

// writeLastSuccess records that a successful refresh just completed.
// We use the file's mtime as the source of truth — no payload — so a
// readiness check is just a stat call. Atomic write keeps a crash from
// leaving a half-truncated marker.
func writeLastSuccess(path string) error {
	dir := filepath.Dir(path)
	err := os.MkdirAll(dir, 0o750)
	if err != nil {
		return fmt.Errorf("mkdir last-success dir %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp last-success: %w", err)
	}
	tmpName := tmp.Name()
	_, _ = tmp.WriteString(time.Now().UTC().Format(time.RFC3339) + "\n")
	err = tmp.Close()
	if err != nil {
		_ = os.Remove(tmpName)

		return fmt.Errorf("close last-success: %w", err)
	}
	err = os.Rename(tmpName, path)
	if err != nil {
		_ = os.Remove(tmpName)

		return fmt.Errorf("rename last-success: %w", err)
	}

	return nil
}

// lastSuccessAge returns how long ago the marker was written. Missing
// file returns errLastSuccessMissing so callers can fail-closed.
func lastSuccessAge(path string) (time.Duration, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, fmt.Errorf("%w: %s", errLastSuccessMissing, path)
		}

		return 0, fmt.Errorf("stat last-success %s: %w", path, err)
	}

	return time.Since(info.ModTime()), nil
}

// healthServer hosts /live, /ready, /health, and /debug/pprof/* on its
// own *http.Server so callers control graceful shutdown. The wrapped
// healthcheck.HealthCheck owns the background check loop.
type healthServer struct {
	hc              healthcheck.HealthCheck
	srv             *http.Server
	lastSuccessPath string
	addr            string
	maxAge          time.Duration
	stopBgOnce      sync.Once
}

// newHealthServer wires the healthcheck library against a "last-success
// younger than maxAge" check and assembles a *http.Server. Addr ":0"
// asks the OS for a free port — useful in tests; production should pass
// a concrete address.
func newHealthServer(addr, lastSuccessPath string, maxAge time.Duration) (*healthServer, error) {
	// No WithBackCheck — the last-success check is a single os.Stat,
	// orders of magnitude cheaper than the 5s cache window. Running
	// synchronously on each /ready hit reports current state without
	// the "before first cycle returns 102" pitfall the background mode
	// surfaces.
	hc := healthcheck.New(
		healthcheck.WithSuccessStatus(http.StatusOK),
		healthcheck.WithErrorStatus(http.StatusServiceUnavailable),
	)

	checkErr := hc.Add("last-success", lastSuccessPath, func(_ context.Context) error {
		age, err := lastSuccessAge(lastSuccessPath)
		if err != nil {
			return err
		}
		if age > maxAge {
			return fmt.Errorf("%w: %s ago > %s", errLastSuccessStale, age.Round(time.Second), maxAge)
		}

		return nil
	})
	if checkErr != nil {
		return nil, fmt.Errorf("register last-success check: %w", checkErr)
	}

	mux := http.NewServeMux()
	// /live is dependency-free: only proves the process is alive.
	// A failing downstream MUST NOT trigger a pod restart.
	mux.HandleFunc(healthcheck.HandlerLive, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc(healthcheck.HandlerReady, hc.HandlerHealth)
	mux.HandleFunc(healthcheck.HandlerStartup, hc.HandlerHealth)
	mux.HandleFunc(healthcheck.HandlerHealthCheck, hc.HandlerHealth)
	mux.HandleFunc(healthcheck.HandlerDebug, hc.HandlerPProf)

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	return &healthServer{
		hc:              hc,
		srv:             srv,
		lastSuccessPath: lastSuccessPath,
		addr:            addr,
		maxAge:          maxAge,
	}, nil
}

// handler is exposed for in-process tests that want to drive the mux
// without a real listener.
func (h *healthServer) handler() http.Handler {
	return h.srv.Handler
}

// start kicks off the background check loop and starts the HTTP
// listener bound to h.addr. Returns nil-or-ErrServerClosed-or-error
// when serve() exits; meant to be called from a goroutine in the
// caller.
func (h *healthServer) start(ctx context.Context) error {
	h.hc.Start()
	lc := &net.ListenConfig{}
	ln, err := lc.Listen(ctx, "tcp", h.addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", h.addr, err)
	}

	return h.serve(ln)
}

// serve runs the embedded http.Server against an existing listener;
// useful from tests where the listener picks an ephemeral port.
func (h *healthServer) serve(ln net.Listener) error {
	err := h.srv.Serve(ln)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}

	return nil
}

// shutdown stops the HTTP listener gracefully (waiting for in-flight
// requests up to ctx deadline) and stops the background check loop.
func (h *healthServer) shutdown(ctx context.Context) error {
	err := h.srv.Shutdown(ctx)
	h.stopBackgroundLoop()
	if err != nil {
		return fmt.Errorf("http shutdown: %w", err)
	}

	return nil
}

// stopBackgroundLoop wraps hc.Shutdown so callers (tests, dual paths)
// don't double-stop.
func (h *healthServer) stopBackgroundLoop() {
	h.stopBgOnce.Do(func() {
		h.hc.Shutdown()
	})
}
