// Command georoute fetches a country prefix list from RIPE Stat, aggregates
// it, splices BGP `network` statements into the FRR config between marker
// comments, and runs frr-reload only when the resulting block changes.
//
// The tool is country-agnostic: pass --country UZ (or KZ, GE, …) and it
// derives the set names, marker comments, route-map name and feed URL.
// Defaults stay RU-shaped for backward compatibility with existing deploys.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const (
	httpTimeout      = 60 * time.Second
	frrReloadTimeout = 3 * time.Minute
	nftTimeout       = 30 * time.Second
	sampleLines      = 5
	maxBodyBytes     = 32 << 20 // cap RIPE Stat JSON at 32 MiB
	retryAttempts    = 3
	retryBaseDelay   = 2 * time.Second
	configFileMode   = 0o640
	limitErrorBody   = 4096
	frrReloadScript  = "/usr/lib/frr/frr-reload.py"
	nftBinary        = "/usr/sbin/nft"
	nftTable         = "inet pbr"
	envPATH          = "PATH=/usr/sbin:/usr/bin:/sbin:/bin"
	feedURLTemplate  = "https://stat.ripe.net/data/country-resource-list/data.json?resource=%s&v4_format=prefix"
)

// version is overwritten at link time via `-X main.version=…` in the Makefile
// or the release workflow. The literal "dev" is the fallback for unstamped
// builds (e.g. `go run .`).
var version = "dev"

// Static error values let callers errors.Is them and satisfy err113.
var (
	errBadStatus    = errors.New("RIPE Stat status not ok")
	errHTTP         = errors.New("RIPE Stat HTTP error")
	errBeginMissing = errors.New("begin marker not found in FRR config")
	errEndMissing   = errors.New("end marker not found or misplaced in FRR config")
	errLocked       = errors.New("another georoute run holds the lock")
)

type ripeResources struct {
	IPv4 []string `json:"ipv4"`
	IPv6 []string `json:"ipv6"`
}

type ripeData struct {
	Resources ripeResources `json:"resources"`
}

type ripeResp struct {
	Status string   `json:"status"`
	Data   ripeData `json:"data"`
}

func main() {
	os.Exit(realMain())
}

type cliFlags struct {
	// strings first (16 B each on 64-bit) for field-alignment, bools last
	frrConf      string
	lockFile     string
	country      string
	routeMap     string
	nftSetV4     string
	nftSetV6     string
	markerPrefix string
	feedURL      string
	extrasV4File string
	extrasV6File string
	reloadOK     bool
	dryRun       bool
	force        bool
	updateNft    bool
}

// applyDefaults fills the country-dependent fields from `country` when the
// operator didn't override them on the CLI. country=RU yields the legacy
// names (MARK-RU-EXIT, ru_v4, ru_v6, BEGIN-RU-FEED-V4, …) so existing
// deploys keep working unchanged.
func (f *cliFlags) applyDefaults() {
	cc := strings.ToUpper(f.country)
	ccLower := strings.ToLower(f.country)
	if f.routeMap == "" {
		f.routeMap = "MARK-" + cc + "-EXIT"
	}
	if f.nftSetV4 == "" {
		f.nftSetV4 = ccLower + "_v4"
	}
	if f.nftSetV6 == "" {
		f.nftSetV6 = ccLower + "_v6"
	}
	if f.markerPrefix == "" {
		f.markerPrefix = cc + "-FEED"
	}
	if f.feedURL == "" {
		f.feedURL = fmt.Sprintf(feedURLTemplate, cc)
	}
	if f.lockFile == "" {
		f.lockFile = "/run/georoute-" + ccLower + ".lock"
	}
}

// markers returns the four marker comment lines used to delimit per-country
// blocks inside frr.conf — order: beginV4, endV4, beginV6, endV6. They
// live inside an `address-family X unicast` block, so they MUST be
// indented two spaces.
func (f *cliFlags) markers() (string, string, string, string) {
	return "  ! BEGIN-" + f.markerPrefix + "-V4",
		"  ! END-" + f.markerPrefix + "-V4",
		"  ! BEGIN-" + f.markerPrefix + "-V6",
		"  ! END-" + f.markerPrefix + "-V6"
}

