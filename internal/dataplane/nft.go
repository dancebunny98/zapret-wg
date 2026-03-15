package dataplane

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"

	"vpngw/internal/config"
)

type Manager struct {
	cfg config.Config
}

func New(cfg config.Config) *Manager {
	return &Manager{cfg: cfg}
}

func (m *Manager) EnsureBase(ctx context.Context) error {
	t := m.cfg.Dataplane.TableName
	mark := m.cfg.Dataplane.FwMarkHex
	iface := m.cfg.Dataplane.ClientInputIF
	clampIfaces := make([]string, 0, 2)
	seenClampIfaces := make(map[string]struct{}, 2)
	for _, name := range []string{m.cfg.WireGuard.UplinkInterface, m.cfg.WireGuard.ClientInterface} {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seenClampIfaces[name]; ok {
			continue
		}
		seenClampIfaces[name] = struct{}{}
		clampIfaces = append(clampIfaces, name)
	}

	cmds := []string{
		fmt.Sprintf("nft list table inet %s >/dev/null 2>&1 || nft add table inet %s", t, t),
		fmt.Sprintf("nft list set inet %s vpn_targets_v4 >/dev/null 2>&1 || nft add set inet %s vpn_targets_v4 '{ type ipv4_addr; flags timeout; }'", t, t),
		fmt.Sprintf("nft list set inet %s vpn_targets_v6 >/dev/null 2>&1 || nft add set inet %s vpn_targets_v6 '{ type ipv6_addr; flags timeout; }'", t, t),
		fmt.Sprintf("nft list set inet %s vpn_static_v4 >/dev/null 2>&1 || nft add set inet %s vpn_static_v4 '{ type ipv4_addr; flags interval; }'", t, t),
		fmt.Sprintf("nft list set inet %s vpn_static_v6 >/dev/null 2>&1 || nft add set inet %s vpn_static_v6 '{ type ipv6_addr; flags interval; }'", t, t),
		fmt.Sprintf("nft list chain inet %s mangle_prerouting >/dev/null 2>&1 || nft add chain inet %s mangle_prerouting '{ type filter hook prerouting priority mangle; policy accept; }'", t, t),
		fmt.Sprintf("nft list chain inet %s mangle_output >/dev/null 2>&1 || nft add chain inet %s mangle_output '{ type route hook output priority mangle; policy accept; }'", t, t),
		fmt.Sprintf("nft list chain inet %s mss_clamp >/dev/null 2>&1 || nft add chain inet %s mss_clamp '{ type filter hook forward priority mangle; policy accept; }'", t, t),
		fmt.Sprintf("nft flush chain inet %s mangle_prerouting", t),
		fmt.Sprintf("nft flush chain inet %s mangle_output", t),
		fmt.Sprintf("nft flush chain inet %s mss_clamp", t),
		fmt.Sprintf("nft add rule inet %s mangle_prerouting iifname \"%s\" ip daddr @vpn_targets_v4 meta mark set %s", t, iface, mark),
		fmt.Sprintf("nft add rule inet %s mangle_prerouting iifname \"%s\" ip6 daddr @vpn_targets_v6 meta mark set %s", t, iface, mark),
		fmt.Sprintf("nft add rule inet %s mangle_prerouting iifname \"%s\" ip daddr @vpn_static_v4 meta mark set %s", t, iface, mark),
		fmt.Sprintf("nft add rule inet %s mangle_prerouting iifname \"%s\" ip6 daddr @vpn_static_v6 meta mark set %s", t, iface, mark),
		fmt.Sprintf("nft add rule inet %s mangle_output ip daddr @vpn_targets_v4 meta mark set %s", t, mark),
		fmt.Sprintf("nft add rule inet %s mangle_output ip6 daddr @vpn_targets_v6 meta mark set %s", t, mark),
		fmt.Sprintf("nft add rule inet %s mangle_output ip daddr @vpn_static_v4 meta mark set %s", t, mark),
		fmt.Sprintf("nft add rule inet %s mangle_output ip6 daddr @vpn_static_v6 meta mark set %s", t, mark),
		fmt.Sprintf("ip rule show | grep -q 'fwmark %s lookup %d' || ip rule add fwmark %s table %d", mark, m.cfg.Dataplane.RouteTableID, mark, m.cfg.Dataplane.RouteTableID),
		fmt.Sprintf("ip -4 route replace default dev %s table %d", m.cfg.WireGuard.UplinkInterface, m.cfg.Dataplane.RouteTableID),
		fmt.Sprintf("ip -6 route replace default dev %s table %d || true", m.cfg.WireGuard.UplinkInterface, m.cfg.Dataplane.RouteTableID),
		"nft list table ip vpngw_nat >/dev/null 2>&1 || nft add table ip vpngw_nat",
		"nft list chain ip vpngw_nat postrouting >/dev/null 2>&1 || nft add chain ip vpngw_nat postrouting '{ type nat hook postrouting priority srcnat; policy accept; }'",
		fmt.Sprintf("nft list ruleset | grep -q 'ip saddr %s oifname != \"%s\" masquerade' || nft add rule ip vpngw_nat postrouting ip saddr %s oifname != \"%s\" masquerade",
			m.cfg.WireGuard.ClientCIDRv4, m.cfg.WireGuard.ClientInterface, m.cfg.WireGuard.ClientCIDRv4, m.cfg.WireGuard.ClientInterface),
	}
	for _, name := range clampIfaces {
		cmds = append(cmds, fmt.Sprintf("nft add rule inet %s mss_clamp oifname \"%s\" tcp flags syn tcp option maxseg size set rt mtu", t, name))
	}

	for _, c := range cmds {
		if err := run(ctx, c); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) AddIP(ctx context.Context, ip net.IP, ttl time.Duration) error {
	set := "vpn_targets_v4"
	if ip.To4() == nil {
		set = "vpn_targets_v6"
	}
	sec := int(ttl.Seconds())
	if sec < 30 {
		sec = 30
	}
	cmd := fmt.Sprintf("nft add element inet %s %s { %s timeout %ds }",
		m.cfg.Dataplane.TableName, set, ip.String(), sec)
	return run(ctx, cmd)
}

func (m *Manager) FlushExpired(ctx context.Context) error {
	// nft timeout cleanup is automatic.
	return nil
}

func (m *Manager) AddStaticElement(ctx context.Context, s string, isV6 bool) error {
	set := "vpn_static_v4"
	if isV6 {
		set = "vpn_static_v6"
	}
	cmd := fmt.Sprintf("nft add element inet %s %s { %s } 2>/dev/null || true",
		m.cfg.Dataplane.TableName, set, s)
	return run(ctx, cmd)
}

func run(ctx context.Context, line string) error {
	cmd := exec.CommandContext(ctx, "bash", "-lc", line)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return fmt.Errorf("cmd failed: %s: %w", line, err)
		}
		return fmt.Errorf("cmd failed: %s: %w: %s", line, err, msg)
	}
	return nil
}
