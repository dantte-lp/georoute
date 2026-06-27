package main

import (
	"net/netip"
	"strings"
	"testing"
	"time"
)

// Marker constants used across splice test cases; goconst-friendly.
const (
	beginMarkerTest = "  ! BEGIN-X"
	endMarkerTest   = "  ! END-X"

	prefix10v24  = "10.0.0.0/24"
	prefix10base = "10.0.0.0"
)

func TestApplyDefaults(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		country string
		want    cliFlags
	}{
		{
			name:    "RU defaults match legacy names",
			country: "RU",
			want: cliFlags{
				country:          "RU",
				routeMap:         "MARK-RU-EXIT",
				nftSetV4:         "ru_v4",
				nftSetV6:         "ru_v6",
				markerPrefix:     "RU-FEED",
				feedURL:          "https://stat.ripe.net/data/country-resource-list/data.json?resource=RU&v4_format=prefix",
				lockFile:         "/run/georoute-ru.lock",
				cacheFile:        "/var/lib/georoute/feed-ru.json.gz",
				cacheMaxAge:      7 * 24 * time.Hour,
				lastSuccessFile:  "/var/lib/georoute/last-success-ru",
				readyMaxAge:      24 * time.Hour,
				logFormat:        logFormatText,
				logLevel:         defaultLogLevel,
				httpTimeout:      60 * time.Second,
				frrReloadTimeout: 3 * time.Minute,
				nftTimeout:       30 * time.Second,
				retryAttempts:    3,
				retryBaseDelay:   2 * time.Second,
				frrReloadScript:  defaultFrrReloadScript,
				nftBinary:        defaultNftBinary,
			},
		},
		{
			name:    "UZ derives uz_v4 etc.",
			country: "UZ",
			want: cliFlags{
				country:          "UZ",
				routeMap:         "MARK-UZ-EXIT",
				nftSetV4:         "uz_v4",
				nftSetV6:         "uz_v6",
				markerPrefix:     "UZ-FEED",
				feedURL:          "https://stat.ripe.net/data/country-resource-list/data.json?resource=UZ&v4_format=prefix",
				lockFile:         "/run/georoute-uz.lock",
				cacheFile:        "/var/lib/georoute/feed-uz.json.gz",
				cacheMaxAge:      7 * 24 * time.Hour,
				lastSuccessFile:  "/var/lib/georoute/last-success-uz",
				readyMaxAge:      24 * time.Hour,
				logFormat:        logFormatText,
				logLevel:         defaultLogLevel,
				httpTimeout:      60 * time.Second,
				frrReloadTimeout: 3 * time.Minute,
				nftTimeout:       30 * time.Second,
				retryAttempts:    3,
				retryBaseDelay:   2 * time.Second,
				frrReloadScript:  defaultFrrReloadScript,
				nftBinary:        defaultNftBinary,
			},
		},
		{
			name:    "lowercase input normalized",
			country: "kz",
			want: cliFlags{
				country:          "kz",
				routeMap:         "MARK-KZ-EXIT",
				nftSetV4:         "kz_v4",
				nftSetV6:         "kz_v6",
				markerPrefix:     "KZ-FEED",
				feedURL:          "https://stat.ripe.net/data/country-resource-list/data.json?resource=KZ&v4_format=prefix",
				lockFile:         "/run/georoute-kz.lock",
				cacheFile:        "/var/lib/georoute/feed-kz.json.gz",
				cacheMaxAge:      7 * 24 * time.Hour,
				lastSuccessFile:  "/var/lib/georoute/last-success-kz",
				readyMaxAge:      24 * time.Hour,
				logFormat:        logFormatText,
				logLevel:         defaultLogLevel,
				httpTimeout:      60 * time.Second,
				frrReloadTimeout: 3 * time.Minute,
				nftTimeout:       30 * time.Second,
				retryAttempts:    3,
				retryBaseDelay:   2 * time.Second,
				frrReloadScript:  defaultFrrReloadScript,
				nftBinary:        defaultNftBinary,
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f := cliFlags{country: c.country}
			f.applyDefaults()
			if f != c.want {
				t.Errorf("got %+v, want %+v", f, c.want)
			}
		})
	}
}

func TestMarkers(t *testing.T) {
	t.Parallel()
	f := cliFlags{country: "UZ"}
	f.applyDefaults()
	bV4, eV4, bV6, eV6 := f.markers()
	if bV4 != "  ! BEGIN-UZ-FEED-V4" {
		t.Errorf("beginV4 = %q", bV4)
	}
	if eV4 != "  ! END-UZ-FEED-V4" {
		t.Errorf("endV4 = %q", eV4)
	}
	if bV6 != "  ! BEGIN-UZ-FEED-V6" {
		t.Errorf("beginV6 = %q", bV6)
	}
	if eV6 != "  ! END-UZ-FEED-V6" {
		t.Errorf("endV6 = %q", eV6)
	}
}