func realMain() int {
	var showVersion bool
	flag.BoolVar(&showVersion, "version", false, "print version and exit")

	flags := cliFlags{}
	flag.StringVar(&flags.frrConf, "frr-conf", "/etc/frr/frr.conf", "path to FRR config")
	flag.BoolVar(&flags.reloadOK, "reload", true, "run frr-reload on change")
	flag.BoolVar(&flags.dryRun, "dry-run", false, "print summary without writing")
	flag.BoolVar(&flags.force, "force", false, "force write even if unchanged")
	flag.BoolVar(&flags.updateNft, "nft", true, "atomically replace nft set inet pbr {<cc>_v4,<cc>_v6}")
	flag.StringVar(&flags.lockFile, "lock-file", "", "exclusive flock path (default /run/georoute-<cc>.lock)")
	flag.StringVar(&flags.country, "country", "RU", "ISO-3166 alpha-2 country code (RU, UZ, KZ, …)")
	flag.StringVar(&flags.routeMap, "route-map", "", "FRR route-map name (default MARK-<CC>-EXIT)")
	flag.StringVar(&flags.nftSetV4, "nft-set-v4", "", "nftables v4 set name (default <cc>_v4)")
	flag.StringVar(&flags.nftSetV6, "nft-set-v6", "", "nftables v6 set name (default <cc>_v6)")
	flag.StringVar(&flags.markerPrefix, "marker-prefix", "", "marker comment prefix between BEGIN-/END- and -V4/-V6 (default <CC>-FEED)")
	flag.StringVar(&flags.feedURL, "feed-url", "", "RIPE Stat URL (default country-resource-list for <cc>)")
	flag.StringVar(&flags.extrasV4File, "extras-v4-file", "", "path to operator-maintained IPv4 prefix list merged with RIPE feed (one prefix per line, # comments; empty = no extras)")
	flag.StringVar(&flags.extrasV6File, "extras-v6-file", "", "path to operator-maintained IPv6 prefix list merged with RIPE feed (one prefix per line, # comments; empty = no extras)")
	flag.Parse()

	if showVersion {
		_, _ = fmt.Fprintln(os.Stdout, version)

		return 0
	}

	flags.applyDefaults()

	log.SetFlags(log.LstdFlags | log.LUTC)
	log.SetPrefix(fmt.Sprintf("georoute[%s] ", strings.ToUpper(flags.country)))

	// Generous outer budget: 60s fetch (with retries) + 30s nft + 3min reload.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Exclusive lock prevents two timer cycles (or timer + manual) from
	// racing on frr.conf.new or frr-reload.py.
	lockF, err := acquireLock(flags.lockFile)
	if err != nil {
		log.Printf("lock: %v", err)

		return 1
	}
	defer func() {
		_ = lockF.Close() // closing releases the flock
	}()

	err = run(ctx, flags)
	if err != nil {
		log.Printf("error: %v", err)

		return 1
	}

	return 0
}

// acquireLock takes an exclusive (LOCK_EX | LOCK_NB) flock on path. Releases
// when the returned *os.File is closed.
func acquireLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("open lock %s: %w", path, err)
	}
	err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err != nil {
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, fmt.Errorf("%w: %s", errLocked, path)
		}

		return nil, fmt.Errorf("flock: %w", err)
	}

	return f, nil
}

