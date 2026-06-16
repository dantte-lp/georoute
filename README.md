# georoute

> Country-based BGP advertisement + nftables PBR sync for geographically split exits.

`georoute` is a small Go daemon that keeps the gap between BGP control plane and
nftables data plane closed when you route by destination country. It fetches the
canonical country prefix list from RIPE Stat, aggregates it down to the minimal
covering set of CIDRs, and atomically updates:

1. **FRR** — splices `network X.X.X.X/Y route-map MARK-RU-EXIT` statements
   between marker comments in `frr.conf`, then triggers a `frr-reload.py`
   *(no-op when the diff is empty)*.
2. **nftables** — atomically replaces the contents of `inet pbr ru_v4` and
   `inet pbr ru_v6` sets via a single `nft -f -` transaction.

A separate `ip rule fwmark 0x201 lookup 100` + dedicated routing table 100 makes
matched destinations exit via the local uplink instead of the BGP-advertised
default. The main routing table stays compact (no 8 k+ static routes), and
BGP keeps advertising the country prefixes to the peer so the other site routes
matching traffic through this node.

## Why not static routes?

Static-route + `redistribute static` is the obvious approach, but it pollutes
the main FIB with thousands of entries and conflates "this is the path for the
data plane" with "this is what I want my peers to know." `georoute` keeps the
two strictly separated:

| Concern | Mechanism |
|---|---|
| Advertise prefixes to BGP peers | FRR `network X` with `no bgp network import-check` |
| Forward local-origin packets to those prefixes | nftables interval set + `fwmark` + policy routing |
| Forward transit packets to those prefixes | same — chain hooks both `prerouting` and `output` |
| Survive BGP failure | local kernel route in the dedicated table is independent |

The nftables interval set is tree-indexed (O(log n) on lookup) so the data-plane
cost is constant regardless of feed size.

## Status

Pre-1.0. Internal infra tooling. Production-ready for the dual-site exit pattern
described in [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md). Country selection is
currently hard-coded to `RU` against RIPE Stat; making the feed source/country
pluggable is on the roadmap.

## Quick start

Build (requires Go ≥ 1.26):

```bash
make build              # produces ./georoute
make install            # installs to /usr/local/bin/georoute
make install-systemd    # installs service + timer to /etc/systemd/system
```

One-shot run (idempotent, refuses to touch FRR if nothing changed):

```bash
georoute
```

Dry run (fetches and aggregates, prints sample, writes nothing):

```bash
georoute --dry-run
```

Force a write/reload even when the BGP block hashes are unchanged:

```bash
georoute --force
```

See [docs/CONFIGURATION.md](docs/CONFIGURATION.md) for the full flag reference
and [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) for the rest of the infrastructure
(nftables table layout, `ip rule`, FRR markers).

## Operating model

```
                   RIPE Stat (HTTPS)
                         │
                         ▼
                    ┌──────────┐
                    │ georoute │  ── timer-driven, OnUnitActiveSec=12h
                    └──────────┘
                       │      │
              ┌────────┘      └─────────┐
              ▼                          ▼
     ┌─────────────────┐         ┌──────────────────┐
     │  /etc/frr/      │         │  nft set         │
     │  frr.conf       │         │  inet pbr ru_v4  │
     │  (markers)      │         │  inet pbr ru_v6  │
     └─────────────────┘         └──────────────────┘
              │                          │
              ▼                          ▼
     frr-reload.py             kernel data plane
              │                          │
              ▼                          ▼
     BGP UPDATE to peer      mark 0x201 → table 100
                                         │
                                         ▼
                              default via local uplink
```

## Repository layout

```
.
├── main.go               # the entire updater, single file by design
├── go.mod
├── Makefile
├── deploy/
│   ├── systemd/          # service + timer units
│   ├── nftables/         # table inet pbr scaffolding
│   └── examples/         # frr.conf snippet showing required markers
├── docs/
│   ├── ARCHITECTURE.md   # control vs. data plane, why nftables + PBR
│   ├── DEPLOYMENT.md     # node prereqs, ip rule, table 100, systemd timer
│   ├── CONFIGURATION.md  # every flag, every marker, every exit code
│   └── RUNBOOK.md        # day-2 ops: rotate keys, recover from drift
└── .github/              # CI, issue templates, dependabot
```

## Documentation

- 🇬🇧 English (source of truth): files listed above.
- 🇷🇺 Russian (canonical translation): same filenames with `.ru.md` suffix
  (e.g. [README.ru.md](README.ru.md)).

## License

Proprietary. See [LICENSE](LICENSE).
