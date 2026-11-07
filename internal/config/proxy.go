package config

import (
	"errors"
	"reflect"
	"strings"
)

// ProxyKind is what a host connects through.
type ProxyKind string

const (
	// ProxyNone connects directly, as a host without a proxy.
	ProxyNone ProxyKind = ""
	// ProxySOCKS5 connects through a SOCKS5 proxy, which resolves the
	// host's name itself.
	ProxySOCKS5 ProxyKind = "socks5"
	// ProxyHTTP connects through an HTTP proxy with CONNECT.
	ProxyHTTP ProxyKind = "http"
	// ProxyJump connects through another saved host, a jump host, as
	// OpenSSH's ProxyJump: its SSH server opens the connection.
	ProxyJump ProxyKind = "jump"
)

// DefaultProxyPort is the usual port of a proxy of kind k: 1080 for
// SOCKS5, 8080 for HTTP, 0 for the others.
func DefaultProxyPort(k ProxyKind) int {
	switch k {
	case ProxySOCKS5:
		return 1080
	case ProxyHTTP:
		return 8080
	}
	return 0
}

// ProxyConfig is a host's proxy. A SOCKS5 or HTTP proxy has an address,
// a port, and a user when it asks for one, whose password is kept as the
// host's: not in hosts.json. A jump host is another saved host, which may
// have a proxy of its own.
type ProxyConfig struct {
	Kind ProxyKind `json:"kind,omitempty"`
	Addr string    `json:"addr,omitempty"`
	Port int       `json:"port,omitempty"`
	User string    `json:"user,omitempty"`
	// JumpHost is the ID of the jump host, for ProxyJump.
	JumpHost string `json:"jumpHost,omitempty"`

	extra extras
}

// PortOrDefault is the proxy's port, the usual one for its kind when
// unset.
func (p *ProxyConfig) PortOrDefault() int {
	if p.Port == 0 {
		return DefaultProxyPort(p.Kind)
	}
	return p.Port
}

// ProxyOf is the proxy h connects through: nil when it connects directly.
func ProxyOf(h *Host) *ProxyConfig {
	if h.Proxy == nil || h.Proxy.Kind == ProxyNone {
		return nil
	}
	return h.Proxy
}

type proxyJSON ProxyConfig

var proxyKeys = knownKeys(reflect.TypeFor[proxyJSON]())

func (p ProxyConfig) MarshalJSON() ([]byte, error) { return encodeObject(proxyJSON(p), p.extra) }

func (p *ProxyConfig) UnmarshalJSON(data []byte) error {
	var v proxyJSON
	ex, err := decodeObject(data, &v, proxyKeys)
	if err != nil {
		return err
	}
	*p = ProxyConfig(v)
	p.extra = ex
	return nil
}

// ErrJumpHostMissing is a jump host that is not saved, or was deleted.
var ErrJumpHostMissing = errors.New("the jump host is missing")

// JumpLoopError is a chain of jump hosts that comes back to one of them.
type JumpLoopError struct {
	// Names are the hosts' titles along the chain, the one met again
	// last.
	Names []string
}

func (e *JumpLoopError) Error() string {
	return "jump hosts loop: " + strings.Join(e.Names, " → ")
}

// JumpError is a jump host on the way to a host that cannot be used.
type JumpError struct {
	// From is the title of the host whose jump host it is.
	From string
	Err  error
}

func (e *JumpError) Error() string { return "jump host of " + e.From + ": " + e.Err.Error() }
func (e *JumpError) Unwrap() error { return e.Err }

// JumpHosts are the jump hosts on the way to h, the one h connects
// through first and the one that connects first last: none when h does
// not go through a jump host. It fails with a *JumpError for a jump
// host that is missing or deleted, and a *JumpLoopError for a chain that
// comes back to a host already on it, h included.
func (s *Store) JumpHosts(h *Host) ([]*Host, error) {
	var jumps []*Host
	seen := map[string]bool{}
	names := []string{h.Title()}
	if h.ID != "" {
		seen[h.ID] = true
	}
	for cur := h; ; {
		p := ProxyOf(cur)
		if p == nil || p.Kind != ProxyJump {
			return jumps, nil
		}
		next := s.Host(p.JumpHost)
		if next == nil || next.Deleted() {
			return nil, &JumpError{From: cur.Title(), Err: ErrJumpHostMissing}
		}
		names = append(names, next.Title())
		if seen[next.ID] {
			return nil, &JumpLoopError{Names: names}
		}
		seen[next.ID] = true
		jumps = append(jumps, next)
		cur = next
	}
}