func run(ctx context.Context, f cliFlags) error {
	log.Printf("fetching RIPE Stat resources for %s", strings.ToUpper(f.country))

	raw, err := fetchWithRetry(ctx, f.feedURL)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	log.Printf("raw: %d v4 prefixes, %d v6 prefixes",
		len(raw.Data.Resources.IPv4), len(raw.Data.Resources.IPv6))

	// Extras are operator-maintained prefix lists (e.g. non-RIPE-country
	// CDN ranges). They're merged before aggregate so dedup / minimal-cover
	// applies uniformly to the union of RIPE + extras.
	extrasV4, err := loadExtras(f.extrasV4File, extrasFamilyV4)
	if err != nil {
		return fmt.Errorf("load extras v4: %w", err)
	}
	extrasV6, err := loadExtras(f.extrasV6File, extrasFamilyV6)
	if err != nil {
		return fmt.Errorf("load extras v6: %w", err)
	}
	if f.extrasV4File != "" {
		log.Printf("extras: loaded %d v4 prefixes from %s", len(extrasV4), f.extrasV4File)
	}
	if f.extrasV6File != "" {
		log.Printf("extras: loaded %d v6 prefixes from %s", len(extrasV6), f.extrasV6File)
	}

	v4Parsed := append(parsePrefixes(raw.Data.Resources.IPv4), extrasV4...)
	v6Parsed := append(parsePrefixes(raw.Data.Resources.IPv6), extrasV6...)
	v4Agg := aggregate(v4Parsed)
	v6Agg := aggregate(v6Parsed)
	log.Printf("aggregated: %d v4, %d v6", len(v4Agg), len(v6Agg))

	v4Block := renderNetworks(v4Agg, f.routeMap)
	v6Block := renderNetworks(v6Agg, f.routeMap)
	log.Printf("v4 hash=%s v6 hash=%s", hashOf(v4Block)[:12], hashOf(v6Block)[:12])

	if f.dryRun {
		printSample("v4-bgp", v4Block)
		printSample("v6-bgp", v6Block)
		log.Printf("nft %s set would have %d elements; %s set %d",
			f.nftSetV4, len(v4Agg), f.nftSetV6, len(v6Agg))

		return nil
	}

	// nft FIRST: update data plane before advertising via BGP. If a new
	// prefix reaches a sibling RIB before the local nft mark, the sibling
	// would loop it back over our transit — updating nft first avoids that
	// window.
	if f.updateNft {
		err = applyNft(ctx, f, v4Agg, v6Agg)
		if err != nil {
			return fmt.Errorf("apply nft: %w", err)
		}
	}

	cur, err := os.ReadFile(f.frrConf)
	if err != nil {
		return fmt.Errorf("read frr.conf: %w", err)
	}

	beginV4, endV4, beginV6, endV6 := f.markers()
	next, err := splice(string(cur), beginV4, endV4, v4Block)
	if err != nil {
		return fmt.Errorf("splice v4: %w", err)
	}
	next, err = splice(next, beginV6, endV6, v6Block)
	if err != nil {
		return fmt.Errorf("splice v6: %w", err)
	}

	if !f.force && string(cur) == next {
		log.Printf("frr.conf unchanged — skipping reload")

		return nil
	}

	err = atomicWrite(f.frrConf, []byte(next))
	if err != nil {
		return fmt.Errorf("write frr.conf: %w", err)
	}
	log.Printf("frr.conf updated")

	if !f.reloadOK {
		return nil
	}

	err = reloadFRR(ctx, f.frrConf)
	if err != nil {
		return fmt.Errorf("frr-reload: %w", err)
	}
	log.Printf("frr-reload completed")

	return nil
}

// applyNft atomically replaces the contents of inet pbr {<cc>_v4, <cc>_v6}.
// Per nftables(8), all commands in a single `nft -f` invocation are applied
// as one atomic netlink transaction.
func applyNft(ctx context.Context, f cliFlags, v4, v6 []netip.Prefix) error {
	var script strings.Builder
	_, _ = fmt.Fprintf(&script, "flush set %s %s\n", nftTable, f.nftSetV4)
	if len(v4) > 0 {
		_, _ = fmt.Fprintf(&script, "add element %s %s { %s }\n", nftTable, f.nftSetV4, joinPrefixes(v4))
	}
	_, _ = fmt.Fprintf(&script, "flush set %s %s\n", nftTable, f.nftSetV6)
	if len(v6) > 0 {
		_, _ = fmt.Fprintf(&script, "add element %s %s { %s }\n", nftTable, f.nftSetV6, joinPrefixes(v6))
	}

	nftCtx, cancel := context.WithTimeout(ctx, nftTimeout)
	defer cancel()

	cmd := exec.CommandContext(nftCtx, nftBinary, "-f", "-")
	cmd.Stdin = strings.NewReader(script.String())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = []string{envPATH}
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("nft -f: %w", err)
	}
	log.Printf("nft sets updated (%s=%d %s=%d)", f.nftSetV4, len(v4), f.nftSetV6, len(v6))

	return nil
}

func joinPrefixes(ps []netip.Prefix) string {
	parts := make([]string, len(ps))
	for i, p := range ps {
		parts[i] = p.String()
	}

	return strings.Join(parts, ", ")
}

