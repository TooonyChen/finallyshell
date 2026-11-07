package config

import (
	"net"
	"reflect"
	"strconv"
)

// TunnelKind is which way a tunnel forwards, as ssh's -L, -R and -D.
type TunnelKind string

const (
	// TunnelLocal listens on this computer and forwards each connection,
	// through the server, to the target as the server reaches it (-L).
	TunnelLocal TunnelKind = "local"
	// TunnelRemote has the server listen and forwards each connection
	// back to the target as this computer reaches it (-R).
	TunnelRemote TunnelKind = "remote"
	// TunnelDynamic is a SOCKS5 proxy on this computer whose connections
	// go out from the server (-D): it has no target.
	TunnelDynamic TunnelKind = "dynamic"
)

// LoopbackHost is where a tunnel listens unless the user chose an
// address: only programs on the same machine can connect.
const LoopbackHost = "127.0.0.1"

// Tunnel is a port forwarded through a host's connection.
type Tunnel struct {
	Kind TunnelKind `json:"kind"`
	// ListenHost is the address the tunnel listens on, on this computer
	// or, for TunnelRemote, on the server: "" for LoopbackHost. Any other
	// address lets other machines in, which the user has to choose.
	ListenHost string `json:"listenHost,omitempty"`
	ListenPort int    `json:"listenPort,omitempty"`
	// TargetHost and TargetPort are where connections go: unset for
	// TunnelDynamic, whose clients name it.
	TargetHost string `json:"targetHost,omitempty"`
	TargetPort int    `json:"targetPort,omitempty"`
	// Disabled keeps the tunnel from starting with the session; it can
	// still be started by hand. The inverse, so that a Tunnel{} is on.
	Disabled bool `json:"disabled,omitempty"`

	extra extras
}

// Enabled says whether t starts as the session connects.
func (t *Tunnel) Enabled() bool { return !t.Disabled }

// ListenHostOrLoopback is the address t listens on.
func (t *Tunnel) ListenHostOrLoopback() string {
	if t.ListenHost == "" {
		return LoopbackHost
	}
	return t.ListenHost
}

// ListenAddr is host:port that t listens on.
func (t *Tunnel) ListenAddr() string {
	return net.JoinHostPort(t.ListenHostOrLoopback(), strconv.Itoa(t.ListenPort))
}

// TargetAddr is host:port that t forwards to; "" for TunnelDynamic.
func (t *Tunnel) TargetAddr() string {
	if t.Kind == TunnelDynamic {
		return ""
	}
	return net.JoinHostPort(t.TargetHost, strconv.Itoa(t.TargetPort))
}

// IsLoopback tells whether a tunnel listening on host, a ListenHost,
// lets in only the machine it listens on: "", localhost, or a loopback
// address.
func IsLoopback(host string) bool {
	if host == "" || host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

type tunnelJSON Tunnel

var tunnelKeys = knownKeys(reflect.TypeFor[tunnelJSON]())

func (t Tunnel) MarshalJSON() ([]byte, error) { return encodeObject(tunnelJSON(t), t.extra) }

func (t *Tunnel) UnmarshalJSON(data []byte) error {
	var v tunnelJSON
	ex, err := decodeObject(data, &v, tunnelKeys)
	if err != nil {
		return err
	}
	*t = Tunnel(v)
	t.extra = ex
	return nil
}
