package wg

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os/exec"
	"sort"
	"strings"
	"time"

	"vpngw/internal/config"
	"vpngw/internal/storage"
)

type Manager struct {
	cfg   config.Config
	store *storage.Store
}

func New(cfg config.Config, store *storage.Store) *Manager {
	return &Manager{cfg: cfg, store: store}
}

func (m *Manager) CreateClient(ctx context.Context, name string) (storage.Client, string, error) {
	if config.IsPlaceholderValue(m.cfg.WireGuard.ServerEndpoint) {
		return storage.Client{}, "", fmt.Errorf("wireguard.server_endpoint must be explicitly configured before generating clients")
	}
	if config.IsPlaceholderValue(m.cfg.WireGuard.ServerPublicKey) {
		return storage.Client{}, "", fmt.Errorf("wireguard.server_public_key must be explicitly configured before generating clients")
	}

	priv, err := runOut(ctx, "wg genkey")
	if err != nil {
		return storage.Client{}, "", err
	}
	pub, err := runOut(ctx, fmt.Sprintf("printf '%s' | wg pubkey", shellQuote(priv)))
	if err != nil {
		return storage.Client{}, "", err
	}
	psk, err := runOut(ctx, "wg genpsk")
	if err != nil {
		return storage.Client{}, "", err
	}

	clientIP, err := m.nextIPv4()
	if err != nil {
		return storage.Client{}, "", err
	}
	id := randomID()
	c := storage.Client{
		ID:         id,
		Name:       name,
		PublicKey:  strings.TrimSpace(pub),
		PrivateKey: strings.TrimSpace(priv),
		Preshared:  strings.TrimSpace(psk),
		IPv4:       clientIP,
		CreatedAt:  time.Now().UTC(),
	}

	if err := m.store.UpsertClient(c); err != nil {
		return storage.Client{}, "", err
	}

	if err := m.ensurePeer(ctx, c); err != nil {
		return storage.Client{}, "", err
	}

	cfgText := m.renderClientConfig(c)
	return c, cfgText, nil
}

func (m *Manager) SyncClients(ctx context.Context) error {
	for _, c := range m.store.ListClients() {
		if err := m.ensurePeer(ctx, c); err != nil {
			return fmt.Errorf("sync client %s (%s): %w", c.Name, c.ID, err)
		}
	}
	return nil
}

func (m *Manager) ensurePeer(ctx context.Context, c storage.Client) error {
	allowed := fmt.Sprintf("%s/32", c.IPv4)
	setCmd := fmt.Sprintf("wg set %s peer %s preshared-key <(printf '%s') allowed-ips %s",
		m.cfg.WireGuard.ClientInterface, c.PublicKey, shellQuote(c.Preshared), allowed)
	return run(ctx, setCmd)
}

func (m *Manager) renderClientConfig(c storage.Client) string {
	return fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s/32
DNS = %s
MTU = %d

[Peer]
PublicKey = %s
PresharedKey = %s
Endpoint = %s
AllowedIPs = 0.0.0.0/0, ::/0
PersistentKeepalive = %d
`,
		c.PrivateKey,
		c.IPv4,
		m.cfg.WireGuard.ClientDNS,
		m.cfg.WireGuard.MTU,
		m.cfg.WireGuard.ServerPublicKey,
		c.Preshared,
		m.cfg.WireGuard.ServerEndpoint,
		m.cfg.WireGuard.KeepAliveSec,
	)
}

func (m *Manager) nextIPv4() (string, error) {
	_, netv4, err := net.ParseCIDR(m.cfg.WireGuard.ClientCIDRv4)
	if err != nil {
		return "", err
	}
	used := map[string]bool{}
	if ip := net.ParseIP(strings.TrimSpace(m.cfg.WireGuard.ClientDNS)); ip != nil {
		used[ip.String()] = true
	}
	for _, c := range m.store.ListClients() {
		used[c.IPv4] = true
	}

	all := hosts(*netv4)
	sort.Slice(all, func(i, j int) bool { return bytesCompare(all[i], all[j]) < 0 })
	for _, ip := range all {
		v := ip.String()
		if !used[v] {
			return v, nil
		}
	}
	return "", fmt.Errorf("no free ipv4 in %s", m.cfg.WireGuard.ClientCIDRv4)
}

func hosts(network net.IPNet) []net.IP {
	var ips []net.IP
	for ip := network.IP.Mask(network.Mask); network.Contains(ip); incIP(ip) {
		cp := make(net.IP, len(ip))
		copy(cp, ip)
		ips = append(ips, cp)
	}
	if len(ips) <= 2 {
		return nil
	}
	return ips[1 : len(ips)-1]
}

func incIP(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}

func bytesCompare(a, b net.IP) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return len(a) - len(b)
}

func randomID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func run(ctx context.Context, cmd string) error {
	c := exec.CommandContext(ctx, "bash", "-lc", cmd)
	out, err := c.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", cmd, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func runOut(ctx context.Context, cmd string) (string, error) {
	c := exec.CommandContext(ctx, "bash", "-lc", cmd)
	out, err := c.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %w: %s", cmd, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func shellQuote(v string) string {
	return strings.ReplaceAll(strings.TrimSpace(v), "'", "'\\''")
}