// fetchWithRetry calls fetch with up to retryAttempts attempts, with
// exponential backoff between them. RIPE Stat occasionally 503/429s under
// load; one failure on a 12h timer means 12h stale state, so retry is
// cheap insurance.
func fetchWithRetry(ctx context.Context, url string) (*ripeResp, error) {
	var lastErr error
	for attempt := 1; attempt <= retryAttempts; attempt++ {
		if attempt > 1 {
			backoff := time.Duration(attempt-1) * retryBaseDelay
			log.Printf("fetch attempt %d/%d after %s backoff (last error: %v)", attempt, retryAttempts, backoff, lastErr)
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("retry wait: %w", ctx.Err())
			case <-time.After(backoff):
			}
		}
		resp, err := fetch(ctx, url)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}

	return nil, fmt.Errorf("retries exhausted: %w", lastErr)
}

func fetch(ctx context.Context, url string) (*ripeResp, error) {
	client := &http.Client{Timeout: httpTimeout}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("User-Agent", "georoute/2.0 (+https://github.com/dantte-lp/georoute)")

	resp, err := client.Do(req) //nolint:bodyclose // drainAndClose handles it
	if err != nil {
		return nil, fmt.Errorf("http do: %w", err)
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, limitErrorBody))

		return nil, fmt.Errorf("%w: status=%d body=%s", errHTTP, resp.StatusCode, string(body))
	}

	var parsed ripeResp
	err = json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(&parsed)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if parsed.Status != "ok" {
		return nil, fmt.Errorf("%w: %s", errBadStatus, parsed.Status)
	}

	return &parsed, nil
}

func drainAndClose(rc io.ReadCloser) {
	_, _ = io.Copy(io.Discard, rc)
	_ = rc.Close()
}

func parsePrefixes(raw []string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(raw))
	for _, s := range raw {
		clean := strings.TrimSpace(s)
		if clean == "" {
			continue
		}
		if strings.Contains(clean, "-") {
			out = append(out, parseRange(clean)...)

			continue
		}

		prefix, err := netip.ParsePrefix(clean)
		if err != nil {
			log.Printf("warn: skipping bad prefix %q: %v", clean, err)

			continue
		}
		out = append(out, prefix.Masked())
	}

	return out
}

func parseRange(s string) []netip.Prefix {
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return nil
	}

	start, errA := netip.ParseAddr(strings.TrimSpace(parts[0]))
	end, errB := netip.ParseAddr(strings.TrimSpace(parts[1]))
	if errA != nil || errB != nil {
		return nil
	}

	return rangeToCIDR(start, end)
}

// rangeToCIDR converts an inclusive address range to the minimal set of CIDR prefixes.
func rangeToCIDR(start, end netip.Addr) []netip.Prefix {
	if start.Is4() != end.Is4() || start.Compare(end) > 0 {
		return nil
	}

	bits := 32
	if start.Is6() {
		bits = 128
	}

	var out []netip.Prefix
	for start.Compare(end) <= 0 {
		mask := bits
		for mask > 0 {
			tryMask := mask - 1
			prefix := netip.PrefixFrom(start, tryMask).Masked()
			if prefix.Addr() != start {
				break
			}
			last := lastAddr(prefix)
			if last.Compare(end) > 0 {
				break
			}
			mask = tryMask
		}
		prefix := netip.PrefixFrom(start, mask).Masked()
		out = append(out, prefix)
		nextStart := lastAddr(prefix).Next()
		if !nextStart.IsValid() {
			break
		}
		start = nextStart
	}

	return out
}

func lastAddr(p netip.Prefix) netip.Addr {
	bytes16 := p.Addr().As16()
	bits := p.Bits()
	if p.Addr().Is4() {
		bits += 96
	}
	for i := bits; i < 128; i++ {
		bytes16[i/8] |= 1 << uint(7-i%8)
	}
	addr := netip.AddrFrom16(bytes16)
	if p.Addr().Is4() {
		addr = addr.Unmap()
	}

	return addr
}

