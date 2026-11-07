package sshx

// Proxies and jump hosts: how Dial reaches a host whose config.Host.Proxy
// is set.
//
// A SOCKS5 or HTTP proxy is dialed over TCP, asked to connect to the
// host, and the SSH connection runs over what it opens. A jump host is
// dialed and logged in to first, as any host, through its own proxy or
// jump host if it has one: its SSH server opens a direct-tcpip channel to
// the host, which carries the SSH connection. Closing the host's client
// closes the jump host's too.
//
// Failures on the way are *HopError, naming the proxy or jump host that
// failed; those of the host itself are returned as without a proxy.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"finalshelly/internal/config"

	"golang.org/x/crypto/ssh"
	"golang.org/x/net/proxy"
)

// Hop is a jump host, with what logging in to it takes: its password or
// passphrase, and its own proxy's password or jump host.
type Hop struct {
	Host    *config.Host
	Options Options
}

// Errors of a proxy, which HopError wraps.
var (
	// ErrProxyAuth is a proxy that refused the user and password, or
	// asked for them when there were none.
	ErrProxyAuth = errors.New("proxy authentication failed")
	// ErrNoJumpHost is a host whose proxy is a jump host, dialed without
	// Options.Jump.
	ErrNoJumpHost = errors.New("no jump host given")
)

// HopError is a failure on the way to a host: at its proxy, or at a jump
// host. Of a chain, it names the first that failed.
type HopError struct {
	// Proxy is the SOCKS5 or HTTP proxy that failed, as host:port; ""
	// for a jump host.
	Proxy string
	// JumpHost is the title of the jump host that failed.
	JumpHost string
	// HostID is the ID of the host the hop belongs to, which titles may
	// not tell apart: the jump host that failed, or the host whose proxy
	// failed.
	HostID string
	Err    error
}

func (e *HopError) Error() string {
	if e.Proxy != "" {
		return "proxy " + e.Proxy + ": " + e.Err.Error()
	}
	return "jump host " + e.JumpHost + ": " + e.Err.Error()
}

func (e *HopError) Unwrap() error { return e.Err }

// ProxyTargetError is a proxy that could not open the connection to the
// host: a SOCKS5 reply that is not success, or an HTTP status that is
// not 2xx.
type ProxyTargetError struct {
	Addr   string // the host's host:port
	Reason string // "connection refused", "502 Bad Gateway"
}

func (e *ProxyTargetError) Error() string { return "cannot reach " + e.Addr + ": " + e.Reason }

// ForwardError is a jump host that could not open the connection to the
// next host.
type ForwardError struct {
	Addr string // the next host's host:port
	Err  error
}

func (e *ForwardError) Error() string { return "cannot reach " + e.Addr + ": " + e.Err.Error() }
func (e *ForwardError) Unwrap() error { return e.Err }

// dialRoute connects to h as cfg says, the way h.Proxy says.
func dialRoute(h *config.Host, opts Options, cfg *ssh.ClientConfig) (*ssh.Client, error) {
	addr := net.JoinHostPort(h.Addr, strconv.Itoa(h.PortOr22()))
	p := config.ProxyOf(h)
	if p == nil {
		return ssh.Dial("tcp", addr, cfg)
	}
	var conn net.Conn
	var err error
	switch p.Kind {
	case config.ProxySOCKS5:
		conn, err = dialSOCKS5(p, opts.ProxyPassword, addr, cfg.Timeout)
	case config.ProxyHTTP:
		conn, err = dialHTTP(p, opts.ProxyPassword, addr, cfg.Timeout)
	case config.ProxyJump:
		conn, err = dialJump(opts.Jump, addr, cfg.Timeout)
		if err == nil {
			// known_hosts again, with what trusting the jump host's key
			// added: a host may share it, or be its own jump host.
			hk, herr := hostKeyCallback(opts)
			if herr != nil {
				conn.Close()
				return nil, herr
			}
			c := *cfg
			c.HostKeyCallback = hk
			cfg = &c
		}
	default:
		return nil, fmt.Errorf("unknown proxy kind %q", p.Kind)
	}
	var hop *HopError
	if err != nil && p.Kind != config.ProxyJump && errors.As(err, &hop) && hop.HostID == "" {
		hop.HostID = h.ID // h's own proxy failed
	}
	if err != nil {
		return nil, err
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg) // closes conn when it fails
	if err != nil {
		return nil, err
	}
	return ssh.NewClient(c, chans, reqs), nil
}

