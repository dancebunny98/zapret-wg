package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type DomainState struct {
	Domain     string    `json:"domain"`
	Mode       string    `json:"mode"` // DIRECT, FORCE_VPN
	Reason     string    `json:"reason"`
	Failures   int       `json:"failures"`
	LastFailAt time.Time `json:"last_fail_at"`
	PinUntil   time.Time `json:"pin_until"`
	IPs        []string  `json:"ips"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type Client struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	PublicKey  string    `json:"public_key"`
	PrivateKey string    `json:"private_key"`
	Preshared  string    `json:"preshared"`
	IPv4       string    `json:"ipv4"`
	CreatedAt  time.Time `json:"created_at"`
}

type State struct {
	Domains map[string]DomainState `json:"domains"`
	Clients map[string]Client      `json:"clients"`
}

type Store struct {
	path string
	mu   sync.RWMutex
	s    State
}

func New(path string) (*Store, error) {
	st := &Store{
		path: path,
		s: State{
			Domains: map[string]DomainState{},
			Clients: map[string]Client{},
		},
	}
	if err := st.load(); err != nil {
		return nil, err
	}
	return st, nil
}

func (s *Store) load() error {
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var v State
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	if v.Domains == nil {
		v.Domains = map[string]DomainState{}
	}
	if v.Clients == nil {
		v.Clients = map[string]Client{}
	}
	s.s = v
	return nil
}

func (s *Store) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.s, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) UpsertDomain(d DomainState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.s.Domains[d.Domain] = d
	return s.saveLocked()
}

func (s *Store) GetDomain(domain string) (DomainState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.s.Domains[domain]
	return d, ok
}

func (s *Store) ListDomains() []DomainState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]DomainState, 0, len(s.s.Domains))
	for _, d := range s.s.Domains {
		out = append(out, d)
	}
	return out
}

func (s *Store) UpsertClient(c Client) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.s.Clients[c.ID] = c
	return s.saveLocked()
}

func (s *Store) ListClients() []Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Client, 0, len(s.s.Clients))
	for _, c := range s.s.Clients {
		out = append(out, c)
	}
	return out
}

func (s *Store) GetClient(id string) (Client, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.s.Clients[id]
	return c, ok
}

