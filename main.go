// Command georoute fetches the RU country prefix list from RIPE Stat,
// aggregates it, splices BGP `network` statements into the FRR config between
// marker comments, and runs frr-reload only when the resulting block changes.
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
	"sort"
	"strings"
	"time"
)

const (
	ripeURL = "https://stat.ripe.net/data/country-resource-list/data.json?resource=RU&v4_format=prefix"

	beginV4 = "  ! BEGIN-RU-FEED-V4"
	endV4   = "  ! END-RU-FEED-V4"
	beginV6 = "  ! BEGIN-RU-FEED-V6"
	endV6   = "  ! END-RU-FEED-V6"

	httpTimeout = 60 * time.Second
	sampleLines = 5

	configFileMode  = 0o640
	limitErrorBody  = 4096
	frrReloadScript = "/usr/lib/frr/frr-reload.py"

	nftBinary  = "/usr/sbin/nft"
	nftTable   = "inet pbr"
	nftSetV4   = "ru_v4"
	nftSetV6   = "ru_v6"
	nftTimeout = 30 * time.Second
)

// Static error values let callers errors.Is them and satisfy err113.
var (
	errBadStatus    = errors.New("RIPE Stat status not ok")
	errHTTP         = errors.New("RIPE Stat HTTP error")
	errBeginMissing = errors.New("begin marker not found in FRR config")
	errEndMissing   = errors.New("end marker not found or misplaced in FRR config")
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
	frrConf   string
	reloadOK  bool
	dryRun    bool
	force     bool
	updateNft bool
}

func realMain() int {
	flags := cliFlags{}
	flag.StringVar(&flags.frrConf, "frr-conf", "/etc/frr/frr.conf", "path to FRR config")
	flag.BoolVar(&flags.reloadOK, "reload", true, "run frr-reload on change")
	flag.BoolVar(&flags.dryRun, "dry-run", false, "print summary without writing")
	flag.BoolVar(&flags.force, "force", false, "force write even if unchanged")
	flag.BoolVar(&flags.updateNft, "nft", true, "atomically replace nft set inet pbr {ru_v4,ru_v6}")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.LUTC)

	ctx, cancel := context.WithTimeout(context.Background(), 2*httpTimeout)
	defer cancel()

	err := run(ctx, flags)
	if err != nil {
		log.Printf("georoute: %v", err)

		return 1
	}

	return 0
}

func run(ctx context.Context, f cliFlags) error {
	log.Printf("fetching RIPE Stat RU resources")

	raw, err := fetch(ctx)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	log.Printf("raw: %d v4 prefixes, %d v6 prefixes",
		len(raw.Data.Resources.IPv4), len(raw.Data.Resources.IPv6))

	v4Agg := aggregate(parsePrefixes(raw.Data.Resources.IPv4))
	v6Agg := aggregate(parsePrefixes(raw.Data.Resources.IPv6))
	log.Printf("aggregated: %d v4, %d v6", len(v4Agg), len(v6Agg))

	v4Block := renderNetworks(v4Agg)
	v6Block := renderNetworks(v6Agg)
	log.Printf("v4 hash=%s v6 hash=%s", hashOf(v4Block)[:12], hashOf(v6Block)[:12])

	if f.dryRun {
		printSample("v4-bgp", v4Block)
		printSample("v6-bgp", v6Block)
		log.Printf("nft v4 set would have %d elements; v6 set %d", len(v4Agg), len(v6Agg))

		return nil
	}

	// nft FIRST: update data plane before advertising via BGP. If a new RU prefix
	// arrives in dev-03 RIB before dev-04 marks it, dev-04 would loop it back —
	// updating nft first avoids that window.
	if f.updateNft {
		err = applyNft(ctx, v4Agg, v6Agg)
		if err != nil {
			return fmt.Errorf("apply nft: %w", err)
		}
	}

	cur, err := os.ReadFile(f.frrConf)
	if err != nil {
		return fmt.Errorf("read frr.conf: %w", err)
	}

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

// applyNft atomically replaces the contents of inet pbr {ru_v4, ru_v6} using
// a single `nft -f -` transaction. Empty prefix slices flush the set.
func applyNft(ctx context.Context, v4, v6 []netip.Prefix) error {
	var script strings.Builder
	_, _ = fmt.Fprintf(&script, "flush set %s %s\n", nftTable, nftSetV4)
	if len(v4) > 0 {
		_, _ = fmt.Fprintf(&script, "add element %s %s { %s }\n", nftTable, nftSetV4, joinPrefixes(v4))
	}
	_, _ = fmt.Fprintf(&script, "flush set %s %s\n", nftTable, nftSetV6)
	if len(v6) > 0 {
		_, _ = fmt.Fprintf(&script, "add element %s %s { %s }\n", nftTable, nftSetV6, joinPrefixes(v6))
	}

	nftCtx, cancel := context.WithTimeout(ctx, nftTimeout)
	defer cancel()

	cmd := exec.CommandContext(nftCtx, nftBinary, "-f", "-")
	cmd.Stdin = strings.NewReader(script.String())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("nft -f: %w", err)
	}
	log.Printf("nft sets updated (v4=%d v6=%d)", len(v4), len(v6))

	return nil
}

func joinPrefixes(ps []netip.Prefix) string {
	parts := make([]string, len(ps))
	for i, p := range ps {
		parts[i] = p.String()
	}

	return strings.Join(parts, ", ")
}

func fetch(ctx context.Context) (*ripeResp, error) {
	client := &http.Client{Timeout: httpTimeout}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ripeURL, nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("User-Agent", "georoute/1.0 (infra4.dev)")

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
	err = json.NewDecoder(resp.Body).Decode(&parsed)
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

	deduped := in[:0]
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

func renderNetworks(ps []netip.Prefix) string {
	if len(ps) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, p := range ps {
		_, _ = fmt.Fprintf(&sb, "  network %s route-map MARK-RU-EXIT\n", p.String())
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

func atomicWrite(path string, data []byte) error {
	tmp := path + ".new"
	err := os.WriteFile(tmp, data, configFileMode) //nolint:gosec // group-read for `frr` user is intentional
	if err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	err = os.Rename(tmp, path)
	if err != nil {
		return fmt.Errorf("rename: %w", err)
	}

	return nil
}

func reloadFRR(ctx context.Context, frrConf string) error {
	log.Printf("running frr-reload.py")
	cmd := exec.CommandContext(ctx, frrReloadScript, "--reload", frrConf) //nolint:gosec // path is constant, conf is operator-supplied
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
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