// ProxyAddr is the proxy's host:port.
func ProxyAddr(p *config.ProxyConfig) string {
	return net.JoinHostPort(p.Addr, strconv.Itoa(p.PortOrDefault()))
}

// dialSOCKS5 asks the SOCKS5 proxy p to connect to addr, with the user
// and password when p has a user. The proxy resolves addr's name.
func dialSOCKS5(p *config.ProxyConfig, password, addr string, timeout time.Duration) (net.Conn, error) {
	paddr := ProxyAddr(p)
	raw, err := net.DialTimeout("tcp", paddr, timeout)
	if err != nil {
		return nil, &HopError{Proxy: paddr, Err: err}
	}
	var auth *proxy.Auth
	if p.User != "" {
		auth = &proxy.Auth{User: p.User, Password: password}
	}
	d, err := proxy.SOCKS5("tcp", paddr, auth, dialed{raw})
	if err != nil {
		raw.Close()
		return nil, &HopError{Proxy: paddr, Err: err}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	conn, err := d.(proxy.ContextDialer).DialContext(ctx, "tcp", addr)
	if err != nil {
		raw.Close()
		return nil, &HopError{Proxy: paddr, Err: socksError(err, addr)}
	}
	return newHopConn(conn, nil, addr, nil), nil
}

// dialed is a proxy.Dialer that returns a connection already made, so
// that a failure to reach the proxy is told apart from the proxy's own.
// It is a ContextDialer too: with Dial alone, the proxy package dials on
// a goroutine that closes the connection if the context is done by the
// time it looks, which the deferred cancel can be.
type dialed struct{ c net.Conn }

func (d dialed) Dial(string, string) (net.Conn, error) { return d.c, nil }

func (d dialed) DialContext(context.Context, string, string) (net.Conn, error) { return d.c, nil }

// socksError says what the SOCKS5 handshake's error means.
func socksError(err error, addr string) error {
	var op *net.OpError
	if errors.As(err, &op) {
		err = op.Err
	}
	msg := err.Error()
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return os.ErrDeadlineExceeded
	case strings.Contains(msg, "authentication failed"), strings.Contains(msg, "no acceptable authentication methods"):
		return ErrProxyAuth
	case strings.HasPrefix(msg, "unknown error "):
		return &ProxyTargetError{Addr: addr, Reason: strings.TrimPrefix(msg, "unknown error ")}
	}
	return err
}

// dialHTTP asks the HTTP proxy p to connect to addr with CONNECT, with
// basic authentication when p has a user.
func dialHTTP(p *config.ProxyConfig, password, addr string, timeout time.Duration) (net.Conn, error) {
	paddr := ProxyAddr(p)
	raw, err := net.DialTimeout("tcp", paddr, timeout)
	if err != nil {
		return nil, &HopError{Proxy: paddr, Err: err}
	}
	fail := func(err error) (net.Conn, error) {
		raw.Close()
		if errors.Is(err, os.ErrDeadlineExceeded) {
			err = os.ErrDeadlineExceeded
		}
		return nil, &HopError{Proxy: paddr, Err: err}
	}
	raw.SetDeadline(time.Now().Add(timeout))
	req := "CONNECT " + addr + " HTTP/1.1\r\nHost: " + addr + "\r\n"
	if p.User != "" {
		req += "Proxy-Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(p.User+":"+password)) + "\r\n"
	}
	if _, err := io.WriteString(raw, req+"\r\n"); err != nil {
		return fail(err)
	}
	br := bufio.NewReader(raw)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		return fail(err)
	}
	// Not resp.Body.Close: the body of a CONNECT's 2xx is the
	// connection, which reading to its end would take.
	switch {
	case resp.StatusCode == http.StatusProxyAuthRequired:
		return fail(ErrProxyAuth)
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return fail(&ProxyTargetError{Addr: addr, Reason: resp.Status})
	}
	raw.SetDeadline(time.Time{})
	// The server's first bytes may have come with the answer.
	var early io.Reader
	if n := br.Buffered(); n > 0 {
		buf, _ := br.Peek(n)
		early = bytes.NewReader(bytes.Clone(buf))
	}
	return newHopConn(raw, early, addr, nil), nil
}

