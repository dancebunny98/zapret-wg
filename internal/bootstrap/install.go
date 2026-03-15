package bootstrap

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"vpngw/internal/app"
	"vpngw/internal/config"
)

type Options struct {
	ConfigPath      string
	InstallDir      string
	ServicePath     string
	WGDir           string
	ServerEP        string
	CreateClient    int
	Force           bool
	AllowNetInstall bool
}

func Run(ctx context.Context, opts Options) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("run as root")
	}
	if err := ensureDependencies(ctx, opts.AllowNetInstall); err != nil {
		return err
	}
	if opts.ConfigPath == "" {
		opts.ConfigPath = "/root/proga/config.json"
	}
	if opts.InstallDir == "" {
		opts.InstallDir = "/root/proga"
	}
	if opts.ServicePath == "" {
		opts.ServicePath = "/etc/systemd/system/vpngw.service"
	}
	if opts.WGDir == "" {
		opts.WGDir = "/etc/wireguard"
	}
	if opts.CreateClient <= 0 {
		opts.CreateClient = 3
	}

	if err := os.MkdirAll(opts.InstallDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(opts.ConfigPath), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll("/var/lib/vpngw", 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(opts.InstallDir, "clients"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(opts.WGDir, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(opts.ServicePath), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll("/etc/systemd/system/wg-quick@wg-clients.service.d", 0o755); err != nil {
		return err
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dstBin := filepath.Join(opts.InstallDir, "vpngw")
	if filepath.Clean(exe) != filepath.Clean(dstBin) {
		if err := copyFile(exe, dstBin, 0o755); err != nil {
			return err
		}
	}

	wgUplink := defaultWGUplinkConf()
	wgUplinkPriv, err := runOut(ctx, "wg genkey")
	if err != nil {
		return err
	}
	wgUplink = strings.ReplaceAll(wgUplink, "__WG_UPLINK_PRIVATE_KEY__", strings.TrimSpace(wgUplinkPriv))
	if err := writeIfNeeded(filepath.Join(opts.WGDir, "wg-uplink.conf"), []byte(wgUplink), 0o600, opts.Force); err != nil {
		return err
	}

	wgClients := defaultWGClientsConf()
	wgPriv, err := runOut(ctx, "wg genkey")
	if err != nil {
		return err
	}
	wgClients = strings.ReplaceAll(wgClients, "__WG_CLIENTS_PRIVATE_KEY__", strings.TrimSpace(wgPriv))
	if err := writeIfNeeded(filepath.Join(opts.WGDir, "wg-clients.conf"), []byte(wgClients), 0o600, opts.Force); err != nil {
		return err
	}

	cfgBytes := []byte(defaultConfigJSON())
	if opts.ServerEP != "" {
		cfgBytes = []byte(strings.ReplaceAll(string(cfgBytes), "REQUIRED_SERVER_ENDPOINT:51820", opts.ServerEP))
	}
	if err := writeIfNeeded(opts.ConfigPath, cfgBytes, 0o600, opts.Force); err != nil {
		return err
	}
	if err := writeIfNeeded(filepath.Join(opts.InstallDir, "force_vpn_ips.txt"), []byte("# one IP or CIDR per line\n# example:\n# 104.26.12.8\n# 1.1.1.0/24\n"), 0o600, opts.Force); err != nil {
		return err
	}
	if err := writeIfNeeded(filepath.Join(opts.InstallDir, "force_vpn_domains.txt"), []byte("# one domain per line\n# example:\n# modrinth.com\n"), 0o600, opts.Force); err != nil {
		return err
	}

	if err := writeIfNeeded(opts.ServicePath, []byte(defaultServiceFile(opts.InstallDir, opts.ConfigPath)), 0o644, opts.Force); err != nil {
		return err
	}
	if err := writeIfNeeded(filepath.Join(opts.InstallDir, "sync_peers.sh"), []byte(defaultSyncPeersScript()), 0o755, opts.Force); err != nil {
		return err
	}
	if err := writeIfNeeded("/etc/systemd/system/wg-quick@wg-clients.service.d/10-vpngw-sync.conf", []byte(defaultWGClientsSyncDropIn(opts.InstallDir)), 0o644, opts.Force); err != nil {
		return err
	}

	wgClientsPriv, err := readWGPrivateKey(filepath.Join(opts.WGDir, "wg-clients.conf"))
	if err != nil {
		return err
	}
	wgClientsPub, err := wgPubFromPriv(ctx, wgClientsPriv)
	if err != nil {
		return err
	}
	if err := patchServerPubKey(opts.ConfigPath, strings.TrimSpace(wgClientsPub), opts.ServerEP); err != nil {
		return err
	}

	if err := os.WriteFile("/etc/sysctl.d/99-vpngw.conf", []byte(defaultSysctlForwardingConf()), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile("/etc/sysctl.d/99-vpngw-tune.conf", []byte(defaultSysctlTuningConf(supportsBBR(ctx))), 0o644); err != nil {
		return err
	}
	cfg, cfgErr := config.Load(opts.ConfigPath)
	wgUplinkBytes, err := os.ReadFile(filepath.Join(opts.WGDir, "wg-uplink.conf"))
	if err != nil {
		return err
	}
	wgUplinkReady := !config.IsPlaceholderValue(string(wgUplinkBytes))
	for _, c := range []string{
		"sysctl --system >/dev/null",
		"systemctl daemon-reload",
		"systemctl enable --now wg-quick@wg-clients",
		"ip link set dev wg-clients txqueuelen 1000 || true",
	} {
		if err := run(ctx, c); err != nil {
			return err
		}
	}

	if wgUplinkReady {
		for _, c := range []string{
			"systemctl enable --now wg-quick@wg-uplink",
			"ip link set dev wg-uplink txqueuelen 1000 || true",
		} {
			if err := run(ctx, c); err != nil {
				return err
			}
		}
	} else {
		fmt.Fprintf(os.Stderr, "bootstrap note: wg-uplink has unresolved placeholders; skipped wg-uplink autostart\n")
	}

	if cfgErr == nil && wgUplinkReady {
		if err := run(ctx, "systemctl enable --now vpngw"); err != nil {
			return err
		}
	} else if cfgErr != nil {
		fmt.Fprintf(os.Stderr, "bootstrap note: config not ready for vpngw autostart: %v\n", cfgErr)
	}

	if cfgErr != nil || config.IsPlaceholderValue(cfg.WireGuard.ServerEndpoint) {
		fmt.Fprintf(os.Stderr, "bootstrap note: skipped client generation until wireguard.server_endpoint and DNS upstreams are explicitly configured\n")
		return nil
	}

	srv, err := app.NewServer(cfg)
	if err != nil {
		return err
	}
	for i := 1; i <= opts.CreateClient; i++ {
		cl, conf, err := srv.CreateClient(ctx, fmt.Sprintf("router-%d", i))
		if err != nil {
			return err
		}
		p := filepath.Join(opts.InstallDir, "clients", fmt.Sprintf("router-%d.conf", i))
		if err := os.WriteFile(p, []byte(conf), 0o600); err != nil {
			return err
		}
		_ = cl
	}
	return nil
}

func readWGPrivateKey(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "PrivateKey") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[1])
		if key != "" {
			return key, nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("PrivateKey not found in %s", path)
}

func wgPubFromPriv(ctx context.Context, priv string) (string, error) {
	c := exec.CommandContext(ctx, "wg", "pubkey")
	c.Stdin = strings.NewReader(strings.TrimSpace(priv) + "\n")
	out, err := c.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("wg pubkey: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, mode)
}

func writeIfNeeded(path string, b []byte, mode os.FileMode, force bool) error {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
	}
	return os.WriteFile(path, b, mode)
}

func patchServerPubKey(configPath, pub, endpoint string) error {
	b, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	wg, _ := v["wireguard"].(map[string]any)
	if wg == nil {
		wg = map[string]any{}
		v["wireguard"] = wg
	}
	wg["server_public_key"] = pub
	if strings.TrimSpace(endpoint) != "" {
		wg["server_endpoint"] = endpoint
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, out, 0o600)
}

func run(ctx context.Context, line string) error {
	c := exec.CommandContext(ctx, "bash", "-lc", line)
	out, err := c.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", line, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func runOut(ctx context.Context, line string) (string, error) {
	c := exec.CommandContext(ctx, "bash", "-lc", line)
	out, err := c.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %w: %s", line, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func ensureDependencies(ctx context.Context, allowNetInstall bool) error {
	requiredBins := []string{"wg", "wg-quick", "nft", "ip", "python3", "systemctl", "bash", "resolvconf"}
	missing := make([]string, 0)
	for _, b := range requiredBins {
		if _, err := exec.LookPath(b); err != nil {
			missing = append(missing, b)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	if !allowNetInstall {
		return fmt.Errorf("missing required binaries: %s; automatic package-manager install is disabled by default, install them manually or rerun bootstrap with -allow-net-install", strings.Join(missing, ", "))
	}

	type manager struct {
		bin      string
		commands []string
	}
	managers := []manager{
		{
			bin: "apt-get",
			commands: []string{
				"DEBIAN_FRONTEND=noninteractive apt-get update -y",
				"DEBIAN_FRONTEND=noninteractive apt-get install -y wireguard-tools nftables iproute2 gawk python3 systemd resolvconf",
			},
		},
		{
			bin: "dnf",
			commands: []string{
				"dnf install -y wireguard-tools nftables iproute gawk python3 systemd openresolv",
			},
		},
		{
			bin: "yum",
			commands: []string{
				"yum install -y wireguard-tools nftables iproute gawk python3 systemd openresolv",
			},
		},
		{
			bin: "pacman",
			commands: []string{
				"pacman -Sy --noconfirm wireguard-tools nftables iproute2 gawk python systemd openresolv",
			},
		},
		{
			bin: "zypper",
			commands: []string{
				"zypper --non-interactive install --no-confirm wireguard-tools nftables iproute2 gawk python3 systemd openresolv",
			},
		},
	}

	var selected *manager
	for i := range managers {
		if _, err := exec.LookPath(managers[i].bin); err == nil {
			selected = &managers[i]
			break
		}
	}
	if selected == nil {
		return fmt.Errorf("missing required binaries: %s; unsupported package manager", strings.Join(missing, ", "))
	}

	for _, c := range selected.commands {
		if err := run(ctx, c); err != nil {
			return fmt.Errorf("auto-install dependencies failed via %s: %w", selected.bin, err)
		}
	}

	// Recheck after install.
	stillMissing := make([]string, 0)
	for _, b := range requiredBins {
		if _, err := exec.LookPath(b); err != nil {
			stillMissing = append(stillMissing, b)
		}
	}
	if len(stillMissing) > 0 {
		return fmt.Errorf("dependencies still missing after install: %s", strings.Join(stillMissing, ", "))
	}
	return nil
}

func defaultServiceFile(installDir, configPath string) string {
	return fmt.Sprintf(`[Unit]
Description=vpngw selective WireGuard gateway
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=%s
ExecStart=%s/vpngw server -config %s
Restart=always
RestartSec=2
User=root
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_BIND_SERVICE
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
`, installDir, installDir, configPath)
}

func defaultConfigJSON() string {
	return `{
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
}`
}

func defaultWGUplinkConf() string {
	return `[Interface]
Address = 10.90.0.5/32, fd42:42:90::5/128
PrivateKey = __WG_UPLINK_PRIVATE_KEY__
MTU = 1380
PostUp = ip link set dev %i txqueuelen 1000

[Peer]
Endpoint = REQUIRED_UPLINK_ENDPOINT:51820
PublicKey = REQUIRED_UPLINK_PUBLIC_KEY
PresharedKey = REQUIRED_UPLINK_PRESHARED_KEY
AllowedIPs = 0.0.0.0/0, ::/0
PersistentKeepalive = 25
`
}

func defaultWGClientsConf() string {
	return `[Interface]
Address = 10.70.0.1/24, fd42:42:70::1/64
ListenPort = 51820
PrivateKey = __WG_CLIENTS_PRIVATE_KEY__
PostUp = ip link set dev %i txqueuelen 1000
`
}

func defaultSysctlForwardingConf() string {
	return "net.ipv4.ip_forward=1\nnet.ipv6.conf.all.forwarding=1\n"
}

func defaultSyncPeersScript() string {
	return `#!/usr/bin/env bash
set -euo pipefail
[[ -f /var/lib/vpngw/state.json ]] || exit 0
python3 - <<'PY'
import json,subprocess,tempfile,os
p='/var/lib/vpngw/state.json'
with open(p,'r',encoding='utf-8') as f:
    s=json.load(f)
for c in s.get('clients',{}).values():
    pub=c['public_key'].strip()
    psk=c['preshared'].strip()
    ip=c['ipv4'].strip()+'/32'
    fd,tmp=tempfile.mkstemp(prefix='wgpsk-')
    os.write(fd,(psk+'\n').encode())
    os.close(fd)
    subprocess.check_call(['wg','set','wg-clients','peer',pub,'preshared-key',tmp,'allowed-ips',ip])
    os.unlink(tmp)
print('peers synced')
PY
wg show wg-clients
`
}

func defaultWGClientsSyncDropIn(installDir string) string {
	return fmt.Sprintf("[Service]\nExecStartPost=/bin/bash -lc '%s/sync_peers.sh'\n", installDir)
}

func defaultSysctlTuningConf(enableBBR bool) string {
	lines := []string{
		"net.core.default_qdisc=fq",
		"net.core.rmem_max=67108864",
		"net.core.wmem_max=67108864",
		"net.core.rmem_default=262144",
		"net.core.wmem_default=262144",
		"net.core.optmem_max=4194304",
		"net.core.netdev_max_backlog=16384",
		"net.ipv4.udp_rmem_min=16384",
		"net.ipv4.udp_wmem_min=16384",
		"net.ipv4.tcp_mtu_probing=1",
	}
	if enableBBR {
		lines = append(lines, "net.ipv4.tcp_congestion_control=bbr")
	}
	return strings.Join(lines, "\n") + "\n"
}

func supportsBBR(ctx context.Context) bool {
	out, err := runOut(ctx, "sysctl -n net.ipv4.tcp_available_congestion_control 2>/dev/null || cat /proc/sys/net/ipv4/tcp_available_congestion_control 2>/dev/null")
	if err != nil {
		return false
	}
	for _, name := range strings.Fields(out) {
		if name == "bbr" {
			return true
		}
	}
	return false
}
