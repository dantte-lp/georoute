package main

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"os"
	"strings"
)

// Family-specific error values let callers errors.Is on parser failures
// without parsing the string payload.
var (
	errExtrasFamilyMismatch = errors.New("extras prefix family does not match flag")
	errExtrasInvalidPrefix  = errors.New("extras prefix invalid")
	errExtrasTooLarge       = errors.New("extras file exceeds maximum size")
	errExtrasLineTooLong    = errors.New("extras line exceeds maximum length")
)

// extrasFamilyV4 and extrasFamilyV6 select which address family the parser
// expects. Caller-side typed constants avoid the bool-blindness of a flag.
const (
	extrasFamilyV4 = 4
	extrasFamilyV6 = 6

	// extrasMaxFileBytes caps the on-disk size of an extras list to bound
	// memory regardless of operator misconfiguration (huge file fed by
	// mistake). 1 MiB holds ~30 000 prefixes — well above any realistic
	// operator-maintained extension list.
	extrasMaxFileBytes = 1 << 20

	// extrasMaxLineBytes caps individual line length. Real prefix lines fit
	// in ~50 bytes; this leaves headroom for inline comments while
	// surfacing pathological input early instead of OOM'ing the scanner.
	extrasMaxLineBytes = 4096

	// utf8BOM is the byte-order mark some editors prepend to UTF-8 files.
	// Stripping it on the first scanned line avoids a confusing
	// netip.ParsePrefix error on what looks like a valid prefix.
	// Escaped because Go source files reject a literal BOM mid-file.
	utf8BOM = "\xef\xbb\xbf"
)

// loadExtras reads a plain-text prefix list and returns the validated
// netip.Prefix values for the requested family.
//
// File format: one prefix per line. `#` starts a comment that runs to the
// end of the line. Blank / whitespace-only lines are skipped. Each prefix
// is parsed with netip.ParsePrefix and canonicalized with .Masked().
//
// Semantics:
//   - path == ""                    → nil, nil  (flag unset, no extras).
//   - file present, no prefixes     → nil, nil  (comments / empty file).
//   - file missing                  → error (operator misconfig).
//   - prefix wrong family for flag  → error (rejects v6 entries in --extras-v4-file).
//   - parse error on any line       → error tagged with the line number.
//
// The returned slice carries duplicates as-is — the merge step downstream
// (parsePrefixes → aggregate) deduplicates and sorts canonically.
func loadExtras(path string, family int) ([]netip.Prefix, error) {
	if path == "" {
		return nil, nil
	}

	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("extras file %s: %w", path, err)
		}

		return nil, fmt.Errorf("open extras %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	// Bound on-disk size before reading. Stat returns -1 for streams; we
	// only reject when size is known and exceeds the limit (regular files).
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat extras %s: %w", path, err)
	}
	if info.Mode().IsRegular() && info.Size() > extrasMaxFileBytes {
		return nil, fmt.Errorf("%w: %s: %d > %d bytes",
			errExtrasTooLarge, path, info.Size(), extrasMaxFileBytes)
	}

	var out []netip.Prefix
	scanner := bufio.NewScanner(f)
	// Default bufio.Scanner caps lines at 64 KiB and surfaces a bare
	// "token too long" without line context. Explicit buffer keeps the cap
	// predictable and lets us return a file-line-aware error below.
	scanner.Buffer(make([]byte, 0, 4096), extrasMaxLineBytes)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		raw := scanner.Text()
		// First line may carry a UTF-8 BOM; strip it so the prefix parser
		// sees a clean address. Subsequent lines never start with a BOM in
		// well-formed UTF-8.
		if lineNo == 1 {
			raw = strings.TrimPrefix(raw, utf8BOM)
		}

		// Strip inline comments: everything after the first `#` is comment.
		if i := strings.IndexByte(raw, '#'); i >= 0 {
			raw = raw[:i]
		}
		clean := strings.TrimSpace(raw)
		if clean == "" {
			continue
		}

		prefix, parseErr := netip.ParsePrefix(clean)
		if parseErr != nil {
			return nil, fmt.Errorf("%w: %s:%d: %q: %w",
				errExtrasInvalidPrefix, path, lineNo, clean, parseErr)
		}

		if !matchesFamily(prefix, family) {
			return nil, fmt.Errorf("%w: %s:%d: %q: want v%d",
				errExtrasFamilyMismatch, path, lineNo, clean, family)
		}

		out = append(out, prefix.Masked())
	}
	err = scanner.Err()
	if err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return nil, fmt.Errorf("%w: %s:%d (limit %d bytes)",
				errExtrasLineTooLong, path, lineNo+1, extrasMaxLineBytes)
		}

		return nil, fmt.Errorf("scan extras %s: %w", path, err)
	}

	return out, nil
}

// matchesFamily reports whether prefix is in the requested address family.
// extras files are family-specific (operators maintain a v4 list and a v6
// list separately) so mixing families is a misconfiguration to surface
// loudly rather than silently routing the wrong family.
func matchesFamily(p netip.Prefix, family int) bool {
	switch family {
	case extrasFamilyV4:
		return p.Addr().Is4()
	case extrasFamilyV6:
		return p.Addr().Is6() && !p.Addr().Is4In6()
	default:
		return false
	}
}
