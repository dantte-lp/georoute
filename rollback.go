package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// frrSyntaxCheck validates a candidate FRR config without applying it.
// Variable so tests can substitute a stub; the production binding runs
// `vtysh -C -f <path>` which exits non-zero on any parse error.
var frrSyntaxCheck = defaultFrrSyntaxCheck

// frrReload runs frr-reload.py against the live config path. Variable
// for tests, same reason as frrSyntaxCheck. The script path and
// timeout come from applyOpts; the production binding wraps
// /usr/lib/frr/frr-reload.py.
var frrReload = defaultFrrReload

// applyOpts captures the configurable behavior of applyFRRConfigOpts.
// Plain struct, zero-value-friendly. applyFRRConfig is the common-case
// wrapper around applyFRRConfigOpts{} so callers don't have to spell
// out defaults.
type applyOpts struct {
	frrReloadScript  string
	skipReload       bool
	frrReloadTimeout time.Duration
}

// applyFRRConfig wraps applyFRRConfigOpts with the production defaults:
// run syntax check, atomic-write, reload, rollback on failure.
func applyFRRConfig(ctx context.Context, livePath, newContents string) error {
	return applyFRRConfigOpts(ctx, livePath, newContents, applyOpts{
		frrReloadScript:  defaultFrrReloadScript,
		frrReloadTimeout: defaultFrrReloadTimeout,
	})
}

// applyFRRConfigOpts safely swaps a FRR config file in place and
// triggers a reload. The full sequence:
//
//  1. Write candidate to a temp file in the same directory (atomicWrite
//     handles permissions + rename semantics).
//  2. Pre-validate with `vtysh -C -f tmp`. On failure, remove temp and
//     return — live file is untouched.
//  3. Capture a backup of the current live file in memory so a reload
//     failure can be undone byte-exactly.
//  4. atomicWrite the new contents over the live path.
//  5. Run frr-reload. On success, return.
//  6. On reload failure, atomicWrite the backup back, then run
//     frr-reload again to resync FRR's in-memory state. Return a
//     joined error that surfaces both the primary reload error and
//     (if it fired) the rollback reload error.
//
// skipReload short-circuits steps 2 and 5: the file is written but no
// external tools are invoked. The reload=false CLI flag uses this path.
func applyFRRConfigOpts(ctx context.Context, livePath, newContents string, opts applyOpts) error {
	if opts.skipReload {
		return atomicWrite(livePath, []byte(newContents))
	}

	// Stage to a temp file in the same dir so syntax check operates on
	// the exact same bytes that will be live.
	dir := filepath.Dir(livePath)
	tmp, err := os.CreateTemp(dir, filepath.Base(livePath)+".staging.*")
	if err != nil {
		return fmt.Errorf("create staging temp: %w", err)
	}
	stagedPath := tmp.Name()
	defer func() {
		_ = os.Remove(stagedPath) // remove on success path too (already renamed)
	}()
	_, err = tmp.WriteString(newContents)
	if err != nil {
		_ = tmp.Close()

		return fmt.Errorf("write staging: %w", err)
	}
	err = tmp.Close()
	if err != nil {
		return fmt.Errorf("close staging: %w", err)
	}

	err = frrSyntaxCheck(ctx, stagedPath)
	if err != nil {
		return fmt.Errorf("syntax check: %w", err)
	}

	// Capture the current live contents so we can restore on reload
	// failure. Missing file is OK — first-run path.
	backup, readErr := os.ReadFile(livePath)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return fmt.Errorf("read live for backup: %w", readErr)
	}

	err = atomicWrite(livePath, []byte(newContents))
	if err != nil {
		return fmt.Errorf("write live: %w", err)
	}

	reloadErr := frrReload(ctx, livePath, opts.frrReloadScript, opts.frrReloadTimeout)
	if reloadErr == nil {
		return nil
	}

	// Rollback: restore the original bytes and re-run frr-reload to
	// resync. A failure here is an incident; surface both errors.
	if backup == nil {
		// We don't have a prior good state to roll back to. Surface the
		// reload error directly; leaving the new (broken) file in place
		// at least gives operators a stable artifact to read.
		return fmt.Errorf("reload: %w (no backup to restore — fresh install path)", reloadErr)
	}
	restoreErr := atomicWrite(livePath, backup)
	if restoreErr != nil {
		return errors.Join(
			fmt.Errorf("reload: %w", reloadErr),
			fmt.Errorf("restore: %w", restoreErr),
		)
	}
	rollbackReloadErr := frrReload(ctx, livePath, opts.frrReloadScript, opts.frrReloadTimeout)
	if rollbackReloadErr != nil {
		return errors.Join(
			fmt.Errorf("reload: %w", reloadErr),
			fmt.Errorf("rollback reload: %w", rollbackReloadErr),
		)
	}

	return fmt.Errorf("reload failed, backup restored: %w", reloadErr)
}

// defaultFrrSyntaxCheck runs `vtysh -C -f <path>` to validate a config
// without committing it. The wrapped CommandContext kills it on
// cancellation.
func defaultFrrSyntaxCheck(ctx context.Context, path string) error {
	cmd := exec.CommandContext(ctx, "/usr/bin/vtysh", "-C", "-f", path) //nolint:gosec // path is constant + ctx-bounded
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = []string{envPATH}
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("vtysh -C: %w", err)
	}

	return nil
}

// defaultFrrReload runs frr-reload.py against path. Same wrap as the
// previous inline reloadFRR; kept here so applyFRRConfig has a single
// hook variable. Script path + timeout flow from the cliFlags via
// applyOpts so the operator can override either at the CLI.
func defaultFrrReload(ctx context.Context, path, scriptPath string, timeout time.Duration) error {
	reloadCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(reloadCtx, scriptPath, "--reload", path) //nolint:gosec // script path + ctx-bounded; operator owns the override
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = []string{envPATH}
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("frr-reload: %w", err)
	}

	return nil
}
