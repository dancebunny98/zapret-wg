# Architecture

## Purpose

`vpngw` is a selective routing gateway.

The project keeps the normal default route on the local server, but it can pin specific
domains and IPs to a WireGuard uplink when direct connectivity fails or when an operator
declares that traffic must go through VPN.

The design splits the system into:

- a local client-facing WireGuard interface: `wg-clients`
- an upstream WireGuard egress interface: `wg-uplink`
- a DNS forwarder that decides when to fall back to VPN DNS
- an nftables + policy-routing dataplane
- a persistent state store for domains and generated clients

## Main Components

### `cmd/vpngw`

The binary exposes three commands:

- `server`: runs the DNS/API/dataplane process
- `gen-client`: generates a new WireGuard client peer and router-ready config
- `bootstrap`: writes files, optionally installs dependencies, and prepares a host

### `internal/app`

`internal/app/server.go` wires together storage, dataplane, DNS forwarding, policy logic,
and WireGuard client management.

Startup order is:

1. load persistent state
2. ensure nftables/ip rule base objects exist
3. restore pinned IPs from state
4. load static lists from disk
5. restore client peers into `wg-clients`
6. start the DNS forwarder
7. start the HTTP API

### `internal/dnsfw`

The DNS server accepts client queries and applies this decision flow:

1. If the queried domain is already forced to VPN, ask `vpn_upstream`.
2. Otherwise, ask `direct_upstream`.
3. If direct resolution fails, ask `vpn_upstream`.
4. If VPN DNS returns A/AAAA answers, feed those IPs into policy state so they can be
   pinned to VPN.

The DNS service is client-facing only. Privacy-safe defaults now bind it to
`wireguard.client_dns:53` instead of `0.0.0.0:53`.

### `internal/policy`

The policy engine tracks failures and decides when a domain should move from direct mode to
forced VPN mode. It persists decisions into the state store and asks the dataplane manager
to add IPs into nftables sets with TTLs.

### `internal/dataplane`

The dataplane manager owns:

- nftables sets for dynamic VPN targets
- nftables sets for operator-defined static targets
- mark rules in `prerouting` and `output`
- an auxiliary routing table selected by `fwmark`
- source NAT for client subnet egress
- MSS clamp rules for `wg-uplink` and `wg-clients`

The dataplane does not proxy payloads in userspace. It programs kernel networking objects
and then gets out of the way.

### `internal/wg`

The WireGuard manager creates end-client peers on `wg-clients`.

For every generated client it:

1. creates private/public keys
2. creates a preshared key
3. allocates the next free IPv4 from `wireguard.client_cidr_v4`
4. persists the client record into state
5. installs the peer into the live `wg-clients` interface
6. renders a client config

On server start, previously generated peers are now restored from persistent state. This
fix avoids the old failure mode where `wg-clients` came up empty after a restart.

### `internal/storage`

The state file persists:

- domain routing mode and TTL information
- generated client peers

The storage layer is intentionally simple: a single JSON file written atomically via
temporary-file rename.

## Interfaces

### `wg-clients`

Purpose:

- receives connections from local routers/clients
- carries client-originated traffic into the gateway

Properties:

- server-side interface and peers are managed locally
- DNS for clients usually points to `10.70.0.1`
- privacy-safe template does not expose this DNS service on all interfaces

### `wg-uplink`

Purpose:

- carries selectively routed traffic to the remote VPN provider/server

Properties:

- no real endpoint, key, or PSK is shipped in templates anymore
- autostart is skipped until placeholders are replaced
- its MTU is intentionally independent from the client-side MTU

## Data Flow

### DNS-Driven Promotion Flow

1. client asks local DNS forwarder
2. direct DNS is tried first
3. if direct DNS fails, VPN DNS is tried
4. returned IPs are observed by policy
5. policy adds those IPs into nftables VPN target sets
6. matching traffic is marked and routed through `wg-uplink`

### Static Rule Flow

Operators can place IPs/CIDRs or domains into static files.

IP/CIDR entries go straight into static nftables sets.

Domain entries are stored as forced domains. If `static.resolve_enabled` is true, the
server proactively resolves them on a timer. If it is false, no periodic outbound DNS
queries are generated for those domains.

## Restart Semantics

After the recent local changes, restart behavior is:

- `wg-clients` can be brought up independently
- a systemd drop-in calls `sync_peers.sh` after `wg-clients` starts
- `vpngw server` also restores peers from state during startup
- pinned destination IPs are restored into nftables

This gives two independent recovery paths for client peers instead of one.

## Files Written By Bootstrap

Typical bootstrap output:

- install directory with binary, config, helper scripts, and generated clients
- `/etc/wireguard/wg-clients.conf`
- `/etc/wireguard/wg-uplink.conf`
- `/etc/systemd/system/vpngw.service`
- `/etc/systemd/system/wg-quick@wg-clients.service.d/10-vpngw-sync.conf`
- `/etc/sysctl.d/99-vpngw.conf`
- `/etc/sysctl.d/99-vpngw-tune.conf`
- `/var/lib/vpngw/state.json`

## Architectural Boundaries

What the project does:

- controls routing policy
- generates WireGuard peers for downstream clients
- forwards DNS with VPN fallback
- persists local routing and peer state

What the project does not do:

- ship telemetry
- manage a remote VPN provider account
- provision remote upstream peers automatically
- auto-discover safe public DNS servers
- hide the fact that DNS and VPN egress are network actions once explicitly configured