func TestSplice(t *testing.T) {
	t.Parallel()
	cases := []struct { //nolint:govet // table-test field order optimized for reader, not packer
		name      string
		cfg       string
		begin     string
		end       string
		body      string
		want      string
		wantErrIs error
	}{
		{
			name:  "empty block insert",
			cfg:   beginMarkerTest + "\n" + endMarkerTest + "\n",
			begin: beginMarkerTest,
			end:   endMarkerTest,
			body:  "  network 1.0.0.0/8 route-map M",
			want:  beginMarkerTest + "\n  network 1.0.0.0/8 route-map M\n" + endMarkerTest + "\n",
		},
		{
			name:  "replace existing block",
			cfg:   beginMarkerTest + "\n  network 2.0.0.0/8 route-map M\n" + endMarkerTest + "\n",
			begin: beginMarkerTest,
			end:   endMarkerTest,
			body:  "  network 1.0.0.0/8 route-map M",
			want:  beginMarkerTest + "\n  network 1.0.0.0/8 route-map M\n" + endMarkerTest + "\n",
		},
		{
			name:  "empty body clears block",
			cfg:   beginMarkerTest + "\n  network 2.0.0.0/8 route-map M\n" + endMarkerTest + "\n",
			begin: beginMarkerTest,
			end:   endMarkerTest,
			body:  "",
			want:  beginMarkerTest + "\n" + endMarkerTest + "\n",
		},
		{
			name:      "missing begin marker",
			cfg:       endMarkerTest + "\n",
			begin:     beginMarkerTest,
			end:       endMarkerTest,
			body:      "",
			wantErrIs: errBeginMissing,
		},
		{
			name:      "missing end marker",
			cfg:       beginMarkerTest + "\n",
			begin:     beginMarkerTest,
			end:       endMarkerTest,
			body:      "",
			wantErrIs: errEndMissing,
		},
		{
			name:      "end before begin",
			cfg:       endMarkerTest + "\n" + beginMarkerTest + "\n",
			begin:     beginMarkerTest,
			end:       endMarkerTest,
			body:      "",
			wantErrIs: errEndMissing,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, err := splice(c.cfg, c.begin, c.end, c.body)
			if c.wantErrIs != nil {
				if err == nil || !errorMatches(err, c.wantErrIs) {
					t.Fatalf("want err %v, got %v", c.wantErrIs, err)
				}

				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("got %q\nwant %q", got, c.want)
			}
		})
	}
}

func errorMatches(err, target error) bool {
	if err == nil {
		return target == nil
	}

	return strings.Contains(err.Error(), target.Error())
}

func TestAggregate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "empty",
			in:   nil,
			want: nil,
		},
		{
			name: "single",
			in:   []string{prefix10v24},
			want: []string{prefix10v24},
		},
		{
			name: "two adjacent merge to one",
			in:   []string{"10.0.0.0/25", "10.0.0.128/25"},
			want: []string{prefix10v24},
		},
		{
			name: "overlap dedup",
			in:   []string{prefix10v24, "10.0.0.0/26"},
			want: []string{prefix10v24},
		},
		{
			name: "non-adjacent stays separate",
			in:   []string{prefix10v24, "10.0.2.0/24"},
			want: []string{prefix10v24, "10.0.2.0/24"},
		},
		{
			name: "chain merge — four /26 → one /24",
			in:   []string{"10.0.0.0/26", "10.0.0.64/26", "10.0.0.128/26", "10.0.0.192/26"},
			want: []string{prefix10v24},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			in := parsePrefixes(c.in)
			got := aggregate(in)
			gotStr := prefixStrs(got)
			if !sliceEq(gotStr, c.want) {
				t.Errorf("got %v want %v", gotStr, c.want)
			}
		})
	}
}

func TestRangeToCIDR(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		start string
		end   string
		want  []string
	}{
		{
			name:  "single address",
			start: "1.2.3.4",
			end:   "1.2.3.4",
			want:  []string{"1.2.3.4/32"},
		},
		{
			name:  "perfect /24",
			start: prefix10base,
			end:   "10.0.0.255",
			want:  []string{prefix10v24},
		},
		{
			name:  "ragged range needs multiple prefixes",
			start: prefix10base,
			end:   "10.0.1.10",
			want:  []string{prefix10v24, "10.0.1.0/29", "10.0.1.8/31", "10.0.1.10/32"},
		},
		{
			name:  "reversed range yields nothing",
			start: "10.0.0.255",
			end:   prefix10base,
			want:  nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			start, _ := netip.ParseAddr(c.start)
			end, _ := netip.ParseAddr(c.end)
			got := rangeToCIDR(start, end)
			gotStr := prefixStrs(got)
			if !sliceEq(gotStr, c.want) {
				t.Errorf("got %v want %v", gotStr, c.want)
			}
		})
	}
}

func TestRenderNetworks(t *testing.T) {
	t.Parallel()
	in := []netip.Prefix{netip.MustParsePrefix("1.0.0.0/8"), netip.MustParsePrefix("2a02:6b8::/29")}
	got := renderNetworks(in, "MARK-UZ-EXIT")
	want := "  network 1.0.0.0/8 route-map MARK-UZ-EXIT\n  network 2a02:6b8::/29 route-map MARK-UZ-EXIT"
	if got != want {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
	if empty := renderNetworks(nil, "X"); empty != "" {
		t.Errorf("empty input must yield empty string, got %q", empty)
	}
}

func prefixStrs(ps []netip.Prefix) []string {
	if len(ps) == 0 {
		return nil
	}
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.String()
	}

	return out
}

func sliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}
