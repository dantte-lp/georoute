package main

import (
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Reusable test-prefix constants for goconst-friendly literals.
const (
	ddosA = "186.2.160.0/22"
	ddosB = "186.2.164.0/22"
	ddosC = "186.2.168.0/22"
	ddosD = "190.115.16.0/22"

	mergeA = "10.0.0.0/24"
	mergeB = "10.0.2.0/24"
)

func TestLoadExtras(t *testing.T) {
	t.Parallel()

	// Test-table struct: field-alignment optimization left informal since the
	// table is allocated once and freed at test exit. Reordering further would
	// hurt readability without measurable benefit.
	//nolint:govet // fieldalignment is informational for test-only struct
	cases := []struct {
		want    []string // expected prefixes (canonical form)
		wantErr error    // sentinel to match with errors.Is; nil = no error expected
		name    string
		content string // file contents; ignored when path overrides
		path    string // override path; empty = use temp file with content
		family  int
	}{
		{
			name:   "unset path returns nil without error",
			family: extrasFamilyV4,
			path:   "<unset>",
			want:   nil,
		},
		{
			name:    "empty file returns nil without error",
			family:  extrasFamilyV4,
			content: "",
			want:    nil,
		},
		{
			name:   "comments-only file returns nil without error",
			family: extrasFamilyV4,
			content: `# Ansible managed
# Sourced from inventory
`,
			want: nil,
		},
		{
			name:   "valid v4 prefixes parse and canonicalize",
			family: extrasFamilyV4,
			content: `# ddos-guard CDN ranges
186.2.160.0/22
186.2.164.0/22
186.2.168.0/22
190.115.16.0/22
`,
			want: []string{
				ddosA,
				ddosB,
				"186.2.168.0/22",
				"190.115.16.0/22",
			},
		},
		{
			name:   "inline comments stripped",
			family: extrasFamilyV4,
			content: `186.2.160.0/22  # ddos-guard A
186.2.164.0/22# adjacent
`,
			want: []string{ddosA, ddosB},
		},
		{
			name:   "leading whitespace tolerated",
			family: extrasFamilyV4,
			content: `   186.2.160.0/22
	186.2.164.0/22
`,
			want: []string{ddosA, ddosB},
		},
		{
			name:   "blank lines skipped",
			family: extrasFamilyV4,
			content: `

186.2.160.0/22

186.2.164.0/22

`,
			want: []string{ddosA, ddosB},
		},
		{
			name:    "duplicates preserved (downstream aggregates)",
			family:  extrasFamilyV4,
			content: "186.2.160.0/22\n186.2.160.0/22\n",
			want:    []string{ddosA, ddosA},
		},
		{
			name:   "non-canonical prefix is masked",
			family: extrasFamilyV4,
			// 10.0.0.1/24 should canonicalize to 10.0.0.0/24
			content: "10.0.0.1/24\n",
			want:    []string{"10.0.0.0/24"},
		},
		{
			name:    "invalid prefix surfaces sentinel and line number",
			family:  extrasFamilyV4,
			content: "186.2.160.0/22\nnot-a-prefix\n",
			wantErr: errExtrasInvalidPrefix,
		},
		{
			name:    "v6 in v4 file rejected",
			family:  extrasFamilyV4,
			content: "2001:db8::/32\n",
			wantErr: errExtrasFamilyMismatch,
		},
		{
			name:    "v4 in v6 file rejected",
			family:  extrasFamilyV6,
			content: "186.2.160.0/22\n",
			wantErr: errExtrasFamilyMismatch,
		},
		{
			name:    "valid v6 prefix accepted",
			family:  extrasFamilyV6,
			content: "2001:db8::/32\n",
			want:    []string{"2001:db8::/32"},
		},
		{
			name:    "unknown family yields family mismatch",
			family:  99,
			content: "186.2.160.0/22\n",
			wantErr: errExtrasFamilyMismatch,
		},
		{
			name:    "missing file errors out",
			family:  extrasFamilyV4,
			path:    "<missing>",
			wantErr: os.ErrNotExist,
		},
		{
			name:    "UTF-8 BOM at file start is stripped",
			family:  extrasFamilyV4,
			content: "\xef\xbb\xbf186.2.160.0/22\n",
			want:    []string{ddosA},
		},
		{
			name:    "CRLF line endings tolerated",
			family:  extrasFamilyV4,
			content: "186.2.160.0/22\r\n186.2.164.0/22\r\n",
			want:    []string{ddosA, ddosB},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			path := c.path
			switch path {
			case "<unset>":
				path = ""
			case "<missing>":
				// t.TempDir is cleaned up at test exit and isolated per
				// subtest, so this name is guaranteed absent under -parallel.
				path = filepath.Join(t.TempDir(), "missing-extras.list")
			case "":
				path = writeTempExtras(t, c.content)
			}

			got, err := loadExtras(path, c.family)

			if c.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error matching %v, got nil (out=%v)", c.wantErr, got)
				}
				if !errors.Is(err, c.wantErr) {
					t.Fatalf("expected error matching %v, got %v", c.wantErr, err)
				}

				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			gotStrs := make([]string, len(got))
			for i, p := range got {
				gotStrs[i] = p.String()
			}
			if !equalStringSlices(gotStrs, c.want) {
				t.Errorf("got %v, want %v", gotStrs, c.want)
			}
		})
	}
}

