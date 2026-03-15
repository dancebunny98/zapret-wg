# Privacy And Network Model

## Goal

The local repository is now changed to avoid accidental network egress and accidental
service exposure during fresh installs.

This does not mean the product never uses the network. The product is a VPN gateway. Once
you explicitly configure upstream DNS and a WireGuard uplink, network traffic is expected.

The goal is narrower and practical:

- no embedded real remote VPN credentials in templates
- no embedded public DNS resolvers in templates
- no automatic package-manager traffic unless explicitly allowed
- no management API exposure on all interfaces by default
- no client DNS listener on all interfaces by default
- no periodic forced-domain DNS refresh unless explicitly enabled

## Inbound Surface

### Management API

Before:

- default bind: `0.0.0.0:18080`

Now:

- default bind: `127.0.0.1:18080`

Risk reduced:

- accidental exposure of management endpoints on LAN/WAN

### DNS Forwarder

Before:

- default bind: `0.0.0.0:53` on UDP and TCP

Now:

- default bind: `wireguard.client_dns:53`, usually `10.70.0.1:53`

Risk reduced:

- accidental exposure of a recursive forwarder on non-VPN interfaces

## Outbound Surface

### Dependency Installation

Before:

- bootstrap automatically used `apt`, `dnf`, `yum`, `pacman`, or `zypper` if dependencies
  were missing

Now:

- bootstrap refuses that by default
- operator must pass `-allow-net-install` to opt in

### Public DNS

Before:

- generated/example configs shipped with `1.1.1.1:53` and `9.9.9.9:53`

Now:

- generated/example configs ship with placeholders:
  - `REQUIRED_DIRECT_DNS:53`
  - `REQUIRED_VPN_DNS:53`

Result:

- a fresh install no longer contacts third-party resolvers by accident

### Remote Uplink

Before:

- templates shipped with a real endpoint, real public key, real PSK, and even a real local
  private key in some repo artifacts

Now:

- uplink configs use placeholders for peer parameters
- uplink private key is generated locally during bootstrap/install
- autostart of `wg-uplink` is skipped while placeholders remain

Result:

- no embedded remote VPN target remains in templates
- no accidental connection attempt to a hardcoded upstream peer

## Background Activity

### Static Domain Resolution

Before:

- the service proactively resolved forced domains on a timer

Now:

- `static.resolve_enabled` defaults to `false`

If you enable it, outbound DNS queries are expected and intentional.

## Placeholder Enforcement

The repository uses placeholders as an explicit privacy guardrail.

The code treats these markers as unresolved:

- `REQUIRED_`
- `YOUR_`
- `AUTO_SET_`
- `SET_ME`
- `__WG_`

Effects:

- bootstrap/install skip unsafe autostart paths
- config loading rejects placeholder DNS upstreams
- client generation rejects placeholder endpoint/public key values

## What Still Leaves The Host After Explicit Configuration

Once you replace placeholders and start the full stack, the following outbound traffic is
expected by design:

- package-manager traffic if you opted into `-allow-net-install`
- DNS queries to `dns.direct_upstream`
- DNS queries to `dns.vpn_upstream`
- WireGuard transport packets to the configured uplink endpoint
- application traffic routed by nftables into `wg-uplink`

That is not telemetry. That is the actual job of the gateway.

## Hardening Checklist

Use this checklist when preparing a new install:

1. Keep `api.listen` on `127.0.0.1:18080` unless remote API access is required.
2. Keep DNS binds on `10.70.0.1:53` or another client-only interface address.
3. Leave `static.resolve_enabled` as `false` unless proactive domain resolution is needed.
4. Fill `dns.direct_upstream` and `dns.vpn_upstream` with internal or explicitly approved
   resolvers.
5. Replace all `REQUIRED_*` placeholders in `/etc/wireguard/wg-uplink.conf`.
6. Replace `wireguard.server_endpoint` before generating downstream client configs.
7. Do not run bootstrap with `-allow-net-install` unless you explicitly want the host to
   reach package repositories.

## Operational Consequence

Fresh installs are now safer but less magical.

You must explicitly configure:

- upstream DNS
- remote uplink peer details
- client-facing endpoint

That tradeoff is deliberate.
