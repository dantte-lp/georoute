---
name: Bug report
about: Report a defect in georoute behavior
title: "bug: <short summary>"
labels: ["bug"]
assignees: []
---

## Summary

<!-- One sentence: what is happening and what did you expect? -->

## Reproduction

1.
2.
3.

## Observed vs. expected

- **Observed:**
- **Expected:**

## Environment

- georoute version (`georoute --version`):
- Go version (`go version`):
- OS / kernel (`uname -a`):
- FRR version (`vtysh -c 'show version'`):
- nftables version (`nft --version`):

## Relevant output

<details><summary>Redacted FRR config between markers</summary>

```
<-- ! BEGIN-RU-FEED-V4 ... ! END-RU-FEED-V4 -->
```
</details>

<details><summary><code>nft list table inet pbr</code></summary>

```

```
</details>

<details><summary>journal: <code>journalctl -u georoute --no-pager -n 200</code></summary>

```

```
</details>

## Anything else worth knowing?
