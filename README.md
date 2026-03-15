# vpngw

`vpngw` is a selective WireGuard gateway for Linux.

It accepts downstream clients on `wg-clients`, keeps the normal default route on the host,
and sends only selected destinations through `wg-uplink` using nftables marks and policy
routing.

## Privacy-First Defaults

The local project is now intentionally conservative.

Fresh templates no longer ship with:

- real remote uplink endpoint values
- real uplink keys or PSKs
- public DNS upstream defaults
- management API bind on `0.0.0.0`
- DNS listener bind on `0.0.0.0`
- automatic package-manager traffic during bootstrap unless explicitly allowed

This means a fresh install is safer, but it also means you must replace placeholders before
the full stack is allowed to autostart.

## Documentation

Detailed documentation lives in `docs/`:

- [Architecture](docs/ARCHITECTURE.md)
- [Configuration Reference](docs/CONFIGURATION.md)
- [Privacy And Network Model](docs/PRIVACY_AND_NETWORK.md)
- [Operations](docs/OPERATIONS.md)
- [Repository Layout](docs/REPOSITORY_LAYOUT.md)

## Build

```bash
go build ./...
```

This command validates all packages without dropping disposable binaries into the project
root.

## Bootstrap

Privacy-safe bootstrap:

```bash
./vpngw bootstrap -clients 3
```

Bootstrap with explicit permission to contact package repositories if dependencies are
missing:

```bash
./vpngw bootstrap -clients 3 -allow-net-install
```

Behavior summary:

- `wg-clients` is prepared locally
- `wg-uplink` is written with placeholders
- `vpngw` autostart is skipped until DNS placeholders are replaced
- downstream client generation is skipped until DNS placeholders are replaced and
  `wireguard.server_endpoint` is explicit

## Manual Configuration Required

Replace placeholders in:

- `config.json`
- `wg-uplink.conf`

At minimum you must explicitly set:

- `dns.direct_upstream`
- `dns.vpn_upstream`
- `wireguard.server_endpoint`
- `wg-uplink` peer endpoint, public key, and preshared key

## Generate A Client

After configuration is complete:

```bash
vpngw gen-client -config /etc/vpngw/config.json -name router-kitchen
```

## Existing Install Tuning

If you already have a running install and only want the host-side throughput tuning:

```bash
bash dist/apply_perf_fix.sh
```

## Repository Notes

- `deploy/` contains source templates and scripts
- `dist/` contains release-oriented artifacts staged inside the repo and should be kept intact
- `docs/` contains the long-form operator and architecture documentation
- project root should not keep ad-hoc SDK archives or one-off binaries; disposable outputs
  belong outside the root tree
