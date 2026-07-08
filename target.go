package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"strings"
	"time"
)

const (
	defaultTarget            = "frr"
	defaultTargetMode        = "apply"
	defaultSeqStep           = 10
	defaultRouteMapSeq       = 10
	defaultDenySeq           = 9999
	iosxeDenyTailSeq         = 999999
	targetFRR                = "frr"
	targetFile               = "file"
	targetIOSXE              = "iosxe"
	targetModeApply          = "apply"
	targetModeRender         = "render"
	targetOutputStdout       = "-"
	iosxePrefixListBase      = "PL-GEOROUTE-%s-V4"
	iosxeRouteMapBase        = "RM-GEOROUTE-%s-OUT"
	iosxeStaticRouteMapBase  = "RM-GEOROUTE-%s-STATIC"
	iosxeStaticRouteNameBase = "GEOROUTE_%s"
	defaultIOSXESSHPort      = 22
	defaultIOSXESSHTimeout   = 30 * time.Second
)

var (
	errUnsupportedTarget     = errors.New("unsupported target")
	errUnsupportedTargetMode = errors.New("unsupported target mode")
	errApplyPrefixLimit      = errors.New("iosxe apply prefix limit exceeded")
	errIOSXEApplyConfig      = errors.New("iosxe apply configuration incomplete")
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
	_, _ = fmt.Fprintf(&sb, "ip prefix-list %s seq %d deny 0.0.0.0/0 le 32\n", name, iosxeDenyTailSeq)

	return strings.TrimRight(sb.String(), "\n")
}

func renderIOSXERouteMap(name, prefixList string, permitSeq, denySeq int) string {
	if name == "" || prefixList == "" {
		return ""
	}
	if permitSeq <= 0 {
		permitSeq = defaultRouteMapSeq
	}
	if denySeq <= 0 {
		denySeq = defaultDenySeq
	}

	return fmt.Sprintf("route-map %s permit %d\n match ip address prefix-list %s\nroute-map %s deny %d",
		name, permitSeq, prefixList, name, denySeq)
}

func renderIOSXEStaticRoutes(prefixes []netip.Prefix, nextHop, routeName string, tag int) string {
	if len(prefixes) == 0 || nextHop == "" {
		return ""
	}

	var sb strings.Builder
	for _, p := range prefixes {
		addr := p.Masked().Addr()
		if !addr.Is4() {
			continue
		}
		_, _ = fmt.Fprintf(&sb, "ip route %s %s %s",
			addr.String(),
			ipv4MaskDotted(p.Bits()),
			nextHop)
		if tag > 0 {
			_, _ = fmt.Fprintf(&sb, " tag %d", tag)
		}
		if routeName != "" {
			_, _ = fmt.Fprintf(&sb, " name %s", routeName)
		}
		_, _ = sb.WriteString("\n")
	}

	return strings.TrimRight(sb.String(), "\n")
}

func renderIOSXERedistributeStatic(routeMap, prefixList string, permitSeq, denySeq int) string {
	return renderIOSXERouteMap(routeMap, prefixList, permitSeq, denySeq)
}

func renderIOSXEBGPRedistributeStatic(asn, routeMap string) string {
	if asn == "" || routeMap == "" {
		return ""
	}

	return fmt.Sprintf("router bgp %s\n address-family ipv4\n  redistribute static route-map %s\n exit-address-family", asn, routeMap)
}

func ipv4MaskDotted(bits int) string {
	if bits <= 0 {
		return "0.0.0.0"
	}
	if bits >= 32 {
		return "255.255.255.255"
	}

	mask := uint32(0xffffffff) << (32 - bits)

	return fmt.Sprintf("%d.%d.%d.%d",
		byte((mask>>24)&0xff),
		byte((mask>>16)&0xff),
		byte((mask>>8)&0xff),
		byte(mask&0xff))
}

func renderIOSXECandidate(f cliFlags, prefixes []netip.Prefix) string {
	staticRouteMap := ""
	if f.iosxeStaticNextHop != "" || f.iosxeBGPAS != "" {
		staticRouteMap = f.iosxeStaticRouteMap
	}
	parts := []string{
		renderIOSXEPrefixList(f.iosxePrefixList, prefixes, defaultSeqStep),
		renderIOSXEStaticRoutes(prefixes, f.iosxeStaticNextHop, f.iosxeStaticRouteName, f.iosxeStaticTag),
		renderIOSXERedistributeStatic(staticRouteMap, f.iosxePrefixList, defaultRouteMapSeq, f.iosxeRouteMapDenySeq),
		renderIOSXERouteMap(f.iosxeRouteMap, f.iosxePrefixList, f.iosxeRouteMapSeq, f.iosxeRouteMapDenySeq),
		renderIOSXEBGPRedistributeStatic(f.iosxeBGPAS, staticRouteMap),
	}
	if f.iosxeReplacePrefixList && f.iosxePrefixList != "" {
		parts = append([]string{"no ip prefix-list " + f.iosxePrefixList}, parts...)
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
		candidate = renderIOSXECandidate(f, v4Agg)
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
		err = validateIOSXEApplyGuard(f, len(v4Agg))
		if err != nil {
			return err
		}

		return applyIOSXEOverSSH(ctx, f, candidate, logger)
	}

	return writeTargetOutput(f.targetOutput, candidate)
}

func validateIOSXEApplyGuard(f cliFlags, prefixCount int) error {
	if f.targetMode != targetModeApply {
		return fmt.Errorf("%w: target %q supports %q or %q",
			errUnsupportedTargetMode, f.target, targetModeRender, targetModeApply)
	}
	if f.iosxeApplyMaxPrefixes > 0 && prefixCount > f.iosxeApplyMaxPrefixes {
		return fmt.Errorf("%w: prefixes=%d max=%d", errApplyPrefixLimit, prefixCount, f.iosxeApplyMaxPrefixes)
	}
	if f.iosxeSSHHost == "" {
		return fmt.Errorf("%w: --iosxe-ssh-host is required", errIOSXEApplyConfig)
	}
	if f.iosxeSSHUser == "" {
		return fmt.Errorf("%w: --iosxe-ssh-user is required", errIOSXEApplyConfig)
	}
	if f.iosxeSSHKeyFile == "" {
		return fmt.Errorf("%w: --iosxe-ssh-key-file is required", errIOSXEApplyConfig)
	}
	if f.iosxeKnownHostsFile == "" {
		return fmt.Errorf("%w: --iosxe-known-hosts-file is required", errIOSXEApplyConfig)
	}

	return nil
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
