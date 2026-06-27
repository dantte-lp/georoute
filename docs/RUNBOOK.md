# Runbook — day-2 operations

Procedures for the operator on call.

## "BGP peer not getting the country prefixes"

Verify the chain end-to-end.

```bash
# 1. Sanity — did georoute actually run today?
systemctl list-timers georoute.timer
journalctl -u georoute.service -n 20

# 2. Did it write the block?
grep -c "BEGIN-RU-FEED-V4" /etc/frr/frr.conf       # should print 1
sed -n '/BEGIN-RU-FEED-V4/,/END-RU-FEED-V4/p' /etc/frr/frr.conf | head

# 3. Is FRR holding the network statements?
vtysh -c 'show running-config' | grep -c 'route-map MARK-RU-EXIT'

# 4. Does BGP have them in the local table?
vtysh -c 'show bgp ipv4 unicast | grep -c "0.0.0.0"'    # ≫0 expected

# 5. Is the peer's outbound route-map letting them out?
vtysh -c 'show bgp neighbor <peer-ip> advertised-routes | head'
```

If step 4 is fine but step 5 is empty: outbound route-map (`TO-PEER`) on the
neighbor doesn't permit the community. Edit, reload.

## "VPN client traffic to a country IP isn't using the local exit"

```bash
# 1. Is the address in the set?
nft get element inet pbr ru_v4 { 198.51.100.10 }   # should not error

# 2. Does the routing decision use the mark?
ip -4 route get 198.51.100.10 mark 0x201           # should show table 100

# 3. Is the mark actually being set in practice? (Add a temporary counter.)
nft 'add rule inet pbr prerouting ip daddr 198.51.100.10 counter comment trace'
# ... generate traffic ...
nft list chain inet pbr prerouting | grep trace
nft -a list chain inet pbr prerouting    # find the handle
nft 'delete rule inet pbr prerouting handle <N>'
```

If step 3 shows zero hits: the chain isn't seeing the traffic. Common causes:

- **VRF double-pass.** Confirm `meta iifkind "vrf" return` is present. If the
  packet ingress is a VRF-master device, our chain RETURNs and the underlying
  zone-aware ruleset processes the packet — but the VRF master also has to be
  in a permissive firewalld zone (e.g. `trusted`).
- **Reply traffic.** `ct direction reply accept` deliberately exempts the
  reverse direction; that's correct. If you want to mark the reverse path too
  (rarely), move the `ip daddr @ru_v4 ...` line *above* the `ct direction`
  filter.

## "I want to force a re-apply right now"

```bash
sudo georoute --force
```

This re-renders even if the BGP block hash is unchanged, rewrites `frr.conf`,
re-runs `frr-reload.py`, and re-issues the nft transaction.

## "I'm staging a change and don't want a reload yet"

```bash
sudo georoute --reload=false      # writes frr.conf, applies nft, no reload
# ... do your other changes ...
sudo /usr/lib/frr/frr-reload.py --reload /etc/frr/frr.conf
```

## "The feed source is wrong / I want to lie about country membership"

Insert a manual `static` route into FRR with the same community used by
`MARK-RU-EXIT`, and a kernel route into table 100. Don't edit georoute's
block — your hand-edit will survive the next run only if the diff matches
exactly. Out-of-band exceptions belong outside the markers.

## "I want to drop the country exit entirely (temporary)"

Easiest: stop the timer and flush the sets and the marker block.

```bash
sudo systemctl disable --now georoute.timer
sudo nft flush set inet pbr ru_v4
sudo nft flush set inet pbr ru_v6
# In frr.conf, manually remove all `  network ... route-map MARK-RU-EXIT`
# lines between the markers (leave the markers themselves).
sudo /usr/lib/frr/frr-reload.py --reload /etc/frr/frr.conf
```

Re-enable: `systemctl enable --now georoute.timer && georoute --force`.

## "Upstream feed is rate-limiting me"

RIPE Stat is liberal but not unlimited. A 12 h schedule with a 30-minute
jitter is well within the budget. If you really need it more often, mirror
the data locally and point at the mirror (a feature flag will land for this;
until then, edit `ripeURL` in `main.go` and rebuild).

## "I see a sudden jump in `PfxSnt`"

Either RIPE updated their dataset (allocations and reallocations are
non-trivial in volume), or someone deleted a `network` directive elsewhere
in `router bgp`. Diff the current `frr.conf` against the committed copy.

## Logs and where to look

| Component | Where |
|---|---|
| `georoute` runs | `journalctl -u georoute.service` |
| `frr-reload` output | included in the above |
| zebra/bgpd warnings | `journalctl -u frr` |
| nft events | none by default — add `nft 'add rule inet pbr prerouting ... log prefix "georoute: " level info'` temporarily |
| Outbound BGP UPDATE | `vtysh -c 'debug bgp updates'` (chatty — disable when done) |
