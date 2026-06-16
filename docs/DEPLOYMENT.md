# Deployment

How to install `georoute` on an exit node from a clean slate.

## Prerequisites

- Linux, kernel ≥ 5.10 (we test on 6.12 LTS, UEK R8).
- nftables (with firewalld or stand-alone — both work; the `inet pbr` table
  lives independently of firewalld's `inet firewalld`).
- FRR ≥ 10.1, integrated `frr.conf`, `bgpd` enabled.
- Go ≥ 1.26 only for building from source; the released binary is static.
- A pre-existing BGP peering with whichever node should receive the country
  prefixes (mesh peer, route reflector, upstream — we don't care, just that
  the route-map is wired up).

## Step 1 — Install the binary

From a release tarball:

```bash
curl -fsSLO https://github.com/dantte-lp/georoute/releases/latest/download/georoute-linux-amd64
sudo install -m 0755 georoute-linux-amd64 /usr/local/bin/georoute
georoute --dry-run
```

From source:

```bash
git clone https://github.com/dantte-lp/georoute.git
cd georoute
make install   # builds, installs to /usr/local/bin/georoute
```

## Step 2 — Add the nftables scaffold

Copy the file once; subsequent edits to its contents are operator-side,
`georoute` never rewrites this file.

```bash
sudo install -d /etc/nft.d
sudo install -m 0644 deploy/nftables/pbr.nft /etc/nft.d/pbr.nft
sudo nft -f /etc/nft.d/pbr.nft
sudo nft list table inet pbr   # smoke-test
```

You probably also want a systemd unit to load it on boot:

```ini
# /etc/systemd/system/nft-pbr.service
[Unit]
Description=Load policy-routing nftables scaffolding for georoute
Before=network-pre.target
DefaultDependencies=no

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/sbin/nft -f /etc/nft.d/pbr.nft
ExecStop=/usr/sbin/nft delete table inet pbr

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now nft-pbr.service
```

## Step 3 — Add the `ip rule` + table 100

These are deployment-specific (which uplink, which gateway):

```bash
sudo ip -4 rule add fwmark 0x201 lookup 100 priority 100
sudo ip -6 rule add fwmark 0x201 lookup 100 priority 100
sudo ip -4 route add default via 91.218.113.129 dev ens1 table 100
sudo ip -6 route add default dev sit1 table 100
```

Persist via systemd (one-shot unit pinned `After=network-online.target`).
See [examples/pbr-ru-exit.service](../deploy/systemd/) for the pattern.

## Step 4 — Edit `frr.conf`

Open `/etc/frr/frr.conf` and:

1. Add the community-list, route-map, and out-bound route-map permits from
   [examples/frr-snippet.conf](../deploy/examples/frr-snippet.conf).
2. Add the two pairs of marker comments inside the `address-family ipv4
   unicast` and `ipv6 unicast` blocks. Indentation = two spaces, same as
   surrounding `network` lines.

Reload FRR once (verify the route-map is recognized):

```bash
sudo /usr/lib/frr/frr-reload.py --test /etc/frr/frr.conf
sudo /usr/lib/frr/frr-reload.py --reload /etc/frr/frr.conf
sudo vtysh -c 'show route-map MARK-RU-EXIT'
```

## Step 5 — First feed run (dry-run, then real)

```bash
sudo georoute --dry-run
sudo georoute
```

After the real run:

```bash
nft list set inet pbr ru_v4 | head
nft list set inet pbr ru_v6 | head
vtysh -c 'show bgp ipv4 unicast | grep "Total"'
ip route show table 100
```

The `inet pbr` sets should have thousands of elements; the FRR table version
should have advanced; table 100 should still hold only your local default.

## Step 6 — Install the systemd timer

```bash
sudo make install-systemd
systemctl list-timers georoute.timer
journalctl -u georoute.service -n 50
```

The timer fires at `OnBootSec=5min` and then every `12h` with up to
`30min` of random jitter (see [deploy/systemd/georoute.timer](../deploy/systemd/georoute.timer)).

## Verifying

A correctly-deployed `georoute` should produce:

- `nft list table inet pbr` — non-empty `ru_v4` and `ru_v6` sets.
- `vtysh -c 'show bgp ... summary'` — peer up, `PfxSnt` reflects the feed.
- For traffic from a VPN client to a country IP: `ip route get <ip>` shows the
  expected local table 100 nexthop *only when run with* `mark 0x201`. Without
  the mark, the lookup falls through to the main FIB (BGP default or whatever
  you have there).
- `journalctl -u georoute.service` shows hashes that change only when the
  upstream feed changes.

## Privileges

`georoute` needs:

- HTTPS to `stat.ripe.net`. No special caps for this.
- Write `/etc/frr/frr.conf` (root or member of `frr` group with `0640`).
- Exec `/usr/lib/frr/frr-reload.py` (root).
- `nft -f -` (CAP_NET_ADMIN).

The bundled systemd unit runs the service as `root`, sandboxed with
`NoNewPrivileges`, `ProtectSystem=strict`, and a tight capability set
(`CAP_NET_ADMIN`, `CAP_NET_RAW`). Run as a less-privileged user requires
delegating those capabilities and write access to `/etc/frr` — possible but
not the default.
