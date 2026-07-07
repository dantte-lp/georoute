package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"strings"
)

const (
	defaultTarget       = "frr"
	defaultTargetMode   = "apply"
	defaultSeqStep      = 10
	targetFRR           = "frr"
	targetFile          = "file"
	targetIOSXE         = "iosxe"
	targetModeApply     = "apply"
	targetModeRender    = "render"
	targetOutputStdout  = "-"
	iosxePrefixListBase = "PL-GEOROUTE-%s-V4"
	iosxeRouteMapBase   = "RM-GEOROUTE-%s-OUT"
)

var (
	errUnsupportedTarget     = errors.New("unsupported target")
	errUnsupportedTargetMode = errors.New("unsupported target mode")
)

func renderIOSXEPrefixList(name string, prefixes []netip.Prefix, seqStep int) string {
	if len(prefixes) == 0 {
		return ""
	}
	if seqStep <= 0 {
		seqStep = defaultSeqStep
	}

	var sb strings.Builder
	for i, p := range prefixes {
		_, _ = fmt.Fprintf(&sb, "ip prefix-list %s seq %d permit %s\n", name, (i+1)*seqStep, p.String())
	}

	return strings.TrimRight(sb.String(), "\n")
}

func renderIOSXERouteMap(name, prefixList string) string {
	if name == "" || prefixList == "" {
		return ""
	}

	return fmt.Sprintf("route-map %s permit 10\n match ip address prefix-list %s", name, prefixList)
}

func renderIOSXECandidate(prefixList, routeMap string, prefixes []netip.Prefix) string {
	parts := []string{
		renderIOSXEPrefixList(prefixList, prefixes, defaultSeqStep),
		renderIOSXERouteMap(routeMap, prefixList),
	}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}

	return strings.Join(out, "\n")
}

func renderPrefixLines(prefixes []netip.Prefix) string {
	if len(prefixes) == 0 {
		return ""
	}

	lines := make([]string, len(prefixes))
	for i, prefix := range prefixes {
		lines[i] = prefix.String()
	}

	return strings.Join(lines, "\n")
}

func validateFRRTargetMode(mode string) error {
	if mode == targetModeApply {
		return nil
	}

	return fmt.Errorf("%w: target %q only supports %q", errUnsupportedTargetMode, targetFRR, targetModeApply)
}

func runTarget(ctx context.Context, f cliFlags, logger *slog.Logger, v4Agg []netip.Prefix) error {
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("target context: %w", err)
	}

	var candidate string
	switch f.target {
	case targetFile:
		candidate = renderPrefixLines(v4Agg)
	case targetIOSXE:
		candidate = renderIOSXECandidate(f.iosxePrefixList, f.iosxeRouteMap, v4Agg)
	default:
		return fmt.Errorf("%w: %q", errUnsupportedTarget, f.target)
	}

	printSample(logger, "target-"+f.target, candidate)
	logger.Info("target render summary",
		slog.String("target", f.target),
		slog.String("mode", f.targetMode),
		slog.Int("v4_count", len(v4Agg)),
		slog.String("candidate_hash", hashOf(candidate)))

	if f.dryRun {
		return nil
	}
	if f.targetMode != targetModeRender {
		return fmt.Errorf("%w: target %q only supports %q in this build",
			errUnsupportedTargetMode, f.target, targetModeRender)
	}

	return writeTargetOutput(f.targetOutput, candidate)
}

func writeTargetOutput(path, contents string) error {
	if path == "" || path == targetOutputStdout {
		_, err := fmt.Fprintln(os.Stdout, contents)
		if err != nil {
			return fmt.Errorf("write stdout: %w", err)
		}

		return nil
	}

	err := atomicWrite(path, []byte(contents+"\n"))
	if err != nil {
		return fmt.Errorf("write target output: %w", err)
	}

	return nil
}
