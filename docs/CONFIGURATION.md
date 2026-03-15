# Configuration Reference

## Overview

Runtime configuration is JSON.

Two important rules now apply:

1. placeholder values are intentional and privacy-safe
2. client config generation is refused until required WireGuard values are explicit

## Top-Level Sections

- `api`
- `state`
- `wireguard`
- `dataplane`
- `dns`
- `policy`
- `static`

## `api`

### `api.listen`

- Type: string
- Example: `127.0.0.1:18080`
- Default: `127.0.0.1:18080`

This is now loopback-only by default. The management API is no longer exposed on all
interfaces unless the operator explicitly changes it.

### `api.token`

- Type: string
- Example: `change-me`

Bearer token for the HTTP API. Empty token disables auth middleware.

## `state`

### `state.path`

- Type: string
- Default: `/var/lib/vpngw/state.json`

Persistent JSON store for domains and generated clients.

## `wireguard`

### `wireguard.client_interface`

- Type: string
- Default: `wg-clients`

### `wireguard.uplink_interface`

- Type: string
- Default: `wg-uplink`

### `wireguard.server_endpoint`

- Type: string
- Example: `192.168.1.105:51820`
- Privacy-safe placeholder example: `REQUIRED_SERVER_ENDPOINT:51820`

Used when rendering downstream client configs. If left as a placeholder, `gen-client`
returns an explicit error.

### `wireguard.server_public_key`

- Type: string

Public key advertised to downstream clients.

In bootstrap mode this is patched from the actual `wg-clients` private key after the
interface config is written.

### `wireguard.client_cidr_v4`

- Type: string
- Default: `10.70.0.0/24`

Pool used for generated client addresses.

### `wireguard.client_dns`

- Type: string
- Default: `10.70.0.1`

This value also drives the default DNS listener bind address.

### `wireguard.mtu`

- Type: integer
- Default: `1420`

This is the client-side MTU rendered into downstream configs and used for `wg-clients`.

### `wireguard.keepalive_sec`

- Type: integer
- Default: `25`

## `dataplane`

### `dataplane.table_name`

- Type: string
- Default: `vpngw`

### `dataplane.fwmark_hex`

- Type: string
- Default: `0x66`

### `dataplane.route_table_id`

- Type: integer
- Default: `166`

### `dataplane.client_input_if`

- Type: string
- Default: same as `wireguard.client_interface`

### `dataplane.strict_on_uplink_down`

- Type: boolean

Currently carried in config for policy intent. The project mainly relies on dataplane
objects and service state rather than complex dynamic failover behavior.

## `dns`

### `dns.listen_udp`

- Type: string
- Default: `10.70.0.1:53`

### `dns.listen_tcp`

- Type: string
- Default: `10.70.0.1:53`

The DNS service now binds to the client-facing WireGuard address by default instead of
`0.0.0.0`.

### `dns.direct_upstream`

- Type: string
- Example: `192.168.1.1:53`
- Placeholder example: `REQUIRED_DIRECT_DNS:53`

This must be explicitly configured. Placeholder values are rejected by config loading.

### `dns.vpn_upstream`

- Type: string
- Example: `10.90.0.1:53`
- Placeholder example: `REQUIRED_VPN_DNS:53`

This must also be explicitly configured.

### `dns.query_timeout`

- Type: duration string
- Default: `2s`

Accepted formats are Go duration strings such as `500ms`, `2s`, `1m`.

## `policy`

### `policy.failure_threshold`

- Type: integer
- Default: `2`

How many failures are needed before a domain is promoted to forced VPN mode.

### `policy.pin_ttl`

- Type: duration string
- Default: `1h`

### `policy.ip_ttl`

- Type: duration string
- Default: `1h`

## `static`

### `static.ips_file`

- Type: string
- Default: `/root/proga/force_vpn_ips.txt`

### `static.domains_file`

- Type: string
- Default: `/root/proga/force_vpn_domains.txt`

### `static.resolve_interval`

- Type: duration string
- Default: `15m`

### `static.resolve_enabled`

- Type: boolean
- Default: `false`

If `false`, static domains are still loaded as forced domains but the service does not
perform background DNS resolution for them. This reduces unsolicited outbound DNS traffic.

If `true`, the service periodically resolves forced domains via `dns.vpn_upstream`.

## Placeholder Rules

The code treats values containing any of these markers as unresolved placeholders:

- `REQUIRED_`
- `YOUR_`
- `AUTO_SET_`
- `SET_ME`
- `__WG_`

Effects:

- `config.Load` rejects placeholder DNS upstreams
- `gen-client` rejects placeholder `server_endpoint` and `server_public_key`
- bootstrap/install skip autostart where placeholders are still present

## Minimal Privacy-Safe Staging Config

This config is intentionally not ready for outbound traffic:

```json
{
  "api": {
    "listen": "127.0.0.1:18080",
    "token": "change-me"
  },
  "state": {
    "path": "/var/lib/vpngw/state.json"
  },
  "wireguard": {
    "client_interface": "wg-clients",
    "uplink_interface": "wg-uplink",
    "server_endpoint": "REQUIRED_SERVER_ENDPOINT:51820",
    "server_public_key": "AUTO_SET_BY_INSTALLER",
    "client_cidr_v4": "10.70.0.0/24",
    "client_dns": "10.70.0.1",
    "mtu": 1420,
    "keepalive_sec": 25
  },
  "dataplane": {
    "table_name": "vpngw",
    "fwmark_hex": "0x66",
    "route_table_id": 166,
    "client_input_if": "wg-clients",
    "strict_on_uplink_down": true
  },
  "dns": {
    "listen_udp": "10.70.0.1:53",
    "listen_tcp": "10.70.0.1:53",
    "direct_upstream": "REQUIRED_DIRECT_DNS:53",
    "vpn_upstream": "REQUIRED_VPN_DNS:53",
    "query_timeout": "2s"
  },
  "policy": {
    "failure_threshold": 2,
    "pin_ttl": "1h",
    "ip_ttl": "1h"
  },
  "static": {
    "ips_file": "/root/proga/force_vpn_ips.txt",
    "domains_file": "/root/proga/force_vpn_domains.txt",
    "resolve_interval": "15m",
    "resolve_enabled": false
  }
}
```
