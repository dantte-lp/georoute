# Security Policy

## Reporting a vulnerability

This is a private internal-infra project. If you discover a vulnerability,
do **not** open a public issue. Send a private report to the maintainer
listed in [MAINTAINERS](MAINTAINERS) (encrypted mail preferred — request the
PGP key first).

Include in the report:

1. A short description of the issue.
2. Steps to reproduce, ideally a minimal proof of concept.
3. The impact you believe it has.
4. The commit / version you tested against.

We aim to acknowledge within **3 business days** and provide a remediation
plan within **10 business days** for high-severity issues.

## Scope

In scope:

- The `georoute` Go binary itself (input parsing, command execution, file
  writes, network requests, signal handling).
- Bundled systemd units, nftables snippet, and `frr-reload.py` invocation
  semantics.

Out of scope:

- Bugs in FRR, nftables, the Linux kernel, RIPE Stat, or any other third
  party. Please report those upstream.
- Misconfiguration of the operator's nodes (we cannot fix bad
  `AllowedIPs`, missing `Table = off`, etc. via this binary).

## Defensive defaults

- The binary writes `frr.conf` with mode `0640` (root-readable, frr-readable)
  and renames atomically over the existing file.
- The binary calls `nft -f -` with a deterministic stdin and never logs the
  contents of files containing keys.
- All external calls have explicit `context.Context` timeouts (60 s for
  RIPE fetch, 30 s for `nft`, inherited for `frr-reload.py`).
- HTTP fetcher refuses non-`200` responses and returns a typed error.
- The systemd unit runs as a oneshot under `root`; we do not need
  `CAP_NET_ADMIN`-only restriction because `frr-reload.py` and `nft` both
  require root anyway. See [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md#privileges)
  for the threat model.

## Known weak spots

- The `frr-reload.py` script is invoked with the path to the modified
  `frr.conf`; if an attacker has write access to that file before we run,
  they can inject arbitrary FRR config. This binary is intended to run on
  trusted nodes only.
- The nft set is replaced atomically per transaction; however, the *static
  scaffold* (`/etc/nft.d/pbr.nft`) is loaded by a separate systemd unit at
  boot — if that file is tampered with, the mark/PBR redirect can be
  diverted. Treat it as part of the host's trusted compute base.
