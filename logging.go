package main

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/google/uuid"
)

// Logging-related sentinel errors. The flag layer maps them to
// human-readable exit messages so operators see the offending value.
var (
	errInvalidLogFormat = errors.New("invalid log format")
	errInvalidLogLevel  = errors.New("invalid log level")
)

const (
	logFormatText = "text"
	logFormatJSON = "json"

	logAttrRunID    = "run_id"
	defaultLogLevel = "info"
)

// newLogger constructs a *slog.Logger that writes to w with the
// requested format ("text" or "json") and minimum level. Unknown values
// return a sentinel error so the caller can surface the misconfiguration.
func newLogger(format, level string, w io.Writer) (*slog.Logger, error) {
	lvl, err := parseLogLevel(level)
	if err != nil {
		return nil, err
	}
	opts := &slog.HandlerOptions{Level: lvl}
	var handler slog.Handler
	switch format {
	case logFormatText:
		handler = slog.NewTextHandler(w, opts)
	case logFormatJSON:
		handler = slog.NewJSONHandler(w, opts)
	default:
		return nil, fmt.Errorf("%w: %q", errInvalidLogFormat, format)
	}

	return slog.New(handler), nil
}

// parseLogLevel maps the string forms slog supports to slog.Level. The
// list is exhaustive on purpose — anything else is a programmer/operator
// error and must surface, not silently default.
func parseLogLevel(s string) (slog.Level, error) {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("%w: %q (want debug|info|warn|error)", errInvalidLogLevel, s)
	}
}

// newRunID returns a fresh per-run identifier suitable for log↔metric
// correlation. UUIDv4 (random) avoids any temporal coupling between
// concurrent runs.
func newRunID() string {
	return uuid.NewString()
}

// withRunID returns a child logger that attaches run_id to every
// record. Pattern: generate the id at the top of run(), thread the
// derived logger through downstream calls. Tests can assert that every
// emitted record carries the same id by parsing the JSON output.
func withRunID(logger *slog.Logger, id string) *slog.Logger {
	return logger.With(slog.String(logAttrRunID, id))
}
