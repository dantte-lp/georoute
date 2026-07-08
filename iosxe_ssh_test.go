package main

import (
	"strings"
	"testing"
)

func TestBuildIOSXEApplyScript(t *testing.T) {
	t.Parallel()
	flags := cliFlags{
		iosxePrefixList:      iosxeRUPrefixListTest,
		iosxeRouteMap:        iosxeRUOutRouteMapTest,
		iosxeStaticRouteMap:  iosxeRUStaticRouteMapTest,
		iosxeStaticRouteName: iosxeRUStaticRouteNameTest,
		iosxeBGPAS:           "4200099722",
	}
	candidate := "ip prefix-list " + iosxeRUPrefixListTest + " seq 10 permit 203.0.113.0/24\n" +
		"ip route 203.0.113.0 255.255.255.0 192.0.2.1 name " + iosxeRUStaticRouteNameTest + "\n" +
		"route-map " + iosxeRUStaticRouteMapTest + " permit 10\n" +
		" match ip address prefix-list " + iosxeRUPrefixListTest
	got := buildIOSXEApplyScript(flags, candidate)
	required := []string{
		"terminal length 0\nconfigure terminal\n",
		"ip route 203.0.113.0 255.255.255.0 192.0.2.1 name " + iosxeRUStaticRouteNameTest,
		"\nend\nshow ip prefix-list " + iosxeRUPrefixListTest,
		"\nshow route-map " + iosxeRUOutRouteMapTest,
		"\nshow route-map " + iosxeRUStaticRouteMapTest,
		"\nshow running-config | section ^router bgp\nexit\n",
	}
	for _, marker := range required {
		if !strings.Contains(got, marker) {
			t.Fatalf("buildIOSXEApplyScript() missing %q in %q", marker, got)
		}
	}
}

func TestVerifyIOSXEApplyOutput(t *testing.T) {
	t.Parallel()
	flags := cliFlags{
		iosxePrefixList:     iosxeRUPrefixListTest,
		iosxeRouteMap:       iosxeRUOutRouteMapTest,
		iosxeStaticRouteMap: iosxeRUStaticRouteMapTest,
		iosxeBGPAS:          "4200099722",
	}
	out := "show ip prefix-list " + iosxeRUPrefixListTest + "\n" +
		"show route-map " + iosxeRUOutRouteMapTest + "\n" +
		"show route-map " + iosxeRUStaticRouteMapTest + "\n" +
		"redistribute static route-map " + iosxeRUStaticRouteMapTest
	err := verifyIOSXEApplyOutput(flags, out)
	if err != nil {
		t.Fatalf("verifyIOSXEApplyOutput() error = %v", err)
	}
}

func TestVerifyIOSXEApplyOutputRejectsInvalidCommand(t *testing.T) {
	t.Parallel()
	flags := cliFlags{
		iosxePrefixList: iosxeRUPrefixListTest,
		iosxeRouteMap:   iosxeRUOutRouteMapTest,
	}
	err := verifyIOSXEApplyOutput(flags, "% Invalid input detected")
	if err == nil {
		t.Fatal("verifyIOSXEApplyOutput() error = nil, want invalid-command error")
	}
	if !strings.Contains(err.Error(), "% Invalid") {
		t.Fatalf("verifyIOSXEApplyOutput() error = %q, want invalid-command marker", err)
	}
}
