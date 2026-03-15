package dnsfw

import (
	"context"
	"log"
	"net"
	"strings"
	"time"

	"github.com/miekg/dns"

	"vpngw/internal/config"
	"vpngw/internal/policy"
)

type Server struct {
	cfg    config.Config
	policy *policy.Engine
	udp    *dns.Server
	tcp    *dns.Server
}

func New(cfg config.Config, p *policy.Engine) *Server {
	return &Server{cfg: cfg, policy: p}
}

func (s *Server) Start(ctx context.Context) error {
	mux := dns.NewServeMux()
	mux.HandleFunc(".", s.handle)
	s.udp = &dns.Server{Addr: s.cfg.DNS.ListenUDP, Net: "udp", Handler: mux}
	s.tcp = &dns.Server{Addr: s.cfg.DNS.ListenTCP, Net: "tcp", Handler: mux}

	go func() {
		<-ctx.Done()
		_ = s.udp.Shutdown()
		_ = s.tcp.Shutdown()
	}()
	go func() {
		if err := s.udp.ListenAndServe(); err != nil {
			log.Printf("dns udp stopped: %v", err)
		}
	}()
	go func() {
		if err := s.tcp.ListenAndServe(); err != nil {
			log.Printf("dns tcp stopped: %v", err)
		}
	}()
	return nil
}

func (s *Server) handle(w dns.ResponseWriter, req *dns.Msg) {
	msg := new(dns.Msg)
	msg.SetReply(req)
	msg.Authoritative = false
	msg.RecursionAvailable = true

	if len(req.Question) == 0 {
		_ = w.WriteMsg(msg)
		return
	}

	q := req.Question[0]
	domain := strings.TrimSuffix(strings.ToLower(q.Name), ".")
	timeout := s.cfg.DNS.QueryTimeout.Std()
	if timeout <= 0 {
		timeout = 2 * time.Second
	}

	if s.policy.IsForcedDomain(domain) {
		vpnAns, vpnErr := exchange(req, s.cfg.DNS.VPNUpstream, timeout)
		if vpnErr != nil || vpnAns == nil {
			msg.Rcode = dns.RcodeServerFailure
			_ = w.WriteMsg(msg)
			return
		}
		ips := extractIPs(vpnAns.Answer)
		if len(ips) > 0 {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			_ = s.policy.ObserveDNSFallback(ctx, domain, ips)
			cancel()
		}
		_ = w.WriteMsg(vpnAns)
		return
	}

	ans, err := exchange(req, s.cfg.DNS.DirectUpstream, timeout)
	if err == nil && ans != nil && ans.Rcode == dns.RcodeSuccess {
		_ = w.WriteMsg(ans)
		return
	}

	vpnAns, vpnErr := exchange(req, s.cfg.DNS.VPNUpstream, timeout)
	if vpnErr != nil || vpnAns == nil {
		msg.Rcode = dns.RcodeServerFailure
		_ = w.WriteMsg(msg)
		return
	}

	ips := extractIPs(vpnAns.Answer)
	if len(ips) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		_ = s.policy.ObserveDNSFallback(ctx, domain, ips)
		cancel()
	}
	_ = w.WriteMsg(vpnAns)
}

func exchange(req *dns.Msg, upstream string, timeout time.Duration) (*dns.Msg, error) {
	c := &dns.Client{Timeout: timeout}
	resp, _, err := c.Exchange(req, upstream)
	return resp, err
}

func extractIPs(rr []dns.RR) []net.IP {
	ips := make([]net.IP, 0, len(rr))
	for _, r := range rr {
		switch t := r.(type) {
		case *dns.A:
			ips = append(ips, t.A)
		case *dns.AAAA:
			ips = append(ips, t.AAAA)
		}
	}
	return ips
}
