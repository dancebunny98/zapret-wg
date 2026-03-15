# Operations

## Build

Linux build:

```bash
go build -o vpngw ./cmd/vpngw
```

## Privacy-First Bootstrap

Bootstrap now prioritizes safety over immediate connectivity.

Command:

```bash
./vpngw bootstrap -clients 3
```

What it does:

- writes binary and config files
- writes `wg-clients.conf`
- writes `wg-uplink.conf` with placeholders
- writes systemd units and helper scripts
- enables forwarding sysctls
- starts `wg-clients`

What it does not do by default:

- does not auto-install missing packages
- does not autostart `wg-uplink` when placeholders remain
- does not autostart `vpngw` when DNS upstream placeholders remain
- does not generate downstream client configs until DNS placeholders are replaced and
  `wireguard.server_endpoint` is explicit

## Allowing Package Manager Traffic

If and only if you want bootstrap to contact package repositories:

```bash
./vpngw bootstrap -clients 3 -allow-net-install
```

Without that flag, missing dependencies are reported as an error.

## Manual Uplink Preparation

Edit `/etc/wireguard/wg-uplink.conf` and replace:

- `REQUIRED_UPLINK_ENDPOINT:51820`
- `REQUIRED_UPLINK_PUBLIC_KEY`
- `REQUIRED_UPLINK_PRESHARED_KEY`

The uplink private key is generated locally during bootstrap/install. It is not shipped in
the repository anymore.

## Manual Runtime Configuration

Edit `config.json` and replace:

- `REQUIRED_DIRECT_DNS:53`
- `REQUIRED_VPN_DNS:53`
- `REQUIRED_SERVER_ENDPOINT:51820`

Recommended order:

1. set DNS upstreams
2. set the public/local endpoint clients must dial
3. verify `server_public_key` was patched from `wg-clients`

## Starting Services After Configuration

Once placeholders are replaced:

```bash
systemctl daemon-reload
systemctl enable --now wg-quick@wg-uplink
systemctl enable --now wg-quick@wg-clients
systemctl enable --now vpngw
```

## Release Install Script

`deploy/scripts/install_root.sh` follows the same privacy-first behavior:

- it always prepares local files
- it always starts `wg-clients`
- it only starts `wg-uplink` if its config has no placeholders
- it only starts `vpngw` if DNS upstreams are explicit
- it only generates client configs if DNS upstreams are explicit and
  `wireguard.server_endpoint` is explicit

## Client Generation

Generate a client after DNS upstreams and the endpoint are configured:

```bash
vpngw gen-client -config /etc/vpngw/config.json -name router-kitchen
```

Failure cases are explicit now:

- placeholder DNS upstreams
- placeholder `server_endpoint`
- placeholder `server_public_key`

## Peer Recovery

Two recovery paths exist for `wg-clients` peers:

1. `vpngw server` restores peers from `state.json` during startup
2. `wg-quick@wg-clients` runs `sync_peers.sh` in `ExecStartPost`

This means:

- restarting the service keeps downstream peers
- restarting only `wg-clients` also keeps downstream peers

## Performance Tuning

The project ships baseline host tuning:

- `fq` qdisc
- enlarged socket buffers
- `tcp_mtu_probing=1`
- `bbr` when available
- `txqueuelen 1000`
- nftables MSS clamp on `wg-uplink` and `wg-clients`

Existing install helper:

```bash
bash dist/apply_perf_fix.sh
```

## Troubleshooting

### Check placeholders still present

```bash
grep -REn 'REQUIRED_|YOUR_|SET_ME|__WG_' /etc/wireguard /etc/vpngw /root/proga
```

### Check interfaces

```bash
ip -br addr
wg show
```

### Check services

```bash
systemctl --no-pager --full status wg-quick@wg-clients
systemctl --no-pager --full status wg-quick@wg-uplink
systemctl --no-pager --full status vpngw
```

### Check peer restore

```bash
cat /var/lib/vpngw/state.json
/root/proga/sync_peers.sh
wg show wg-clients
```

### Check DNS bind addresses

```bash
ss -lntup | grep ':53'
ss -lntup | grep ':18080'
```

## Recommended Rollout Sequence

For a new host:

1. bootstrap locally and safely
2. fill all placeholders
3. start `wg-clients`
4. start `wg-uplink`
5. start `vpngw`
6. generate client configs
7. test DNS and a VPN-routed destination
