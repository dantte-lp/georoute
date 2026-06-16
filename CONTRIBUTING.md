# Contributing

This is a private internal-infra project, so the contributor pool is small —
but the bar for changes is the same as for any production tool that touches
BGP and kernel routing tables.

## Ground rules

1. **Atomicity matters.** Any change to feed-application logic must keep both
   updates (nftables set and `frr.conf` splice) order-correct and rollback-safe.
   The current order is: nft first, then FRR; rationale is in
   [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md#update-ordering).
2. **Zero lint findings.** Run `make lint`. We use golangci-lint v2 with
   `default: all`. New code must clear it without `//nolint` directives unless
   the suppression is explained in a same-line comment.
3. **Single-file as long as possible.** Resist the urge to package-split until
   the file grows beyond ~600 lines or a logical sub-system (e.g. a second feed
   source) clearly justifies its own package.
4. **No external runtime dependencies.** Standard library only. Tests may use
   `testing`; benchmarks may use `testing.B`. That's it.
5. **No reflection, no codegen.** This is a 200-line tool. Keep it boring.

## Development loop

```bash
make fmt        # gofmt + goimports
make lint       # golangci-lint run
make build      # produces ./georoute
make test       # unit tests
make test-race  # unit tests with -race
```

Before opening a PR, run the full chain:

```bash
make ci
```

`make ci` is the exact command CI runs.

## Commit messages

Conventional Commits, scope-prefixed by area:

```
feat(feed): allow country code via --country flag
fix(nft): handle empty v6 prefix set without leaving stale elements
docs(arch): clarify why we mark in output chain with type=route
```

Allowed types: `feat`, `fix`, `refactor`, `perf`, `docs`, `test`, `chore`,
`ci`, `build`.

## Pull request flow

1. Branch from `main`. Branch names: `topic/<short-slug>`.
2. Open a draft PR early. CI must be green before review.
3. One reviewer minimum. The reviewer should run the new code in dry-run mode
   against a real frr.conf and confirm the diff is what was intended.
4. Squash-merge with a single Conventional Commit subject. The body of the
   merge commit must reference any related infra change (e.g. a node config
   update) so the operational context is recoverable from `git log`.

## Reporting bugs and ideas

- Bugs: use the bug report template; include the exact `frr.conf` marker block
  contents (with secrets redacted) and the relevant `nft list table inet pbr`
  output.
- Feature ideas: use the feature request template; argue for or against the
  change against the "ground rules" above.

## Code of conduct

See [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).