// TestLoadExtras_BoundsTooLarge verifies the per-file byte cap kicks in
// before we allocate per-prefix. We construct a file just over the limit
// to ensure the boundary is rejected, then a file at the limit to ensure
// it's accepted.
func TestLoadExtras_BoundsTooLarge(t *testing.T) {
	t.Parallel()

	// Just over the cap.
	tooBig := writeTempExtras(t, strings.Repeat("# pad\n", (extrasMaxFileBytes/6)+1))
	_, err := loadExtras(tooBig, extrasFamilyV4)
	if !errors.Is(err, errExtrasTooLarge) {
		t.Fatalf("expected errExtrasTooLarge, got %v", err)
	}

	// At/below the cap with a single valid prefix → succeeds.
	padded := strings.Repeat("# pad\n", 1000) + "186.2.160.0/22\n"
	okFile := writeTempExtras(t, padded)
	got, err := loadExtras(okFile, extrasFamilyV4)
	if err != nil {
		t.Fatalf("padded valid file: unexpected error %v", err)
	}
	if len(got) != 1 || got[0].String() != ddosA {
		t.Errorf("padded valid file: got %v, want [%s]", got, ddosA)
	}
}

// TestLoadExtras_BoundsLineTooLong verifies the per-line byte cap fires
// with a line-numbered error (no bare "token too long"). The line is
// constructed from a comment string of cap+1 bytes so it's syntactically
// reasonable input that just happens to exceed the buffer.
func TestLoadExtras_BoundsLineTooLong(t *testing.T) {
	t.Parallel()

	longLine := "# " + strings.Repeat("x", extrasMaxLineBytes) + "\n"
	path := writeTempExtras(t, longLine)
	_, err := loadExtras(path, extrasFamilyV4)
	if !errors.Is(err, errExtrasLineTooLong) {
		t.Fatalf("expected errExtrasLineTooLong, got %v", err)
	}
}

// TestAggregate_AbsorbsExtras verifies that loadExtras output flows into
// aggregate the same way RIPE-fed prefixes do: append + aggregate. The
// call site at run() in main.go relies on this composition. We don't drive
// run() itself here because that would require stubbing the RIPE HTTP
// fetch; the test covers the merge semantics that the wiring depends on.
func TestAggregate_AbsorbsExtras(t *testing.T) {
	t.Parallel()

	// RIPE feed has 10.0.0.0/24 and 10.0.1.0/24 — these merge to 10.0.0.0/23.
	// Operator extras add 10.0.0.0/24 (duplicate) plus 10.0.2.0/24 — the final
	// aggregate should contain just 10.0.0.0/23 + 10.0.2.0/24.
	ripe := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/24"),
		netip.MustParsePrefix("10.0.1.0/24"),
	}
	extrasFile := writeTempExtras(t, "10.0.0.0/24\n10.0.2.0/24\n")

	extras, err := loadExtras(extrasFile, extrasFamilyV4)
	if err != nil {
		t.Fatalf("loadExtras: %v", err)
	}

	combined := append([]netip.Prefix{}, ripe...)
	combined = append(combined, extras...)
	agg := aggregate(combined)

	got := make([]string, len(agg))
	for i, p := range agg {
		got[i] = p.String()
	}
	want := []string{"10.0.0.0/23", mergeB}
	if !equalStringSlices(got, want) {
		t.Errorf("aggregate(combined) = %v, want %v", got, want)
	}
}

// writeTempExtras creates a temp file with content and registers cleanup.
// Returns the path. Failures abort the test via t.Fatalf — caller does not
// need to check.
func writeTempExtras(t *testing.T, content string) string {
	t.Helper()

	f, err := os.CreateTemp(t.TempDir(), "extras-*.list")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	defer func() { _ = f.Close() }()

	_, err = f.WriteString(content)
	if err != nil {
		t.Fatalf("write temp: %v", err)
	}

	return f.Name()
}

func equalStringSlices(a, b []string) bool {
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
