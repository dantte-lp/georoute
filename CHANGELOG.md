# Changelog

All notable changes to this project are documented here.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
versioning follows [SemVer](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

[Unreleased]: https://github.com/dantte-lp/georoute/compare/v2.1.0...HEAD

## [2.1.0] - 2026-06-27

### Added
- Daemon mode via `--refresh-interval=N` (`GEOROUTE_REFRESH_INTERVAL`).
  When set + `--http-addr` is set, the process runs the work pipeline
  on an internal ticker instead of exiting after one cycle. SIGTERM
  cancels the loop and drains any in-flight cycle (no nft / frr-reload
  is left mid-transaction).
- Eight new operator-tunable flags, matching `GEOROUTE_*` env vars:
  - `--http-timeout` (default `60s`)
  - `--frr-reload-timeout` (default `3m`)
  - `--nft-timeout` (default `30s`)
  - `--retry-attempts` (default `3`)
  - `--retry-base-delay` (default `2s`)
  - `--frr-reload-script` (default `/usr/lib/frr/frr-reload.py`)
  - `--nft-binary` (default `/usr/sbin/nft`)
  - `--refresh-interval` (default `0` = oneshot semantics)
- New Prometheus counter `georoute_skipped_overlap_total{country}` —
  bumps when a refresh tick fires while the previous cycle is still
  holding the work mutex.
- Per-cycle `run_id` in JSON logs (daemon mode). Oneshot still emits
  exactly one id per process, identical to v2.0.

### Changed
- `realMain` now synchronously binds the HTTP listener via the new
  `healthServer.preBind()` before any work starts. Previously the
  listener was opened inside a goroutine, so `EADDRINUSE` / `EACCES`
  leaked silently into the log and the parent still parked; systemd
  saw the unit as UP while `/metrics` was dead. The bind error now
  exits the process with status 1.
- `applyFRRConfigOpts` accepts the FRR-reload script path + timeout
  via the `applyOpts` struct rather than reading package constants.
- `fetch`/`fetchWithRetry`/`fetchWithCache` thread the HTTP timeout,
  retry count, and base delay from `cliFlags` instead of constants.
- `applyNft` reads the nft binary path + timeout from `cliFlags`.

### Fixed
- Daemon-mode shutdown: `refreshLoop` now `sync.WaitGroup`-drains
  in-flight work before returning, so SIGTERM is properly ordered.

### Operations
- New file: `roles/georoute/defaults/main.yml` in the polyexit-prod
  role with `georoute_mode: oneshot | daemon` toggle plus the new
  flag-backed env vars.
- dev-04 switched to daemon mode (bind `127.0.0.1:9494`, refresh
  every 12h, log-format=json). The team-default `:9090` was occupied
  by the crowdsec ocserv-bouncer; pick a free port per host.

[2.1.0]: https://github.com/dantte-lp/georoute/releases/tag/v2.1.0

## [2.0.0] - 2026-06-27

Umbrella release for PRs #5/6/7/8/10/11 between v1.0 and this tag.
Every new feature is opt-in — empty flag values preserve v1 behavior
— so existing `.env` files keep working without changes.

### Added — Operator extras (#6)
- `--extras-v4-file` / `--extras-v6-file` merge operator-maintained
  prefix lists with the RIPE response before aggregation. Strict
  `netip.ParsePrefix` validation; bad lines fail with a line-numbered
  error.

### Added — Cache fallback + FRR rollback (#7)
- `--cache-file` (`/var/lib/georoute/feed-<cc>.json.gz`) — gzip+JSON
  snapshot of the last successful RIPE response. On 5xx exhaustion
  the binary falls back to the cache if it's still fresh.
- `--cache-max-age` (default 7 days).
- FRR rollback: `vtysh -C` pre-validates the staged config; on
  reload failure the previous known-good `frr.conf` is restored
  byte-exactly and `frr-reload.py` is re-run to resync.

### Added — Healthcheck HTTP server (#8)
- Optional `--http-addr=:port` starts an embedded HTTP server with
  `/live`, `/ready`, and `/debug/pprof/*`.
- `--last-success-file` records the cycle timestamp; `/ready` reports
  503 when missing or older than `--ready-max-age` (default 24h).

### Added — Prometheus /metrics (#10)
- `/metrics` endpoint sharing one `*prometheus.Registry` between the
  healthcheck library, Go runtime collectors, and the app series:
  - `georoute_runs_total{country, result}`
  - `georoute_fetches_total{country, source, result}`
  - `georoute_nft_applies_total{country, result}`
  - `georoute_frr_reloads_total{country, result}`
  - `georoute_prefixes{country, family, source}`
  - `georoute_last_success_unixtime{country}`
  - `georoute_cache_age_seconds{country}`
  - `georoute_fetch_duration_seconds{country, source}`
  - `georoute_nft_apply_duration_seconds{country}`
  - `georoute_frr_reload_duration_seconds{country}`

### Added — Structured logs + run_id (#11)
- `--log-format=text|json` (default `text`).
- `--log-level=debug|info|warn|error` (default `info`).
- Every record carries `country`; cycle-scoped records carry a
  per-cycle `run_id` (UUIDv4).

### Added — Multi-country (#5)
- `--country=<ISO2>` flag (default `RU`). All `<cc>` defaults derive
  from it. Existing RU deploys keep working unchanged.

### Changed
- systemd template: `RuntimeDirectoryPreserve=yes` for stable flock
  path, `StateDirectory=georoute` for the cache + last-success
  marker, `ReadWritePaths=/run/frr` for frr-reload's temp file under
  `ProtectSystem=strict`.

[2.0.0]: https://github.com/dantte-lp/georoute/releases/tag/v2.0.0

## [1.0.0] - 2026-06-15

### Added
- Initial implementation: fetch RIPE Stat country list, CIDR aggregate, atomic
  splice into FRR `frr.conf` between marker comments, atomic `nft -f -` update
  of `inet pbr` v4/v6 prefix sets.
- `--dry-run`, `--force`, `--nft=false`, `--reload=false`, `--frr-conf=PATH`
  CLI flags.
- Strict golangci-lint v2 configuration; zero outstanding issues at first
  release.
- systemd `georoute.service` (oneshot) + `georoute.timer`
  (`OnBootSec=30s`, `OnUnitActiveSec=12h`, `RandomizedDelaySec=30min`).
- nftables `inet pbr` table scaffold (interval sets, prerouting + output hooks).
- FRR snippet under `deploy/examples/frr-snippet.conf`.

### Known limitations
- Country and feed source hard-coded to `RU` / RIPE Stat (lifted in v2).
- Single-binary deployment only.
- nft set names hard-coded (lifted in v2).

[1.0.0]: https://github.com/dantte-lp/georoute/releases/tag/v1.0.0
