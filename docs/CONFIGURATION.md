# Configuration reference

`georoute` is configured entirely by flags. Defaults are tuned for a node that
runs FRR with an integrated `frr.conf`, has nftables-backed firewalld (so the
`inet pbr` table is independent), and is happy to talk to RIPE Stat for the
feed.

## Flags

| Flag | Default | Effect |
|---|---|---|
| `--frr-conf` | `/etc/frr/frr.conf` | Path to the FRR config file containing the marker comments. |
| `--reload` | `true` | Run `frr-reload.py --reload <frr-conf>` after a successful write. Set to `false` for staged deployments. |
| `--nft` | `true` | Atomically replace `inet pbr ru_v4` / `ru_v6` set membership via `nft -f -`. Set to `false` to skip the data-plane update. |
| `--dry-run` | `false` | Fetch, aggregate, print a sample and the would-be counts. Make no changes. Mutually exclusive with `--reload` and write semantics. |
| `--force` | `false` | Apply even if the rendered BGP block matches what's already on disk. Useful after a manual edit to `frr.conf`. |

## Markers in `frr.conf`

`georoute` writes `network X/Y route-map MARK-RU-EXIT` lines between markers.
The markers must already exist in the file before the first run:

```
! BEGIN-RU-FEED-V4
! END-RU-FEED-V4
```

and

```
! BEGIN-RU-FEED-V6
! END-RU-FEED-V6
```

They go *inside* an `address-family ipv4 unicast` / `address-family ipv6
unicast` block of a `router bgp` stanza. Indentation matters: two leading
spaces, matching the surrounding `network` lines. See
[deploy/examples/frr-snippet.conf](../deploy/examples/frr-snippet.conf).

## Data-plane expectations

`georoute` assumes the static scaffolding is already in place on the node:

1. The nftables table `inet pbr` exists with `set ru_v4` and `set ru_v6`
   (type `ipv4_addr` / `ipv6_addr`, `flags interval`). Use
   [deploy/nftables/pbr.nft](../deploy/nftables/pbr.nft) once at install.
2. The kernel has an `ip rule` directing `fwmark 0x201` to a numbered table
   (the default in our examples is `100`):

   ```bash
   ip -4 rule add fwmark 0x201 lookup 100 priority 100
   ip -6 rule add fwmark 0x201 lookup 100 priority 100
   ```
3. Table 100 contains the *local* exit's default route — i.e. where the
   marked traffic should go:

   ```bash
   ip -4 route add default via <local-uplink-gw> dev <iface> table 100
   ip -6 route add default dev <v6-iface> table 100
   ```

`georoute` itself only touches *contents* of the sets; the scaffolding above
is install-time and stays put.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Success. Either nothing changed and we skipped, or change applied. |
| 1 | A step failed (fetch, aggregate, splice, write, reload). Inspect stderr / journald. |

## Idempotency

A run that produces the same rendered output as the previous run does *not*
rewrite `frr.conf` and does *not* call `frr-reload.py`. The nft update is
always issued (it's a single transaction; the cost is negligible), and `nft`
itself is idempotent — replacing a set with identical content is a no-op
relative to the data plane.

This makes `OnUnitActiveSec=12h` cheap: nothing happens if the feed didn't
change.

## Failure modes

- **RIPE Stat unreachable.** Run exits with code 1. The next scheduled run
  (timer) retries. No state is left half-applied.
- **`frr.conf` markers missing.** Run exits with `errBeginMissing` /
  `errEndMissing`. Add the markers (see above) and re-run.
- **`nft -f -` fails.** Likely because the table or set doesn't exist. Load
  [deploy/nftables/pbr.nft](../deploy/nftables/pbr.nft) once and try again.
- **`frr-reload.py` syntax error.** `georoute` already wrote the new
  `frr.conf`. Inspect, fix, and either revert via `git checkout` (if you
  version `/etc/frr/`) or hand-edit. Re-run with `--reload` to push.

## Source code layout

The whole tool is a single `main.go`. The intentional structure:

| Section | Responsibility |
|---|---|
| `fetch` | HTTP `GET` to RIPE Stat, JSON decode, basic shape check. |
| `parsePrefixes` / `parseRange` | Accept CIDR or `start-end` ranges from the feed; normalize to `netip.Prefix`. |
| `aggregate` | Sort, drop strict subsets, iteratively coalesce adjacent same-length prefixes. |
| `renderNetworks` | Emit `network X/Y route-map MARK-RU-EXIT` lines. |
| `splice` | Replace the marker-bounded block in `frr.conf`. |
| `applyNft` | Build a single `nft -f -` script that flushes both sets and adds elements. |
| `atomicWrite` | `<file>.new` + `rename`. |
| `reloadFRR` | Shell out to `frr-reload.py --reload`. |

Adding a flag means adding fields to `cliFlags`, parsing in `realMain`, and
threading through `run`. There is no plugin system, no DI container, and no
config file by design.
