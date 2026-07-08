# Configuration reference

`georoute` is configured entirely by flags. Defaults are tuned for a node that
runs FRR with an integrated `frr.conf`, has nftables-backed firewalld (so the
`inet pbr` table is independent), and is happy to talk to RIPE Stat for the
feed.

## Flags

### Core (v1)

| Flag | Default | Effect |
|---|---|---|
| `--frr-conf` | `/etc/frr/frr.conf` | Path to the FRR config file containing the marker comments. |
| `--reload` | `true` | Run `frr-reload.py --reload <frr-conf>` after a successful write. Set to `false` for staged deployments. |
| `--nft` | `true` | Atomically replace `inet pbr <cc>_v4` / `<cc>_v6` set membership via `nft -f -`. Set to `false` to skip the data-plane update. |
| `--dry-run` | `false` | Fetch, aggregate, print a sample and the would-be counts. Make no changes. |
| `--force` | `false` | Apply even if the rendered BGP block matches what's already on disk. |
| `--lock-file` | `/run/georoute-<cc>.lock` | Path to the flock; lets two concurrent runs reject the loser fast. |

### Country selection (v2.0)

| Flag | Default | Env | Effect |
|---|---|---|---|
| `--country` | `RU` | `GEOROUTE_COUNTRY` | ISO-3166 alpha-2 code; all `<cc>` defaults derive from this. |
| `--feed-url` | RIPE country-resource-list URL | `GEOROUTE_FEED_URL` | Override the RIPE Stat endpoint (custom mirror, etc.). |
| `--route-map` | `MARK-<CC>-EXIT` | `GEOROUTE_ROUTE_MAP` | FRR route-map name used in every `network` line. |
| `--nft-set-v4` | `<cc>_v4` | `GEOROUTE_NFT_SET_V4` | nftables v4 set name in `inet pbr`. |
| `--nft-set-v6` | `<cc>_v6` | `GEOROUTE_NFT_SET_V6` | nftables v6 set name in `inet pbr`. |
| `--marker-prefix` | `<CC>-FEED` | `GEOROUTE_MARKER_PREFIX` | Marker comment prefix; controls the `BEGIN-<P>-V4` / `END-<P>-V4` form. |

### Operator data + state (v2.0)

| Flag | Default | Env | Effect |
|---|---|---|---|
| `--extras-v4-file` | `""` | `GEOROUTE_EXTRAS_V4_FILE` | Path to an operator-maintained IPv4 prefix list merged with the RIPE feed. One prefix per line, `#` comments allowed. Empty = no extras. |
| `--extras-v6-file` | `""` | `GEOROUTE_EXTRAS_V6_FILE` | Same for IPv6. |
| `--cache-file` | `/var/lib/georoute/feed-<cc>.json.gz` | `GEOROUTE_CACHE_FILE` | Gzip+JSON snapshot of the last good RIPE response. Used as fallback on consecutive 5xx. |
| `--cache-max-age` | `168h` (7 days) | `GEOROUTE_CACHE_MAX_AGE` | Max cache age before it is refused (a stale cache is worse than failing the cycle). |
| `--last-success-file` | `/var/lib/georoute/last-success-<cc>` | `GEOROUTE_LAST_SUCCESS_FILE` | Timestamp marker; `/readyz` reports unhealthy when missing or older than `--ready-max-age`. |

### Observability (v2.0 — opt-in via `--http-addr`)

| Flag | Default | Env | Effect |
|---|---|---|---|
| `--http-addr` | `""` | `GEOROUTE_HTTP_ADDR` | Listen address for `/live`, `/ready`, `/metrics`, `/debug/pprof/*`. Empty disables the server. |
| `--ready-max-age` | `24h` | `GEOROUTE_READY_MAX_AGE` | Age of last-success past which `/readyz` returns 503. |
| `--log-format` | `text` | `GEOROUTE_LOG_FORMAT` | `text` (human-readable) or `json` (one-record-per-line for systemd-journald structured ingest). |
| `--log-level` | `info` | `GEOROUTE_LOG_LEVEL` | `debug`, `info`, `warn`, `error`. |

### Tunable timeouts + tool paths (v2.1)

| Flag | Default | Env | Effect |
|---|---|---|---|
| `--http-timeout` | `60s` | `GEOROUTE_HTTP_TIMEOUT` | Per-request timeout for the RIPE Stat fetch. |
| `--frr-reload-timeout` | `3m` | `GEOROUTE_FRR_RELOAD_TIMEOUT` | Wall budget for `frr-reload.py`. |
| `--nft-timeout` | `30s` | `GEOROUTE_NFT_TIMEOUT` | Budget for the `nft -f -` set replacement. |
| `--retry-attempts` | `3` | `GEOROUTE_RETRY_ATTEMPTS` | Number of RIPE Stat fetch attempts before falling back to the cache. |
| `--retry-base-delay` | `2s` | `GEOROUTE_RETRY_BASE_DELAY` | Linear backoff base; the N-th retry waits `(N-1) * delay`. |
| `--frr-reload-script` | `/usr/lib/frr/frr-reload.py` | `GEOROUTE_FRR_RELOAD_SCRIPT` | Override for distros that ship it elsewhere. |
| `--nft-binary` | `/usr/sbin/nft` | `GEOROUTE_NFT_BINARY` | Override for musl or alternate installs. |
| `--refresh-interval` | `0` (oneshot) | `GEOROUTE_REFRESH_INTERVAL` | When `> 0` + `--http-addr` is set: enter daemon mode. The work pipeline runs on this interval; `Type=oneshot` semantics are off. |

### Target rendering (experimental)

Target rendering lets `georoute` act as a prefix-loader for routers that do not
run local FRR. It does not become a routing daemon: it fetches, validates,
aggregates, and renders prefix or policy objects. The target router still owns
route origin, protocol sessions, forwarding, NAT, and policy-based routing.

