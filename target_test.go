package main

import (
	"context"
	"log/slog"
	"net/netip"
	"strings"
	"testing"
)

func mustPrefix(t *testing.T, s string) netip.Prefix {
	t.Helper()
	p, err := netip.ParsePrefix(s)
	if err != nil {
		t.Fatalf("ParsePrefix(%q): %v", s, err)
	}

	return p
}

func TestRenderIOSXEPrefixList(t *testing.T) {
	t.Parallel()
	prefixes := []netip.Prefix{
		mustPrefix(t, "203.0.113.0/24"),
		mustPrefix(t, "198.51.100.0/24"),
	}
	got := renderIOSXEPrefixList("PL-GEOROUTE-RU-V4", prefixes, 10)
	want := "ip prefix-list PL-GEOROUTE-RU-V4 seq 10 permit 203.0.113.0/24\n" +
		"ip prefix-list PL-GEOROUTE-RU-V4 seq 20 permit 198.51.100.0/24"
	if got != want {
		t.Fatalf("renderIOSXEPrefixList() = %q, want %q", got, want)
	}
}

func TestRenderIOSXECandidate(t *testing.T) {
	t.Parallel()
	prefixes := []netip.Prefix{mustPrefix(t, "203.0.113.0/24")}
	got := renderIOSXECandidate("PL-GEOROUTE-RU-V4", "RM-GEOROUTE-RU-OUT", prefixes)
	want := "ip prefix-list PL-GEOROUTE-RU-V4 seq 10 permit 203.0.113.0/24\n" +
		"route-map RM-GEOROUTE-RU-OUT permit 10\n" +
		" match ip address prefix-list PL-GEOROUTE-RU-V4"
	if got != want {
		t.Fatalf("renderIOSXECandidate() = %q, want %q", got, want)
	}
}

func TestRenderPrefixLines(t *testing.T) {
	t.Parallel()
	prefixes := []netip.Prefix{
		mustPrefix(t, "203.0.113.0/24"),
		mustPrefix(t, "198.51.100.0/24"),
	}
	got := renderPrefixLines(prefixes)
	want := "203.0.113.0/24\n198.51.100.0/24"
	if got != want {
		t.Fatalf("renderPrefixLines() = %q, want %q", got, want)
	}
}

func TestRunTargetRejectsApplyMode(t *testing.T) {
	t.Parallel()
	flags := cliFlags{
		target:          targetIOSXE,
		targetMode:      targetModeApply,
		targetOutput:    targetOutputStdout,
		iosxePrefixList: "PL-GEOROUTE-RU-V4",
		iosxeRouteMap:   "RM-GEOROUTE-RU-OUT",
	}
	logger := slog.New(slog.DiscardHandler)
	err := runTarget(context.Background(), flags, logger, []netip.Prefix{
		mustPrefix(t, "203.0.113.0/24"),
	})
	if err == nil {
		t.Fatal("runTarget() error = nil, want apply-mode error")
	}
	if !strings.Contains(err.Error(), `only supports "render"`) {
		t.Fatalf("runTarget() error = %q, want render-mode guard", err)
	}
}

func TestValidateFRRTargetModeRejectsRender(t *testing.T) {
	t.Parallel()
	err := validateFRRTargetMode(targetModeRender)
	if err == nil {
		t.Fatal("validateFRRTargetMode() error = nil, want render-mode error")
	}
	if !strings.Contains(err.Error(), `only supports "apply"`) {
		t.Fatalf("validateFRRTargetMode() error = %q, want apply-mode guard", err)
	}
}
