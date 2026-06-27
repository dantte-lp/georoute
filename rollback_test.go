package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var (
	errTestSyntax        = errors.New("vtysh: % Unknown command")
	errTestReloadPrimary = errors.New("frr-reload exit 1")
	errTestReloadSecond  = errors.New("frr-reload exit 2")
)

const testFilePerm os.FileMode = 0o600

// fakeFrrEnv installs stub frrSyntaxCheck + frrReload that record calls
// and return injected errors. Restored automatically via t.Cleanup.
// fieldalignment is informal for a test-only stub.
//
//nolint:govet // fieldalignment is informal for test-only stubs
type fakeFrrEnv struct {
	reloadErrs    []error // consumed in order; each reload call pops one
	reloadedPaths []string
	syntaxErr     error
	syntaxCalls   int
	reloadCalls   int
}

func installFakeFRR(t *testing.T, env *fakeFrrEnv) {
	t.Helper()
	prevSyntax := frrSyntaxCheck
	prevReload := frrReload
	frrSyntaxCheck = func(_ context.Context, _ string) error {
		env.syntaxCalls++

		return env.syntaxErr
	}
	frrReload = func(_ context.Context, path, _ string, _ time.Duration) error {
		env.reloadCalls++
		env.reloadedPaths = append(env.reloadedPaths, path)
		if env.reloadCalls-1 < len(env.reloadErrs) {
			return env.reloadErrs[env.reloadCalls-1]
		}

		return nil
	}
	t.Cleanup(func() {
		frrSyntaxCheck = prevSyntax
		frrReload = prevReload
	})
}

// TestApplyFRRConfig_HappyPath — syntax check passes, write succeeds,
// frr-reload returns 0. Live file ends up with the new contents and no
// rollback artifacts remain.
func TestApplyFRRConfig_HappyPath(t *testing.T) {
	env := &fakeFrrEnv{}
	installFakeFRR(t, env)

	dir := t.TempDir()
	live := filepath.Join(dir, "frr.conf")
	err := os.WriteFile(live, []byte("old contents\n"), testFilePerm)
	if err != nil {
		t.Fatalf("seed live config: %v", err)
	}

	err = applyFRRConfig(context.Background(), live, "new contents\n")
	if err != nil {
		t.Fatalf("applyFRRConfig: %v", err)
	}

	got, err := os.ReadFile(live)
	if err != nil {
		t.Fatalf("read live: %v", err)
	}
	if string(got) != "new contents\n" {
		t.Errorf("live config not updated: %q", got)
	}
	if env.syntaxCalls != 1 || env.reloadCalls != 1 {
		t.Errorf("expected 1 syntax + 1 reload, got %d / %d", env.syntaxCalls, env.reloadCalls)
	}
}

// TestApplyFRRConfig_SyntaxFailureLeavesLiveUntouched — vtysh -C fails
// on the candidate temp file before the live file is replaced, so the
// running config never changes.
func TestApplyFRRConfig_SyntaxFailureLeavesLiveUntouched(t *testing.T) {
	env := &fakeFrrEnv{syntaxErr: errTestSyntax}
	installFakeFRR(t, env)

	dir := t.TempDir()
	live := filepath.Join(dir, "frr.conf")
	err := os.WriteFile(live, []byte("good contents\n"), testFilePerm)
	if err != nil {
		t.Fatalf("seed live config: %v", err)
	}

	err = applyFRRConfig(context.Background(), live, "bogus contents\n")
	if !errors.Is(err, errTestSyntax) {
		t.Fatalf("expected errTestSyntax wrapped, got %v", err)
	}

	got, err := os.ReadFile(live)
	if err != nil {
		t.Fatalf("read live: %v", err)
	}
	if string(got) != "good contents\n" {
		t.Errorf("live config touched on syntax failure: %q", got)
	}
	if env.reloadCalls != 0 {
		t.Errorf("frr-reload should not be called when syntax check fails (got %d)", env.reloadCalls)
	}
}

// TestApplyFRRConfig_ReloadFailureRestoresBackup — vtysh -C passes,
// atomic-write succeeds, frr-reload returns non-zero. We must restore
// the pre-change live contents AND run frr-reload a second time to
// resync the in-memory state.
func TestApplyFRRConfig_ReloadFailureRestoresBackup(t *testing.T) {
	env := &fakeFrrEnv{reloadErrs: []error{errTestReloadPrimary, nil}} // first fails, rollback succeeds
	installFakeFRR(t, env)

	dir := t.TempDir()
	live := filepath.Join(dir, "frr.conf")
	err := os.WriteFile(live, []byte("good contents\n"), testFilePerm)
	if err != nil {
		t.Fatalf("seed live config: %v", err)
	}

	err = applyFRRConfig(context.Background(), live, "new contents\n")
	if err == nil {
		t.Fatal("expected error when frr-reload fails")
	}
	if !errors.Is(err, errTestReloadPrimary) {
		t.Errorf("expected errTestReloadPrimary wrapped, got %v", err)
	}

	// Live should be restored to the pre-attempt contents.
	got, err := os.ReadFile(live)
	if err != nil {
		t.Fatalf("read live: %v", err)
	}
	if string(got) != "good contents\n" {
		t.Errorf("backup not restored: %q", got)
	}

	// frr-reload should have been called twice: once for the candidate
	// (failing), once for the rollback (succeeding).
	if env.reloadCalls != 2 {
		t.Errorf("expected 2 reload calls (apply + rollback), got %d", env.reloadCalls)
	}
}

// TestApplyFRRConfig_DoubleReloadFailureJoinsErrors — both the initial
// reload and the rollback reload fail. The returned error must carry
// both underlying causes so an operator post-mortem can see what hit.
func TestApplyFRRConfig_DoubleReloadFailureJoinsErrors(t *testing.T) {
	env := &fakeFrrEnv{reloadErrs: []error{errTestReloadPrimary, errTestReloadSecond}}
	installFakeFRR(t, env)

	dir := t.TempDir()
	live := filepath.Join(dir, "frr.conf")
	err := os.WriteFile(live, []byte("good contents\n"), testFilePerm)
	if err != nil {
		t.Fatalf("seed live config: %v", err)
	}

	err = applyFRRConfig(context.Background(), live, "new contents\n")
	if err == nil {
		t.Fatal("expected error on double failure")
	}
	if !errors.Is(err, errTestReloadPrimary) {
		t.Errorf("primary error not preserved: %v", err)
	}
	if !errors.Is(err, errTestReloadSecond) {
		t.Errorf("rollback error not preserved: %v", err)
	}
}

// TestApplyFRRConfig_SkipsReloadWhenDisabled — when reloadOK is false
// (test/manual use) we still write the new file (so subsequent reloads
// can pick it up) but never call frrReload nor frrSyntaxCheck.
func TestApplyFRRConfig_SkipsReloadWhenDisabled(t *testing.T) {
	env := &fakeFrrEnv{}
	installFakeFRR(t, env)

	dir := t.TempDir()
	live := filepath.Join(dir, "frr.conf")
	err := os.WriteFile(live, []byte("old\n"), testFilePerm)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	err = applyFRRConfigOpts(context.Background(), live, "new\n", applyOpts{skipReload: true})
	if err != nil {
		t.Fatalf("applyFRRConfigOpts: %v", err)
	}
	got, _ := os.ReadFile(live)
	if string(got) != "new\n" {
		t.Errorf("file not written: %q", got)
	}
	if env.syntaxCalls != 0 || env.reloadCalls != 0 {
		t.Errorf("expected 0 syntax + 0 reload, got %d / %d", env.syntaxCalls, env.reloadCalls)
	}
}
