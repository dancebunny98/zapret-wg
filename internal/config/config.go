package config

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

type Duration time.Duration

func (d *Duration) UnmarshalJSON(b []byte) error {
	// Accept either "1h30m" or raw nanoseconds.
	if len(b) == 0 {
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		v, err := time.ParseDuration(s)
		if err != nil {
			return err
		}
		*d = Duration(v)
		return nil
	}
	var ns int64
	if err := json.Unmarshal(b, &ns); err != nil {
		return err
	}
	*d = Duration(time.Duration(ns))
	return nil
}

func (d Duration) Std() time.Duration { return time.Duration(d) }

type Config struct {
	LogLevel string `json:"log_level"`

	API struct {
		Listen string `json:"listen"`
		Token  string `json:"token"`
	} `json:"api"`

	State struct {
		Path string `json:"path"`
	} `json:"state"`

	WireGuard struct {
		ClientInterface string `json:"client_interface"`
		UplinkInterface string `json:"uplink_interface"`
		ServerEndpoint  string `json:"server_endpoint"`
		ServerPublicKey string `json:"server_public_key"`
		ClientCIDRv4    string `json:"client_cidr_v4"`
		ClientDNS       string `json:"client_dns"`
		MTU             int    `json:"mtu"`
		KeepAliveSec    int    `json:"keepalive_sec"`
	} `json:"wireguard"`

	Dataplane struct {
		TableName          string `json:"table_name"`
		FwMarkHex          string `json:"fwmark_hex"`
		RouteTableID       int    `json:"route_table_id"`
		ClientInputIF      string `json:"client_input_if"`
		StrictOnUplinkDown bool   `json:"strict_on_uplink_down"`
	} `json:"dataplane"`

	DNS struct {
		ListenUDP      string   `json:"listen_udp"`
		ListenTCP      string   `json:"listen_tcp"`
		DirectUpstream string   `json:"direct_upstream"`
		VPNUpstream    string   `json:"vpn_upstream"`
		QueryTimeout   Duration `json:"query_timeout"`
	} `json:"dns"`

	Policy struct {
		FailureThreshold int      `json:"failure_threshold"`
		PinTTL           Duration `json:"pin_ttl"`
		IPTTL            Duration `json:"ip_ttl"`
	} `json:"policy"`

	Static struct {
		IPsFile         string   `json:"ips_file"`
		DomainsFile     string   `json:"domains_file"`
		ResolveInterval Duration `json:"resolve_interval"`
		ResolveEnabled  bool     `json:"resolve_enabled"`
	} `json:"static"`
}

func Load(path string) (Config, error) {
	var c Config
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	if err := c.setDefaults(); err != nil {
		return c, err
	}
	return c, nil
}

func (c *Config) setDefaults() error {
	if c.API.Listen == "" {
		c.API.Listen = "127.0.0.1:18080"
	}
	if c.State.Path == "" {
		c.State.Path = "/var/lib/vpngw/state.json"
	}
	if c.WireGuard.ClientInterface == "" {
		c.WireGuard.ClientInterface = "wg-clients"
	}
	if c.WireGuard.UplinkInterface == "" {
		c.WireGuard.UplinkInterface = "wg-uplink"
	}
	if c.WireGuard.ClientCIDRv4 == "" {
		c.WireGuard.ClientCIDRv4 = "10.70.0.0/24"
	}
	if c.WireGuard.ClientDNS == "" {
		c.WireGuard.ClientDNS = "10.70.0.1"
	}
	if c.WireGuard.MTU == 0 {
		c.WireGuard.MTU = 1420
	}
	if c.WireGuard.KeepAliveSec == 0 {
		c.WireGuard.KeepAliveSec = 25
	}

	if c.Dataplane.TableName == "" {
		c.Dataplane.TableName = "vpngw"
	}
	if c.Dataplane.FwMarkHex == "" {
		c.Dataplane.FwMarkHex = "0x66"
	}
	if c.Dataplane.RouteTableID == 0 {
		c.Dataplane.RouteTableID = 166
	}
	if c.Dataplane.ClientInputIF == "" {
		c.Dataplane.ClientInputIF = c.WireGuard.ClientInterface
	}

	if c.DNS.ListenUDP == "" {
		c.DNS.ListenUDP = net.JoinHostPort(c.WireGuard.ClientDNS, "53")
	}
	if c.DNS.ListenTCP == "" {
		c.DNS.ListenTCP = net.JoinHostPort(c.WireGuard.ClientDNS, "53")
	}
	if c.DNS.QueryTimeout == 0 {
		c.DNS.QueryTimeout = Duration(2 * time.Second)
	}
	if c.DNS.DirectUpstream == "" || c.DNS.VPNUpstream == "" {
		return fmt.Errorf("dns direct_upstream and vpn_upstream are required")
	}
	if IsPlaceholderValue(c.DNS.DirectUpstream) || IsPlaceholderValue(c.DNS.VPNUpstream) {
		return fmt.Errorf("dns direct_upstream and vpn_upstream must be explicitly configured")
	}

	if c.Policy.FailureThreshold <= 0 {
		c.Policy.FailureThreshold = 2
	}
	if c.Policy.PinTTL == 0 {
		c.Policy.PinTTL = Duration(60 * time.Minute)
	}
	if c.Policy.IPTTL == 0 {
		c.Policy.IPTTL = Duration(60 * time.Minute)
	}
	if c.Static.IPsFile == "" {
		c.Static.IPsFile = "/root/proga/force_vpn_ips.txt"
	}
	if c.Static.DomainsFile == "" {
		c.Static.DomainsFile = "/root/proga/force_vpn_domains.txt"
	}
	if c.Static.ResolveInterval == 0 {
		c.Static.ResolveInterval = Duration(15 * time.Minute)
	}
	return nil
}

func IsPlaceholderValue(v string) bool {
	v = strings.TrimSpace(strings.ToUpper(v))
	if v == "" {
		return true
	}
	for _, marker := range []string{"REQUIRED_", "YOUR_", "AUTO_SET_", "__WG_", "SET_ME"} {
		if strings.Contains(v, marker) {
			return true
		}
	}
	return false
}
