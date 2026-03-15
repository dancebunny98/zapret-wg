package app

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/miekg/dns"

	"vpngw/internal/config"
	"vpngw/internal/dataplane"
	"vpngw/internal/dnsfw"
	"vpngw/internal/policy"
	"vpngw/internal/storage"
	"vpngw/internal/wg"
)

type Server struct {
	cfg           config.Config
	store         *storage.Store
	dp            *dataplane.Manager
	policy        *policy.Engine
	dns           *dnsfw.Server
	wg            *wg.Manager
	httpSrv       *http.Server
	forcedDomains []string
}

func NewServer(cfg config.Config) (*Server, error) {
	store, err := storage.New(cfg.State.Path)
	if err != nil {
		return nil, err
	}
	dp := dataplane.New(cfg)
	p := policy.New(cfg, dp, store)
	return &Server{
		cfg:    cfg,
		store:  store,
		dp:     dp,
		policy: p,
		dns:    dnsfw.New(cfg, p),
		wg:     wg.New(cfg, store),
	}, nil
}

func (s *Server) Start(ctx context.Context) error {
	bootCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := s.dp.EnsureBase(bootCtx); err != nil {
		return fmt.Errorf("ensure dataplane: %w", err)
	}
	if err := s.restorePinnedIPs(bootCtx); err != nil {
		log.Printf("restore pins: %v", err)
	}
	if err := s.loadStaticLists(bootCtx); err != nil {
		log.Printf("load static lists: %v", err)
	}
	if err := s.wg.SyncClients(bootCtx); err != nil {
		return fmt.Errorf("restore wg clients: %w", err)
	}
	if err := s.dns.Start(ctx); err != nil {
		return fmt.Errorf("start dns: %w", err)
	}
	go s.staticDomainRefreshLoop(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/report", s.handleReport)
	mux.HandleFunc("/v1/clients", s.handleClients)
	mux.HandleFunc("/v1/domains", s.handleDomains)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })

	s.httpSrv = &http.Server{
		Addr:              s.cfg.API.Listen,
		Handler:           s.auth(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = s.httpSrv.Shutdown(stopCtx)
	}()

	log.Printf("api listening on %s", s.cfg.API.Listen)
	if err := s.httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *Server) CreateClient(ctx context.Context, name string) (storage.Client, string, error) {
	return s.wg.CreateClient(ctx, name)
}

func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Domain string   `json:"domain"`
		IPs    []string `json:"ips"`
		Reason string   `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ips := make([]net.IP, 0, len(req.IPs))
	for _, s := range req.IPs {
		if ip := net.ParseIP(strings.TrimSpace(s)); ip != nil {
			ips = append(ips, ip)
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := s.policy.ObserveFailure(ctx, req.Domain, req.Reason, ips); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleClients(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, s.store.ListClients())
	case http.MethodPost:
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.Name) == "" {
			http.Error(w, "name required", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		client, cfgText, err := s.wg.CreateClient(ctx, req.Name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{
			"client": client,
			"config": cfgText,
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleDomains(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, s.store.ListDomains())
}

func (s *Server) auth(next http.Handler) http.Handler {
	if strings.TrimSpace(s.cfg.API.Token) == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+s.cfg.API.Token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) restorePinnedIPs(ctx context.Context) error {
	now := time.Now().UTC()
	for _, d := range s.store.ListDomains() {
		if d.Mode != "FORCE_VPN" || now.After(d.PinUntil) {
			continue
		}
		for _, v := range d.IPs {
			ip := net.ParseIP(v)
			if ip == nil {
				continue
			}
			if err := s.dp.AddIP(ctx, ip, s.cfg.Policy.IPTTL.Std()); err != nil {
				log.Printf("restore ip %s failed: %v", v, err)
			}
		}
	}
	return nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func (s *Server) loadStaticLists(ctx context.Context) error {
	ipLines, err := readListFile(s.cfg.Static.IPsFile)
	if err != nil {
		return err
	}
	domainLines, err := readListFile(s.cfg.Static.DomainsFile)
	if err != nil {
		return err
	}
	s.forcedDomains = domainLines
	s.policy.ReplaceForcedDomains(domainLines)
	for _, item := range ipLines {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ipnet, err := net.ParseCIDR(item); err == nil && ipnet != nil {
			isV6 := ipnet.IP.To4() == nil
			if err := s.dp.AddStaticElement(ctx, ipnet.String(), isV6); err != nil {
				log.Printf("add static cidr %s failed: %v", item, err)
			}
			continue
		}
		ip := net.ParseIP(item)
		if ip == nil {
			log.Printf("skip invalid static ip/cidr: %s", item)
			continue
		}
		if err := s.dp.AddStaticElement(ctx, ip.String(), ip.To4() == nil); err != nil {
			log.Printf("add static ip %s failed: %v", item, err)
		}
	}
	if s.cfg.Static.ResolveEnabled {
		s.resolveForcedDomainsOnce(ctx)
	}
	return nil
}

func (s *Server) staticDomainRefreshLoop(ctx context.Context) {
	if !s.cfg.Static.ResolveEnabled {
		return
	}
	interval := s.cfg.Static.ResolveInterval.Std()
	if interval < time.Minute {
		interval = 15 * time.Minute
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			runCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			s.resolveForcedDomainsOnce(runCtx)
			cancel()
		}
	}
}

func (s *Server) resolveForcedDomainsOnce(ctx context.Context) {
	if len(s.forcedDomains) == 0 {
		return
	}
	for _, d := range s.forcedDomains {
		ips := resolveAandAAAA(d, s.cfg.DNS.VPNUpstream, s.cfg.DNS.QueryTimeout.Std())
		if len(ips) == 0 {
			continue
		}
		if err := s.policy.ObserveDNSFallback(ctx, d, ips); err != nil {
			log.Printf("force domain resolve failed %s: %v", d, err)
		}
	}
}

func resolveAandAAAA(domain, upstream string, timeout time.Duration) []net.IP {
	out := make([]net.IP, 0, 4)
	for _, qt := range []uint16{dns.TypeA, dns.TypeAAAA} {
		m := new(dns.Msg)
		m.SetQuestion(dns.Fqdn(domain), qt)
		c := &dns.Client{Timeout: timeout}
		resp, _, err := c.Exchange(m, upstream)
		if err != nil || resp == nil {
			continue
		}
		for _, rr := range resp.Answer {
			switch v := rr.(type) {
			case *dns.A:
				out = append(out, v.A)
			case *dns.AAAA:
				out = append(out, v.AAAA)
			}
		}
	}
	return out
}

func readListFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	defer f.Close()
	out := []string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out, sc.Err()
}
