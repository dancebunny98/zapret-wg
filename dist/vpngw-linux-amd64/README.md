# vpngw Release Bundle

This bundle is privacy-safe by default.

It does not ship with a real remote uplink endpoint, real uplink peer credentials, or
public DNS upstream defaults. You must replace placeholders before the full stack is
allowed to autostart.

## Bundle Contents

- `vpngw` is the Linux binary.
- `config.json` is the runtime config template.
- `install.sh` installs the bundle into `/opt/vpngw`.
- `deploy/` contains the staged WireGuard templates and install helper script.
- `vpngw.service` is the staged systemd service unit.

This directory is a release bundle, not the primary source tree. Source edits should be
made in the repository and then propagated into `dist/`.

## Install

```bash
chmod +x install.sh
./install.sh
```

What happens immediately:

- files are copied into place
- `wg-clients` can be started locally
- host sysctls and queue tuning are applied

What is intentionally skipped while placeholders remain:

- `wg-uplink` autostart
- `vpngw` autostart
- downstream client generation

## Required Placeholder Replacement

Edit:

- `/opt/vpngw/config.json`
- `/etc/wireguard/wg-uplink.conf`

Replace:

- `REQUIRED_SERVER_ENDPOINT:51820`
- `REQUIRED_DIRECT_DNS:53`
- `REQUIRED_VPN_DNS:53`
- `REQUIRED_UPLINK_ENDPOINT:51820`
- `REQUIRED_UPLINK_PUBLIC_KEY`
- `REQUIRED_UPLINK_PRESHARED_KEY`

## Start Services

After replacing placeholders:

```bash
systemctl daemon-reload
systemctl enable --now wg-quick@wg-uplink
systemctl enable --now wg-quick@wg-clients
systemctl enable --now vpngw
```

## Generate Clients

After DNS upstreams and `wireguard.server_endpoint` are set:

```bash
/opt/vpngw/vpngw gen-client -config /opt/vpngw/config.json -name router-kitchen
```

## Cleanup Rule

Keep this bundle and the archives in `dist/`. Root-level disposable binaries and downloaded
SDK archives are cleanup targets, but `dist/` is the preserved release area.