| Flag | Default | Effect |
|---|---|---|
| `--target` | `frr` | Target adapter. `frr` keeps the legacy FRR + nftables apply path. `file` and `iosxe` currently support render mode only. |
| `--mode` | `apply` | Execution mode. Use `render` with non-FRR targets. |
| `--target-output` | `-` | Render destination. `-` writes to stdout; any other value is written atomically as a file. |
| `--iosxe-prefix-list` | `PL-GEOROUTE-<CC>-V4` | IOS XE IPv4 prefix-list object name. |
| `--iosxe-route-map` | `RM-GEOROUTE-<CC>-OUT` | IOS XE route-map object name matching the generated prefix-list. |
| `--iosxe-route-map-seq` | `10` | IOS XE route-map permit sequence for the generated prefix-list match. Use a free sequence when adding georoute to an existing route-map. |
| `--iosxe-route-map-deny-seq` | `9999` | IOS XE route-map explicit deny sequence. |
| `--iosxe-replace-prefix-list` | `false` | Render `no ip prefix-list <name>` before IOS XE prefix-list entries. Use when the rendered candidate is applied as the authoritative object state and stale entries must be removed. |
| `--iosxe-static-next-hop` | `""` | IOS XE static route next-hop. Empty disables static-route rendering. |
| `--iosxe-static-route-map` | `RM-GEOROUTE-<CC>-STATIC` | IOS XE route-map used by `redistribute static`. Defaults only when `--target=iosxe`. |
| `--iosxe-static-route-name` | `GEOROUTE_<CC>` | IOS XE static route name for generated static routes. Defaults only when `--target=iosxe`. |
| `--iosxe-static-tag` | `0` | Optional IOS XE static route tag. `0` omits the tag. |
| `--iosxe-bgp-as` | `""` | IOS XE BGP AS used to render `router bgp <asn> address-family ipv4 redistribute static route-map <name>`. Empty disables the BGP stanza. |
| `--iosxe-apply-max-prefixes` | `100` | Safety limit for future IOS XE apply mode. Full feeds must raise this explicitly after diff/postcheck support exists. |
| `--iosxe-ssh-host` | `""` | IOS XE SSH host for apply mode. Required for `--target=iosxe --mode=apply`. |
| `--iosxe-ssh-port` | `22` | IOS XE SSH port for apply mode. |
| `--iosxe-ssh-user` | `""` | IOS XE SSH username for apply mode. Required. |
| `--iosxe-ssh-key-file` | `""` | IOS XE SSH private key file for apply mode. Required. |
| `--iosxe-known-hosts-file` | `""` | `known_hosts` file used for IOS XE host key verification. Required; insecure host-key ignore is not supported. |
| `--iosxe-ssh-timeout` | `30s` | SSH dial and session timeout for IOS XE apply mode. |
| `--iosxe-save-config` | `false` | Run `write memory` after IOS XE apply postcheck succeeds. |
| `--iosxe-vrf-leak-route-map` | `""` | Optional IOS XE route-map that leaks generated global BGP prefixes into a service VRF, for example `RM-GLOBAL-TO-TRANSIT`. Empty disables VRF leak rendering. |
| `--iosxe-vrf-leak-route-map-seq` | `20` | Permit sequence for `--iosxe-vrf-leak-route-map`. The renderer emits only the permit sequence and does not replace existing deny tails. |

Examples:

```bash
georoute --country=RU --target=file --mode=render --target-output=ru-prefixes.txt
georoute --country=RU --target=iosxe --mode=render --target-output=iosxe-ru.cfg
georoute --country=RU --target=iosxe --mode=render --iosxe-replace-prefix-list --target-output=iosxe-ru.cfg
georoute --country=RU --target=iosxe --mode=render --iosxe-replace-prefix-list --iosxe-route-map=RM-RU-GEOROUTE-OUT --iosxe-route-map-seq=20 --target-output=iosxe-ru-c8000v.cfg
georoute --country=RU --target=iosxe --mode=render --iosxe-replace-prefix-list --iosxe-route-map=RM-RU-GEOROUTE-OUT --iosxe-route-map-seq=20 --iosxe-static-next-hop=94.139.250.25 --iosxe-bgp-as=4200099722 --target-output=iosxe-ru-c8000v-static-origin.cfg
```

The IOS XE renderer emits an explicit `deny 0.0.0.0/0 le 32` prefix-list tail
and an explicit route-map `deny 9999`. Without `--iosxe-replace-prefix-list`,
IOS XE keeps old prefix-list sequence entries if a later feed shrinks. Enable
replace mode for idempotent loads into C8000V, then apply the candidate during a
controlled config transaction.

Static-origin rendering is optional. When `--iosxe-static-next-hop` is set,
the candidate includes one `ip route <prefix> <mask> <next-hop>` line per
aggregated IPv4 prefix. When `--iosxe-bgp-as` is also set, the candidate
includes a dedicated static route-map and `redistribute static route-map`
stanza. Use this only when the target router must own route origin for the feed.

IOS XE apply mode opens an SSH session with public-key authentication, verifies
the router host key through `known_hosts`, sends the rendered candidate in
configuration mode, then runs show-based postchecks for the generated
prefix-list, route-map and optional static redistribution stanza. The default
`--iosxe-apply-max-prefixes=100` intentionally blocks full production feeds
unless the operator raises the limit explicitly.

When a service peer lives in an IOS XE VRF, static-origin routes created in the
global table must be leaked into that VRF before the outbound neighbor route-map
can advertise them. Use `--iosxe-vrf-leak-route-map` to add a separate permit
sequence matching the generated prefix-list to the existing leak route-map
without changing its current non-georoute sequences.

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
