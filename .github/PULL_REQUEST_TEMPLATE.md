<!--
Conventional Commits subject:
  type(scope): description
Allowed types: feat, fix, refactor, perf, docs, test, chore, ci, build.
-->

## What does this PR do?

<!-- One paragraph. Reference issues with #N. -->

## How was it tested?

<!--
At minimum:
  - `make ci` clean (zero lint findings, tests pass)
  - For feed-changes: `georoute --dry-run` output before/after
  - For FRR-touching changes: diff of `frr.conf` between markers
  - For nft-touching changes: `nft list table inet pbr` before/after
-->

## Operational impact

<!--
Will this require a restart of FRR? A reload? Disruption on the affected nodes?
If yes, how do operators verify the change took effect?
-->

## Checklist

- [ ] CI green (lint, vet, tests).
- [ ] No new `//nolint` directives without an explanation comment.
- [ ] CHANGELOG.md updated under `## [Unreleased]`.
- [ ] Docs touched if behavior changed (`docs/`).
- [ ] No secrets or PII in diff.
