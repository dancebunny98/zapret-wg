package policy

import (
	"context"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"vpngw/internal/config"
	"vpngw/internal/dataplane"
	"vpngw/internal/storage"
)

type Engine struct {
	cfg           config.Config
	dp            *dataplane.Manager
	store         *storage.Store
	mu            sync.Mutex
	forcedDomains map[string]struct{}
}

func New(cfg config.Config, dp *dataplane.Manager, store *storage.Store) *Engine {
	return &Engine{cfg: cfg, dp: dp, store: store, forcedDomains: map[string]struct{}{}}
}

func (e *Engine) ObserveFailure(ctx context.Context, domain, reason string, ips []net.IP) error {
	domain = normalizeDomain(domain)
	if domain == "" && len(ips) == 0 {
		return nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now().UTC()
	if domain != "" {
		d, ok := e.store.GetDomain(domain)
		if !ok {
			d = storage.DomainState{Domain: domain, Mode: "DIRECT"}
		}
		d.Failures++
		d.LastFailAt = now
		d.Reason = reason
		d.UpdatedAt = now
		if d.Failures >= e.cfg.Policy.FailureThreshold {
			d.Mode = "FORCE_VPN"
			d.PinUntil = now.Add(e.cfg.Policy.PinTTL.Std())
			for _, ip := range ips {
				if err := e.dp.AddIP(ctx, ip, e.cfg.Policy.IPTTL.Std()); err != nil {
					log.Printf("add ip failed %s: %v", ip, err)
				}
				d.IPs = upsertIP(d.IPs, ip.String())
			}
		}
		if err := e.store.UpsertDomain(d); err != nil {
			return err
		}
		return nil
	}

	for _, ip := range ips {
		if err := e.dp.AddIP(ctx, ip, e.cfg.Policy.IPTTL.Std()); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) ObserveDNSFallback(ctx context.Context, domain string, ips []net.IP) error {
	domain = normalizeDomain(domain)
	if domain == "" {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now().UTC()
	d, ok := e.store.GetDomain(domain)
	if !ok {
		d = storage.DomainState{Domain: domain}
	}
	d.Mode = "FORCE_VPN"
	d.PinUntil = now.Add(e.cfg.Policy.PinTTL.Std())
	d.UpdatedAt = now
	d.Reason = "dns_fallback_to_vpn"
	for _, ip := range ips {
		if err := e.dp.AddIP(ctx, ip, e.cfg.Policy.IPTTL.Std()); err != nil {
			log.Printf("add ip failed %s: %v", ip, err)
			continue
		}
		d.IPs = upsertIP(d.IPs, ip.String())
	}
	return e.store.UpsertDomain(d)
}

func (e *Engine) IsVPNPinned(domain string) bool {
	domain = normalizeDomain(domain)
	if domain == "" {
		return false
	}
	if e.IsForcedDomain(domain) {
		return true
	}
	d, ok := e.store.GetDomain(domain)
	if !ok {
		return false
	}
	return d.Mode == "FORCE_VPN" && time.Now().UTC().Before(d.PinUntil)
}

func (e *Engine) ReplaceForcedDomains(domains []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	next := make(map[string]struct{}, len(domains))
	for _, d := range domains {
		d = normalizeDomain(d)
		if d != "" {
			next[d] = struct{}{}
		}
	}
	e.forcedDomains = next
}

func (e *Engine) IsForcedDomain(domain string) bool {
	domain = normalizeDomain(domain)
	if domain == "" {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	_, ok := e.forcedDomains[domain]
	return ok
}

func normalizeDomain(v string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(v)), ".")
}

func upsertIP(items []string, ip string) []string {
	for _, x := range items {
		if x == ip {
			return items
		}
	}
	return append(items, ip)
}