// aggregate merges adjacent and overlapping prefixes into the minimal covering set.
func aggregate(in []netip.Prefix) []netip.Prefix {
	if len(in) == 0 {
		return in
	}

	sort.Slice(in, func(i, j int) bool {
		cmp := in[i].Addr().Compare(in[j].Addr())
		if cmp != 0 {
			return cmp < 0
		}

		return in[i].Bits() < in[j].Bits()
	})

	// Allocate a fresh slice rather than aliasing in[:0] — the alias would
	// "work" today because deduplication only shrinks, but it's a footgun.
	deduped := make([]netip.Prefix, 0, len(in))
	for _, p := range in {
		if n := len(deduped); n > 0 && deduped[n-1].Bits() <= p.Bits() && deduped[n-1].Contains(p.Addr()) {
			continue
		}
		deduped = append(deduped, p)
	}

	for {
		merged := make([]netip.Prefix, 0, len(deduped))
		i := 0
		changed := false

		for i < len(deduped) {
			if i+1 < len(deduped) && canMerge(deduped[i], deduped[i+1]) {
				parent := netip.PrefixFrom(deduped[i].Addr(), deduped[i].Bits()-1).Masked()
				merged = append(merged, parent)
				i += 2
				changed = true

				continue
			}
			merged = append(merged, deduped[i])
			i++
		}

		deduped = merged
		if !changed {
			break
		}
	}

	return deduped
}

func canMerge(a, b netip.Prefix) bool {
	if a.Bits() != b.Bits() || a.Bits() == 0 || a.Addr().Is4() != b.Addr().Is4() {
		return false
	}
	parentA := netip.PrefixFrom(a.Addr(), a.Bits()-1).Masked()
	parentB := netip.PrefixFrom(b.Addr(), b.Bits()-1).Masked()

	return parentA == parentB && parentA.Contains(a.Addr()) && parentA.Contains(b.Addr())
}

func renderNetworks(ps []netip.Prefix, routeMap string) string {
	if len(ps) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, p := range ps {
		_, _ = fmt.Fprintf(&sb, "  network %s route-map %s\n", p.String(), routeMap)
	}

	return strings.TrimRight(sb.String(), "\n")
}

func splice(cfg, begin, end, body string) (string, error) {
	bIdx := strings.Index(cfg, begin)
	if bIdx < 0 {
		return "", fmt.Errorf("%w: %q", errBeginMissing, begin)
	}
	eIdx := strings.Index(cfg, end)
	if eIdx < 0 || eIdx < bIdx {
		return "", fmt.Errorf("%w: %q", errEndMissing, end)
	}

	afterBegin := bIdx + len(begin)
	if afterBegin < len(cfg) && cfg[afterBegin] == '\n' {
		afterBegin++
	}

	bodyText := body
	if bodyText != "" {
		bodyText += "\n"
	}

	return cfg[:afterBegin] + bodyText + cfg[eIdx:], nil
}

// atomicWrite writes data to a unique temp file in the same directory as
// path and renames it. Using os.CreateTemp prevents two concurrent
// invocations from clobbering each other on a shared `.new` suffix.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()

	_, err = tmp.Write(data)
	if err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)

		return fmt.Errorf("write tmp: %w", err)
	}
	err = tmp.Chmod(configFileMode)
	if err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)

		return fmt.Errorf("chmod tmp: %w", err)
	}
	err = tmp.Close()
	if err != nil {
		_ = os.Remove(tmpName)

		return fmt.Errorf("close tmp: %w", err)
	}
	err = os.Rename(tmpName, path)
	if err != nil {
		_ = os.Remove(tmpName)

		return fmt.Errorf("rename: %w", err)
	}

	return nil
}

// reloadFRR runs frr-reload.py with a dedicated timeout. The parent ctx
// covers the whole pipeline (5 min); reload alone gets 3 min so a slow
// frr-reload doesn't starve the outer budget.
func reloadFRR(parentCtx context.Context, frrConf string) error {
	log.Printf("running frr-reload.py")
	ctx, cancel := context.WithTimeout(parentCtx, frrReloadTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, frrReloadScript, "--reload", frrConf) //nolint:gosec // path is constant, conf is operator-supplied
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = []string{envPATH}
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("run frr-reload: %w", err)
	}

	return nil
}

func hashOf(s string) string {
	sum := sha256.Sum256([]byte(s))

	return hex.EncodeToString(sum[:])
}

func printSample(label, block string) {
	lines := strings.SplitN(block, "\n", sampleLines+1)
	if len(lines) > sampleLines {
		lines = lines[:sampleLines]
	}
	log.Printf("--- %s sample ---\n%s", label, strings.Join(lines, "\n"))
}