// dialJump logs in to the jump host, the way it says, and opens a
// connection to addr through it.
func dialJump(hop *Hop, addr string, timeout time.Duration) (net.Conn, error) {
	if hop == nil || hop.Host == nil {
		return nil, ErrNoJumpHost
	}
	name, id := hop.Host.Title(), hop.Host.ID
	jc, err := Dial(hop.Host, hop.Options)
	if err != nil {
		var hop *HopError
		if errors.As(err, &hop) {
			return nil, err // a hop before the jump host failed: it says which
		}
		return nil, &HopError{JumpHost: name, HostID: id, Err: err}
	}
	conn, err := dialThrough(jc, addr, timeout)
	if err != nil {
		jc.Close()
		return nil, &HopError{JumpHost: name, HostID: id, Err: &ForwardError{Addr: addr, Err: err}}
	}
	return newHopConn(conn, nil, addr, jc), nil
}

// dialThrough opens a connection to addr from client's server, giving up
// after timeout.
func dialThrough(client *ssh.Client, addr string, timeout time.Duration) (net.Conn, error) {
	type result struct {
		conn net.Conn
		err  error
	}
	done := make(chan result, 1)
	go func() {
		conn, err := client.Dial("tcp", addr)
		done <- result{conn, err}
	}()
	select {
	case r := <-done:
		return r.conn, r.err
	case <-time.After(timeout):
		client.Close() // ends the Dial; the caller closes it anyway
		return nil, os.ErrDeadlineExceeded
	}
}

// hopConn is a connection to a host made through a proxy or a jump host.
// Its remote address is the host's, which the host key is checked
// against and the app shows, not the proxy's: a *net.TCPAddr for an IP
// address, else the name. Closing it closes the jump host's client.
type hopConn struct {
	net.Conn
	early  io.Reader // what was read past the proxy's answer; nil when none
	remote net.Addr
	via    io.Closer // the jump host's client; nil for a proxy

	once sync.Once
}

func newHopConn(conn net.Conn, early io.Reader, addr string, via io.Closer) *hopConn {
	var remote net.Addr = hostAddr(addr)
	if host, port, err := net.SplitHostPort(addr); err == nil {
		if ip := net.ParseIP(host); ip != nil {
			n, _ := strconv.Atoi(port)
			remote = &net.TCPAddr{IP: ip, Port: n}
		}
	}
	return &hopConn{Conn: conn, early: early, remote: remote, via: via}
}

func (c *hopConn) Read(p []byte) (int, error) {
	if c.early != nil {
		n, err := c.early.Read(p)
		if err == io.EOF {
			c.early, err = nil, nil
		}
		if n > 0 || err != nil {
			return n, err
		}
	}
	return c.Conn.Read(p)
}

func (c *hopConn) RemoteAddr() net.Addr { return c.remote }

func (c *hopConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() {
		if c.via != nil {
			c.via.Close()
		}
	})
	return err
}

// hostAddr is a host:port whose host is a name.
type hostAddr string

func (a hostAddr) Network() string { return "tcp" }
func (a hostAddr) String() string  { return string(a) }
