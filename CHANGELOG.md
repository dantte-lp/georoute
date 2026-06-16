# Changelog

All notable changes to this project are documented here.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
versioning follows [SemVer](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Initial implementation: fetch RIPE Stat country list, CIDR aggregate, atomic
  splice into FRR `frr.conf` between marker comments, atomic `nft -f -` update
  of `inet pbr` v4/v6 prefix sets.
- `--dry-run`, `--force`, `--nft=false`, `--reload=false`, `--frr-conf=PATH`
  CLI flags.
- Strict golangci-lint v2 configuration (`default: all` minus opinionated
  style linters); zero outstanding issues at first release.
- systemd `georoute.service` (oneshot) + `georoute.timer`
  (`OnBootSec=30s`, `OnUnitActiveSec=12h`, `RandomizedDelaySec=30min`).
- nftables `inet pbr` table scaffold (interval sets, prerouting + output hooks).
- FRR snippet under `deploy/examples/frr-snippet.conf` showing where the
  `! BEGIN-RU-FEED-V4` / `! END-RU-FEED-V4` (and v6) markers must live.

### Known limitations
- Country and feed source are hard-coded to `RU` / RIPE Stat. See the roadmap
  in [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md#roadmap).
- Single-binary deployment only; no agent/server split.
- nft set name (`ru_v4` / `ru_v6`) is hard-coded; renaming requires patching
  the binary.

[Unreleased]: https://github.com/dantte-lp/georoute/commits/main
