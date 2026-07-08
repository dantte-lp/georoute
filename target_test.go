package main

import (
	"context"
	"log/slog"
	"net/netip"
	"strings"
	"testing"
)

const (
	iosxeRUPrefixListTest      = "PL-GEOROUTE-RU-V4"
	iosxeRUDefaultRouteMapTest = "RM-GEOROUTE-RU-OUT"
	iosxeRUOutRouteMapTest     = "RM-RU-GEOROUTE-OUT"
	iosxeRUStaticRouteMapTest  = "RM-GEOROUTE-RU-STATIC"
	iosxeRUStaticRouteNameTest = "GEOROUTE_RU"
	iosxeGlobalToTransitTest   = "RM-GLOBAL-TO-TRANSIT"
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
	got := renderIOSXEPrefixList(iosxeRUPrefixListTest, prefixes, 10)
	want := "ip prefix-list " + iosxeRUPrefixListTest + " seq 10 permit 203.0.113.0/24\n" +
		"ip prefix-list " + iosxeRUPrefixListTest + " seq 20 permit 198.51.100.0/24\n" +
		"ip prefix-list " + iosxeRUPrefixListTest + " seq 999999 deny 0.0.0.0/0 le 32"
	if got != want {
		t.Fatalf("renderIOSXEPrefixList() = %q, want %q", got, want)
	}
}

func TestRenderIOSXEReplaceCandidate(t *testing.T) {
	t.Parallel()
	prefixes := []netip.Prefix{mustPrefix(t, "203.0.113.0/24")}
	got := renderIOSXECandidate(cliFlags{
		iosxePrefixList:        iosxeRUPrefixListTest,
		iosxeRouteMap:          iosxeRUDefaultRouteMapTest,
		iosxeReplacePrefixList: true,
		iosxeRouteMapSeq:       10,
		iosxeRouteMapDenySeq:   9999,
	}, prefixes)
	want := "no ip prefix-list " + iosxeRUPrefixListTest + "\n" +
		"ip prefix-list " + iosxeRUPrefixListTest + " seq 10 permit 203.0.113.0/24\n" +
		"ip prefix-list " + iosxeRUPrefixListTest + " seq 999999 deny 0.0.0.0/0 le 32\n" +
		"route-map " + iosxeRUDefaultRouteMapTest + " permit 10\n" +
		" match ip address prefix-list " + iosxeRUPrefixListTest + "\n" +
		"route-map " + iosxeRUDefaultRouteMapTest + " deny 9999"
	if got != want {
		t.Fatalf("renderIOSXECandidate() = %q, want %q", got, want)
	}
}

func TestRenderIOSXECandidate(t *testing.T) {
	t.Parallel()
	prefixes := []netip.Prefix{mustPrefix(t, "203.0.113.0/24")}
	got := renderIOSXECandidate(cliFlags{
		iosxePrefixList:      iosxeRUPrefixListTest,
		iosxeRouteMap:        iosxeRUDefaultRouteMapTest,
		iosxeRouteMapSeq:     10,
		iosxeRouteMapDenySeq: 9999,
	}, prefixes)
	want := "ip prefix-list " + iosxeRUPrefixListTest + " seq 10 permit 203.0.113.0/24\n" +
		"ip prefix-list " + iosxeRUPrefixListTest + " seq 999999 deny 0.0.0.0/0 le 32\n" +
		"route-map " + iosxeRUDefaultRouteMapTest + " permit 10\n" +
		" match ip address prefix-list " + iosxeRUPrefixListTest + "\n" +
		"route-map " + iosxeRUDefaultRouteMapTest + " deny 9999"
	if got != want {
		t.Fatalf("renderIOSXECandidate() = %q, want %q", got, want)
	}
}

func TestRenderIOSXECandidateWithRouteMapSequence(t *testing.T) {
	t.Parallel()
	prefixes := []netip.Prefix{mustPrefix(t, "203.0.113.0/24")}
	got := renderIOSXECandidate(cliFlags{
		iosxePrefixList:        iosxeRUPrefixListTest,
		iosxeRouteMap:          iosxeRUOutRouteMapTest,
		iosxeReplacePrefixList: true,
		iosxeRouteMapSeq:       20,
		iosxeRouteMapDenySeq:   9999,
	}, prefixes)
	want := "no ip prefix-list " + iosxeRUPrefixListTest + "\n" +
		"ip prefix-list " + iosxeRUPrefixListTest + " seq 10 permit 203.0.113.0/24\n" +
		"ip prefix-list " + iosxeRUPrefixListTest + " seq 999999 deny 0.0.0.0/0 le 32\n" +
		"route-map RM-RU-GEOROUTE-OUT permit 20\n" +
		" match ip address prefix-list " + iosxeRUPrefixListTest + "\n" +
		"route-map RM-RU-GEOROUTE-OUT deny 9999"
	if got != want {
		t.Fatalf("renderIOSXECandidate() = %q, want %q", got, want)
	}
}

