package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// TestNewLogger_JSONFormat — JSON output must be a single-line JSON
// object per log entry containing the canonical slog keys (level, msg,
// time) plus any structured attributes the caller supplied.
func TestNewLogger_JSONFormat(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger, err := newLogger("json", "info", &buf)
	if err != nil {
		t.Fatalf("newLogger: %v", err)
	}
	logger.Info("hello", slog.String("country", "ru"))

	line := buf.String()
	if !strings.Contains(line, "{") || !strings.HasSuffix(strings.TrimSpace(line), "}") {
		t.Fatalf("not JSON: %q", line)
	}
	var rec map[string]any
	err = json.Unmarshal([]byte(line), &rec)
	if err != nil {
		t.Fatalf("json decode: %v (line: %s)", err, line)
	}
	for _, key := range []string{"time", "level", "msg", "country"} {
		if _, ok := rec[key]; !ok {
			t.Errorf("missing key %q in: %v", key, rec)
		}
	}
	if rec["msg"] != "hello" {
		t.Errorf("msg=%v, want hello", rec["msg"])
	}
	if rec["country"] != "ru" {
		t.Errorf("country=%v, want ru", rec["country"])
	}
}

// TestNewLogger_TextFormat — text format must produce human-readable
// key=value pairs (the slog TextHandler default).
func TestNewLogger_TextFormat(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger, err := newLogger("text", "info", &buf)
	if err != nil {
		t.Fatalf("newLogger: %v", err)
	}
	logger.Info("hello", slog.String("country", "ru"))

	got := buf.String()
	if !strings.Contains(got, "msg=hello") {
		t.Errorf("expected msg=hello in: %s", got)
	}
	if !strings.Contains(got, "country=ru") {
		t.Errorf("expected country=ru in: %s", got)
	}
	if strings.Contains(strings.TrimSpace(got), "{") {
		t.Errorf("text format produced JSON: %s", got)
	}
}

// TestNewLogger_LevelFiltering — debug messages must be dropped when
// the configured level is info.
func TestNewLogger_LevelFiltering(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger, err := newLogger("json", "info", &buf)
	if err != nil {
		t.Fatalf("newLogger: %v", err)
	}
	logger.Debug("noisy", slog.String("shouldnt", "appear"))
	logger.Info("important", slog.String("should", "appear"))

	got := buf.String()
	if strings.Contains(got, "noisy") {
		t.Errorf("debug leaked through at level=info: %s", got)
	}
	if !strings.Contains(got, "important") {
		t.Errorf("info dropped: %s", got)
	}
}

// TestNewLogger_InvalidFormat — bad format must return a sentinel-able
// error so main.go can fail fast with an operator-friendly message.
func TestNewLogger_InvalidFormat(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	_, err := newLogger("yaml", "info", &buf)
	if !errors.Is(err, errInvalidLogFormat) {
		t.Errorf("want errInvalidLogFormat, got %v", err)
	}
}

// TestNewLogger_InvalidLevel — same, but for the level flag.
func TestNewLogger_InvalidLevel(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	_, err := newLogger("json", "loud", &buf)
	if !errors.Is(err, errInvalidLogLevel) {
		t.Errorf("want errInvalidLogLevel, got %v", err)
	}
}

// TestRunID_GeneratedAndStable — newRunID produces a UUID-shaped
// string per call, and the same id is preserved when threaded via
// withRunID into a derived logger.
func TestRunID_GeneratedAndStable(t *testing.T) {
	t.Parallel()
	id1 := newRunID()
	id2 := newRunID()
	if id1 == id2 {
		t.Errorf("run id should be unique per call (got %q twice)", id1)
	}
	if len(id1) < 32 {
		t.Errorf("run id too short: %q", id1)
	}

	var buf bytes.Buffer
	logger, err := newLogger("json", "info", &buf)
	if err != nil {
		t.Fatalf("newLogger: %v", err)
	}
	logger = withRunID(logger, id1)
	logger.Info("first")
	logger.Info("second")

	for line := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
		var rec map[string]any
		err := json.Unmarshal([]byte(line), &rec)
		if err != nil {
			t.Fatalf("decode %q: %v", line, err)
		}
		if rec["run_id"] != id1 {
			t.Errorf("run_id=%v, want %q (line: %s)", rec["run_id"], id1, line)
		}
	}
}
