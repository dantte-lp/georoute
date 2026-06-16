# georoute

> Country-based BGP advertisement + nftables PBR sync for geographically split exits.

`georoute` is a small Go daemon that keeps the gap between BGP control plane and
nftables data plane closed when you route by destination country. It fetches the
canonical country prefix list from [RIPE Stat](https://stat.ripe.net/docs/data_api),
aggregates it down to the minimal covering set of CIDRs, and atomically updates:

1. **FRR** — splices `network X/Y route-map MARK-<CC>-EXIT` statements between
   marker comments in `frr.conf`, then triggers `frr-reload.py`
   *(no-op when the rendered block is byte-identical to the previous run)*.
2. **nftables** — atomically replaces the contents of `inet pbr <cc>_v4` and
   `inet pbr <cc>_v6` sets via a single `nft -f -` transaction (one netlink
   batch, kernel applies atomically — see
   [nftables wiki](https://wiki.nftables.org/wiki-nftables/index.php/Atomic_rule_replacement)).

A separate `ip rule fwmark <X> lookup <N>` + per-country routing table makes
matched destinations exit via the *local* uplink instead of the BGP-advertised
default. The main routing table stays compact (no 8 000-entry static dump), and
BGP keeps advertising the country prefixes to siblings so they route matching
traffic *into* this node.

## Multi-country

A single binary handles any country. Pass `--country UZ`, `--country KZ`, etc.
Five further flags (`--route-map`, `--nft-set-v4`, `--nft-set-v6`,
`--marker-prefix`, `--feed-url`) are derived from the country code; override
only when you need a non-default name (e.g. legacy `MARK-RU-EXIT`).

Run multiple instances on the same host with the systemd template unit:

```bash
systemctl enable --now georoute@ru.timer
systemctl enable --now georoute@uz.timer
systemctl list-timers georoute@*.timer
```

Each instance reads `/etc/georoute/<cc>.env`.

## Why not static routes?

Static-route + `redistribute static` is the obvious approach, but it pollutes
the main FIB with thousands of entries and conflates "this is the path for the
data plane" with "this is what I want my peers to know." `georoute` keeps the
two strictly separated:

| Concern                                          | Mechanism                                                                  |
|--------------------------------------------------|----------------------------------------------------------------------------|
| Advertise prefixes to BGP peers                  | FRR `network X` with `no bgp network import-check`                         |
| Forward local-origin packets to those prefixes   | nftables interval set + `fwmark` + policy routing                          |
| Forward transit packets to those prefixes        | same — chain hooks both `prerouting` and `output`                          |
| Survive BGP failure                              | local kernel route in the dedicated table is independent                   |

The nftables interval set is tree-indexed (O(log n) on lookup) so the data-plane
cost is constant regardless of feed size.

## Quick start

Build (requires Go ≥ 1.26):

```bash
make build               # produces ./georoute
make install             # installs to /usr/local/bin/georoute
make install-systemd     # installs georoute@.service + georoute@.timer
```

Drop a per-country env file and enable the instance:

```bash
cp deploy/systemd/georoute.env.example /etc/georoute/ru.env
$EDITOR /etc/georoute/ru.env             # change GEOROUTE_COUNTRY if needed
systemctl enable --now georoute@ru.timer
```

One-shot manual run (idempotent — refuses to touch FRR if the rendered block is
unchanged):

```bash
georoute --country=RU
```

Dry run — fetches and aggregates, prints sample, writes nothing:

```bash
georoute --country=UZ --dry-run
```

Force a write/reload even when hashes match (post-recovery):

```bash
georoute --country=RU --force
```

## CLI flags

```text
-country string         ISO-3166 alpha-2 country code (RU, UZ, KZ, …) (default "RU")
-route-map string       FRR route-map name                          (default MARK-<CC>-EXIT)
-nft-set-v4 string      nftables v4 set name                        (default <cc>_v4)
-nft-set-v6 string      nftables v6 set name                        (default <cc>_v6)
-marker-prefix string   marker comment prefix                       (default <CC>-FEED)
-feed-url string        RIPE Stat URL                                (default country-resource-list for <cc>)
-lock-file string       exclusive flock path                         (default /run/georoute-<cc>.lock)
-frr-conf string        path to FRR config                          (default /etc/frr/frr.conf)
-reload                 run frr-reload on change                    (default true)
-nft                    atomically replace nft set <cc>_v4 / <cc>_v6 (default true)
-dry-run                print summary without writing
-force                  force write even if unchanged
```

## Safety properties

| Property                                          | Mechanism                                                                              |
|---------------------------------------------------|----------------------------------------------------------------------------------------|
| No race between timer cycles or timer + manual    | `flock(2)` on `/run/georoute-<cc>.lock`                                                |
| `frr.conf` write is atomic                        | `os.CreateTemp` in the same dir + `rename` — no shared `.new` suffix                   |
| `nft` set replacement is atomic                   | single `nft -f -` invocation = one netlink batch                                       |
| Hostile JSON can't OOM the host                   | `io.LimitReader(_, 32 MiB)` on response body                                            |
| Transient RIPE 503/429 doesn't skip a 12-h cycle  | 3 attempts with exponential backoff                                                    |
| `frr-reload.py` can't exceed its time budget      | dedicated 3-minute child context (parent ctx is 5 min total)                            |
| Hostile env from a misbehaving caller can't leak  | `cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin"}`                              |
| Idempotency                                       | SHA-256 hash of rendered block — `frr.conf` rewritten only on diff (unless `--force`)  |

## Operating model

```text
                   RIPE Stat (HTTPS)
                         │
                         ▼
                    ┌──────────┐
                    │ georoute │  ── timer-driven, OnUnitActiveSec=12h, 30min jitter
                    └──────────┘
                       │      │
              ┌────────┘      └─────────┐
              ▼                          ▼
     ┌─────────────────┐         ┌────────────────────────┐
     │  /etc/frr/      │         │  nft set               │
     │  frr.conf       │         │  inet pbr <cc>_v4      │
     │  (markers)      │         │  inet pbr <cc>_v6      │
     └─────────────────┘         └────────────────────────┘
              │                              │
              ▼                              ▼
        frr-reload.py                 kernel data plane
              │                              │
              ▼                              ▼
      BGP UPDATE to peer           mark 0x<x> → table <N>
                                   → local uplink (NAT44/NAT66)
```

## Documentation

| Document                                      | Topic                                                              |
|-----------------------------------------------|--------------------------------------------------------------------|
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)  | Why this exists; FRR community split; routing table layout         |
| [docs/CONFIGURATION.md](docs/CONFIGURATION.md)| Every flag in detail; idempotency contract; failure modes          |
| [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md)      | Installing on a node from scratch                                  |
| [docs/RUNBOOK.md](docs/RUNBOOK.md)            | Day-2 procedures and incident triage                               |

Russian translations are available alongside (`docs/<NAME>.ru.md`).

## Status

`v2.0.0` — multi-country flag-driven, single-shot systemd replaced by a
template unit per ISO code. Backward-compatible defaults for existing RU
deploys (no flag = `--country=RU`).

## License

MIT — see [LICENSE](LICENSE).