func TestRenderIOSXECandidateWithStaticOrigin(t *testing.T) {
	t.Parallel()
	prefixes := []netip.Prefix{
		mustPrefix(t, "203.0.113.0/24"),
		mustPrefix(t, "198.51.100.0/25"),
	}
	got := renderIOSXECandidate(cliFlags{
		iosxePrefixList:      iosxeRUPrefixListTest,
		iosxeRouteMap:        iosxeRUOutRouteMapTest,
		iosxeStaticNextHop:   "192.0.2.1",
		iosxeStaticRouteMap:  iosxeRUStaticRouteMapTest,
		iosxeStaticRouteName: iosxeRUStaticRouteNameTest,
		iosxeBGPAS:           "4200099722",
		iosxeRouteMapSeq:     20,
		iosxeRouteMapDenySeq: 9999,
		iosxeStaticTag:       65001,
	}, prefixes)
	want := "ip prefix-list " + iosxeRUPrefixListTest + " seq 10 permit 203.0.113.0/24\n" +
		"ip prefix-list " + iosxeRUPrefixListTest + " seq 20 permit 198.51.100.0/25\n" +
		"ip prefix-list " + iosxeRUPrefixListTest + " seq 999999 deny 0.0.0.0/0 le 32\n" +
		"ip route 203.0.113.0 255.255.255.0 192.0.2.1 tag 65001 name " + iosxeRUStaticRouteNameTest + "\n" +
		"ip route 198.51.100.0 255.255.255.128 192.0.2.1 tag 65001 name " + iosxeRUStaticRouteNameTest + "\n" +
		"route-map " + iosxeRUStaticRouteMapTest + " permit 10\n" +
		" match ip address prefix-list " + iosxeRUPrefixListTest + "\n" +
		"route-map " + iosxeRUStaticRouteMapTest + " deny 9999\n" +
		"route-map RM-RU-GEOROUTE-OUT permit 20\n" +
		" match ip address prefix-list " + iosxeRUPrefixListTest + "\n" +
		"route-map RM-RU-GEOROUTE-OUT deny 9999\n" +
		"router bgp 4200099722\n" +
		" address-family ipv4\n" +
		"  redistribute static route-map " + iosxeRUStaticRouteMapTest + "\n" +
		" exit-address-family"
	if got != want {
		t.Fatalf("renderIOSXECandidate() = %q, want %q", got, want)
	}
}

func TestRenderIOSXECandidateWithVRFLeakRouteMap(t *testing.T) {
	t.Parallel()
	prefixes := []netip.Prefix{mustPrefix(t, "203.0.113.0/24")}
	got := renderIOSXECandidate(cliFlags{
		iosxePrefixList:         iosxeRUPrefixListTest,
		iosxeRouteMap:           iosxeRUOutRouteMapTest,
		iosxeVRFLeakRouteMap:    iosxeGlobalToTransitTest,
		iosxeRouteMapSeq:        20,
		iosxeVRFLeakRouteMapSeq: 20,
		iosxeRouteMapDenySeq:    9999,
	}, prefixes)
	want := "ip prefix-list " + iosxeRUPrefixListTest + " seq 10 permit 203.0.113.0/24\n" +
		"ip prefix-list " + iosxeRUPrefixListTest + " seq 999999 deny 0.0.0.0/0 le 32\n" +
		"route-map " + iosxeGlobalToTransitTest + " permit 20\n" +
		" match ip address prefix-list " + iosxeRUPrefixListTest + "\n" +
		"route-map RM-RU-GEOROUTE-OUT permit 20\n" +
		" match ip address prefix-list " + iosxeRUPrefixListTest + "\n" +
		"route-map RM-RU-GEOROUTE-OUT deny 9999"
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

func TestRunTargetApplyModeRequiresSSHConfig(t *testing.T) {
	t.Parallel()
	flags := cliFlags{
		target:                targetIOSXE,
		targetMode:            targetModeApply,
		targetOutput:          targetOutputStdout,
		iosxePrefixList:       iosxeRUPrefixListTest,
		iosxeRouteMap:         iosxeRUDefaultRouteMapTest,
		iosxeApplyMaxPrefixes: 10,
		iosxeRouteMapSeq:      defaultRouteMapSeq,
		iosxeRouteMapDenySeq:  defaultDenySeq,
		iosxeStaticRouteMap:   iosxeRUStaticRouteMapTest,
		iosxeStaticRouteName:  iosxeRUStaticRouteNameTest,
	}
	logger := slog.New(slog.DiscardHandler)
	err := runTarget(context.Background(), flags, logger, []netip.Prefix{
		mustPrefix(t, "203.0.113.0/24"),
	})
	if err == nil {
		t.Fatal("runTarget() error = nil, want apply config error")
	}
	if !strings.Contains(err.Error(), "--iosxe-ssh-host is required") {
		t.Fatalf("runTarget() error = %q, want SSH config guard", err)
	}
}

func TestRunTargetApplyModePrefixLimit(t *testing.T) {
	t.Parallel()
	flags := cliFlags{
		target:                targetIOSXE,
		targetMode:            targetModeApply,
		targetOutput:          targetOutputStdout,
		iosxePrefixList:       iosxeRUPrefixListTest,
		iosxeRouteMap:         iosxeRUDefaultRouteMapTest,
		iosxeApplyMaxPrefixes: 1,
	}
	logger := slog.New(slog.DiscardHandler)
	err := runTarget(context.Background(), flags, logger, []netip.Prefix{
		mustPrefix(t, "203.0.113.0/24"),
		mustPrefix(t, "198.51.100.0/24"),
	})
	if err == nil {
		t.Fatal("runTarget() error = nil, want apply prefix limit error")
	}
	if !strings.Contains(err.Error(), "iosxe apply prefix limit exceeded") {
		t.Fatalf("runTarget() error = %q, want prefix limit guard", err)
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
