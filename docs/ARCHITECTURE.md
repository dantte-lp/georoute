# Architecture

> The control plane and the data plane do different jobs. `georoute` keeps both
> in step from a single source — a country prefix feed — without coupling them.

## The problem

When you split exits by destination country, you need *two* lists in two
places that have to agree:

1. **The control plane**: BGP must advertise the country prefixes to your
   peers, so traffic from elsewhere in your network is steered toward this
   node.
2. **The data plane**: the local kernel FIB must forward matching traffic out
   the right uplink — *without* also installing the advertised prefixes
   into the main routing table (that would pollute every other lookup).

The naive approach — `redistribute static` + 10 k blackhole routes — couples
the two and bloats the main FIB. `georoute` keeps them strictly separated.

## The two planes

```
                       ┌─────────────────────────────┐
                       │  RIPE Stat country list     │
                       │  (HTTPS, idempotent fetch)  │
                       └──────────────┬──────────────┘
                                      ▼
                       ┌─────────────────────────────┐
                       │         georoute            │
                       │ aggregate → diff → apply    │
                       └──────────────┬──────────────┘
                            ┌─────────┴──────────┐
                            ▼                    ▼
              ┌──────────────────┐    ┌────────────────────┐
              │ /etc/frr/frr.conf │    │  nft inet pbr      │
              │  network X/Y      │    │  set ru_v4/ru_v6   │
              │  (between markers)│    │  (atomic replace)  │
              └──────────┬───────┘    └──────────┬─────────┘
                         ▼                       ▼
                ┌─────────────────┐    ┌───────────────────────┐
                │ frr-reload.py   │    │ mark + ip rule fwmark │
                │ → BGP advert    │    │ → lookup table 100    │
                │   to neighbors  │    │ → out local uplink    │
                └─────────────────┘    └───────────────────────┘
                CONTROL PLANE              DATA PLANE
```

The two outputs are independent on the wire but logically consistent because
they came from the same aggregation pass.

## Update ordering

Within a single run `georoute` applies in this order:

1. **nft first** (data plane). A new prefix entering the BGP RIB before nft
   knows about it would mean the peer can attract traffic for a destination
   we don't yet mark — and a missed mark falls through to the BGP-default
   route, which on a transit node is "back to the peer." That is a loop.
   Updating nft first closes the window.

2. **FRR second** (control plane). After `splice → frr-reload.py --reload`,
   peers see the new prefixes. By construction, our nft already knows them.

The reverse order is unsafe; do not change it without considering this window.

## Why not just static routes?

`redistribute static` with thousands of `ip route ... blackhole` entries
*works* but it:

- Pollutes the main FIB. Every connected service on the node now has 10 k
  extra routes to wade through (worsens cache footprint, slows `ip route get`).
- Conflates "what I want the peer to know" with "what I want the kernel to
  do." Two failure modes for one prefix list.
- Doesn't compose with policy routing: you can't say "VPN clients use the
  country list, my own SSH traffic goes direct." With nft+fwmark you can.

The nft interval set is tree-indexed: O(log n) match for 10 k entries is
indistinguishable from O(log n) match for 100. Memory is ~256 KB.

## Why `type route` for the OUTPUT chain

```nft
chain output {
    type route hook output priority mangle;
    ...
    ip daddr @ru_v4 meta mark set 0x201
}
```

`type route` tells the kernel: *if* this chain changes routing-relevant
metadata (mark, source, ToS), redo the routing decision. Without it, a
locally-generated packet keeps the routing decision made *before* the OUTPUT
chain ran, and the mark we just set is ignored. With it, the kernel re-evaluates
the FIB and our `ip rule fwmark 0x201 lookup 100` takes effect.

## VRF gotcha: double netfilter pass

When the input interface is a VRF slave (e.g. an ocserv `vpns0` enslaved into
`vrf-vpn`), netfilter sees the packet *twice*: once with `iif=vpns0` and once
with `iif=vrf-vpn`. Without protection we'd mark a packet twice, which is
mostly harmless but wastes work. The provided `pbr.nft` has

```
meta iifkind "vrf" return
```

at the top of the chain, so we don't process the VRF-master second pass.

## What's NOT in scope

- **Maintaining the country list.** `georoute` pulls from RIPE Stat. If you
  want a different source (BGP RIS, RIR delegated stats, antifilter.download,
  your own curated AS-SET), you fork the fetcher.
- **NAT.** Masquerade for client traffic is left to firewalld (zone-level)
  or whatever you use. `georoute` only does routing, not address translation.
- **Cross-VRF nexthops.** If you use Linux VRF, the route in table 100 should
  resolve in the same VRF, or you use `nexthop vrf` syntax. That's a deployment
  detail; `georoute` doesn't manage it.
- **More than one country per node.** Today we hard-code `RU` against RIPE
  Stat. Multi-country (or a different feed) is a planned change — see roadmap.

## Roadmap

- `--country` / `--source` flags to make the feed pluggable per node.
- `--peer-escape` to skip exact peer endpoints from the set (so we don't
  accidentally route the WG underlay back through itself).
- BGP large-community option for operators using AS plans that need it
  (RFC 8092).
- Pure netlink for nft + a vtysh-backed mode for FRR, to remove the dependency
  on the `frr-reload.py` script.
