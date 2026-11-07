package sshx

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"finallyshell/internal/config"

	"golang.org/x/crypto/ssh"
)

// ErrUnknownTunnel is a tunnel of a kind this build does not know, as a
// newer one may write.
var ErrUnknownTunnel = errors.New("unknown kind of tunnel")

// ErrRemoteListenHost is a remote tunnel whose listen address is a host
// name: the address is sent to the server as an IP address, and a name
// would be looked up here, not on the server.
var ErrRemoteListenHost = errors.New("a remote tunnel listens on an IP address or localhost, not a host name")

// RemoteListenAddr is host:port that a remote tunnel asks the server to
// listen on: its ListenHost, an IP address, or the loopback for "" and
// localhost. It fails with ErrRemoteListenHost for a host name.
func RemoteListenAddr(t config.Tunnel) (string, error) {
	host := t.ListenHost
	if host == "" || strings.EqualFold(host, "localhost") {
		host = config.LoopbackHost
	}
	if net.ParseIP(host) == nil {
		return "", ErrRemoteListenHost
	}
	return net.JoinHostPort(host, strconv.Itoa(t.ListenPort)), nil
}

// TunnelError is a tunnel that could not start listening.
type TunnelError struct {
	// Addr is where it was to listen.
	Addr string
	// Remote is set when the server was to listen, and refused or failed.
	Remote bool
	// InUse is set when the port is taken on this computer.
	InUse bool
	Err   error
}

func (e *TunnelError) Error() string { return "listen on " + e.Addr + ": " + e.Err.Error() }
func (e *TunnelError) Unwrap() error { return e.Err }

// Tunnel is a port forwarded through an SSH connection, as ssh -L, -R or
// -D: a listener, and the connections it has open. Its methods are safe
// to call from any goroutine.
type Tunnel struct {
	spec   config.Tunnel
	client *ssh.Client
	ln     net.Listener
	notify func()

	active   atomic.Int64
	total    atomic.Int64
	up, down atomic.Int64

	wg   sync.WaitGroup // the accept loop and every connection
	done chan struct{}

	mu      sync.Mutex
	stopped bool
	err     error // why it stopped by itself; nil once closed
	dialErr error // why the last connection could not reach the target
	conns   map[io.Closer]struct{}
	timer   *time.Timer // a notify that is due
}

// TunnelStats are what a tunnel has carried.
type TunnelStats struct {
	// Active are the connections open now; Total all of them.
	Active, Total int64
	// Up are the bytes from the side that connected to the target, Down
	// the bytes back.
	Up, Down int64
}

// notifyEvery is how often a tunnel calls notify at most.
const notifyEvery = 250 * time.Millisecond

// StartTunnel starts listening as t says, on this computer or, for a
// remote tunnel, on the server client is connected to, and forwards
// what connects. notify, if not nil, is called on a goroutine of the
// tunnel's after its stats or state change, at most every quarter of a
// second; it must not block. The tunnel stops on Stop or Close, or by
// itself when the listener fails, as when the connection is lost.
func StartTunnel(client *ssh.Client, t config.Tunnel, notify func()) (*Tunnel, error) {
	addr := t.ListenAddr()
	var ln net.Listener
	var err error
	switch t.Kind {
	case config.TunnelLocal, config.TunnelDynamic:
		ln, err = net.Listen("tcp", addr)
		if err != nil {
			return nil, &TunnelError{Addr: addr, InUse: addrInUse(err), Err: err}
		}
	case config.TunnelRemote:
		// x/crypto resolves the address on this computer: only an IP
		// address means the same on the server.
		var raddr string
		if raddr, err = RemoteListenAddr(t); err != nil {
			return nil, &TunnelError{Addr: addr, Remote: true, Err: err}
		}
		addr = raddr
		ln, err = client.Listen("tcp", addr)
		if err != nil {
			return nil, &TunnelError{Addr: addr, Remote: true, Err: err}
		}
	default:
		return nil, ErrUnknownTunnel
	}
	tun := &Tunnel{spec: t, client: client, ln: ln, notify: notify, done: make(chan struct{}), conns: map[io.Closer]struct{}{}}
	tun.wg.Add(1)
	go tun.accept()
	return tun, nil
}

// Addr is the address the tunnel listens on, the port chosen when it
// asked for 0.
func (t *Tunnel) Addr() net.Addr { return t.ln.Addr() }

// Stats are what the tunnel has carried so far.
func (t *Tunnel) Stats() TunnelStats {
	return TunnelStats{Active: t.active.Load(), Total: t.total.Load(), Up: t.up.Load(), Down: t.down.Load()}
}

// Done is closed once the tunnel stops listening.
func (t *Tunnel) Done() <-chan struct{} { return t.done }

// Err is why the tunnel stopped by itself, as ErrConnectionClosed; nil
// while it listens, and after Stop.
func (t *Tunnel) Err() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.err
}

// DialErr is why the last connection that failed could not reach its
// target; nil once one reaches it.
func (t *Tunnel) DialErr() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.dialErr
}

// Stop stops listening and closes the tunnel's connections. It does not
// wait: a port on this computer is free once it returns, while a server
// is told to stop listening on a goroutine. Stopping again does nothing.
func (t *Tunnel) Stop() { t.stop(nil) }

// Wait waits until the tunnel has stopped and its goroutines are gone.
func (t *Tunnel) Wait() { t.wg.Wait() }

// Close stops the tunnel, as Stop, and waits until its connections are
// gone, as Wait. Closing again does nothing.
func (t *Tunnel) Close() error {
	t.Stop()
	t.Wait()
	t.mu.Lock()
	if t.timer != nil {
		t.timer.Stop()
		t.timer = nil
	}
	t.mu.Unlock()
	return nil
}

// stop closes the listener and the connections; err is why, nil for
// Stop.
func (t *Tunnel) stop(err error) {
	t.mu.Lock()
	if t.stopped {
		t.mu.Unlock()
		return
	}
	t.stopped, t.err = true, err
	conns := t.conns
	t.conns = nil
	t.mu.Unlock()
	if t.spec.Kind == config.TunnelRemote {
		// Cancelling the forward waits for the server's answer. The
		// counter is above zero, for the accept loop, or Close calls stop
		// before it waits.
		t.wg.Add(1)
		go func() {
			defer t.wg.Done()
			t.ln.Close()
		}()
	} else {
		t.ln.Close()
	}
	for c := range conns {
		c.Close()
	}
	close(t.done)
	t.changed()
}

// track keeps c to close as the tunnel stops; false, closing c, when it
// has stopped.
func (t *Tunnel) track(c io.Closer) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopped {
		c.Close()
		return false
	}
	t.conns[c] = struct{}{}
	return true
}

func (t *Tunnel) untrack(c io.Closer) {
	c.Close()
	t.mu.Lock()
	delete(t.conns, c)
	t.mu.Unlock()
}

// changed calls notify soon, once for the changes until then.
func (t *Tunnel) changed() {
	if t.notify == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.timer == nil {
		t.timer = time.AfterFunc(notifyEvery, func() {
			t.mu.Lock()
			t.timer = nil
			t.mu.Unlock()
			t.notify()
		})
	}
}

func (t *Tunnel) noteDial(err error) {
	t.mu.Lock()
	t.dialErr = err
	t.mu.Unlock()
}

func (t *Tunnel) accept() {
	defer t.wg.Done()
	for {
		c, err := t.ln.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) && !errors.Is(err, io.EOF) {
				t.stop(err)
			} else {
				t.stop(ErrConnectionClosed)
			}
			return
		}
		// Added under the lock that stop takes, so that Close, waiting,
		// sees it.
		t.mu.Lock()
		if t.stopped {
			t.mu.Unlock()
			c.Close()
			return
		}
		t.wg.Add(1)
		t.conns[c] = struct{}{}
		t.mu.Unlock()
		t.total.Add(1)
		t.active.Add(1)
		t.changed()
		go t.handle(c)
	}
}

// wsaeAddrInUse is Windows' error for a port taken, which syscall does
// not name there.
const wsaeAddrInUse = 10048

// addrInUse tells whether err, from listening, is a port taken.
func addrInUse(err error) bool {
	var errno syscall.Errno
	return errors.As(err, &errno) && (errno == syscall.EADDRINUSE || runtime.GOOS == "windows" && errno == wsaeAddrInUse)
}

// ErrConnectionClosed is a tunnel whose listener ended without Stop: the
// connection to the server is gone.
var ErrConnectionClosed = errors.New("the connection to the server closed")

func (t *Tunnel) handle(c net.Conn) {
	defer func() {
		t.untrack(c)
		t.active.Add(-1)
		t.changed()
		t.wg.Done()
	}()
	var target string
	if t.spec.Kind == config.TunnelDynamic {
		c.SetDeadline(time.Now().Add(30 * time.Second))
		var err error
		if target, err = socksRequest(c); err != nil {
			return
		}
		c.SetDeadline(time.Time{})
	} else {
		target = t.spec.TargetAddr()
	}
	var out net.Conn
	var err error
	if t.spec.Kind == config.TunnelRemote {
		out, err = net.DialTimeout("tcp", target, 15*time.Second)
	} else {
		out, err = t.client.Dial("tcp", target)
	}
	if t.spec.Kind == config.TunnelDynamic {
		code := byte(socksOK)
		if err != nil {
			code = socksCode(err)
		}
		if werr := socksReply(c, code); werr != nil && err == nil {
			out.Close()
			return
		}
	}
	if err != nil {
		t.noteDial(fmt.Errorf("%s: %w", target, err))
		t.changed()
		return
	}
	t.noteDial(nil)
	if !t.track(out) {
		return
	}
	defer t.untrack(out)
	t.pipe(c, out)
}

// pipe copies between in, the connection that came in, and out, the one
// to the target, until both directions end. As one direction ends, the
// other side is told no more is coming, if it can be, as ssh does.
func (t *Tunnel) pipe(in, out net.Conn) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		t.copy(out, in, &t.up)
	}()
	t.copy(in, out, &t.down)
	wg.Wait()
}

type closeWriter interface{ CloseWrite() error }

func (t *Tunnel) copy(dst, src net.Conn, n *atomic.Int64) {
	buf := make([]byte, 32*1024)
	var err error
	for {
		k, rerr := src.Read(buf)
		if k > 0 {
			if _, werr := dst.Write(buf[:k]); werr != nil {
				err = werr
				break
			}
			n.Add(int64(k))
			t.changed()
		}
		if rerr != nil {
			if rerr != io.EOF {
				err = rerr
			}
			break
		}
	}
	if cw, ok := dst.(closeWriter); ok && err == nil {
		cw.CloseWrite()
		return
	}
	// One side failed: neither direction can go on.
	dst.Close()
	src.Close()
}

// SOCKS5 (RFC 1928), as ssh -D serves it: no authentication, CONNECT
// only, to an IPv4 or IPv6 address or a name that the server resolves.
const (
	socksVersion   = 5
	socksNoAuth    = 0
	socksNoMethod  = 0xff
	socksConnect   = 1
	socksIPv4      = 1
	socksDomain    = 3
	socksIPv6      = 4
	socksOK        = 0
	socksFailure   = 1
	socksNotAllow  = 2
	socksNetUnr    = 3
	socksRefused   = 5
	socksBadCmd    = 7
	socksBadAddrTy = 8
)

// socksRequest reads a SOCKS5 greeting and CONNECT request from c, and
// returns the host:port asked for. It answers what it cannot serve.
func socksRequest(c net.Conn) (string, error) {
	var head [2]byte
	if _, err := io.ReadFull(c, head[:]); err != nil {
		return "", err
	}
	if head[0] != socksVersion {
		return "", fmt.Errorf("socks: version %d", head[0])
	}
	methods := make([]byte, head[1])
	if _, err := io.ReadFull(c, methods); err != nil {
		return "", err
	}
	if !slices.Contains(methods, socksNoAuth) {
		c.Write([]byte{socksVersion, socksNoMethod})
		return "", errors.New("socks: the client needs authentication")
	}
	if _, err := c.Write([]byte{socksVersion, socksNoAuth}); err != nil {
		return "", err
	}
	var req [4]byte // version, command, reserved, address type
	if _, err := io.ReadFull(c, req[:]); err != nil {
		return "", err
	}
	if req[0] != socksVersion {
		return "", fmt.Errorf("socks: version %d", req[0])
	}
	var host string
	switch req[3] {
	case socksIPv4, socksIPv6:
		ip := make(net.IP, 4)
		if req[3] == socksIPv6 {
			ip = make(net.IP, 16)
		}
		if _, err := io.ReadFull(c, ip); err != nil {
			return "", err
		}
		host = ip.String()
	case socksDomain:
		var n [1]byte
		if _, err := io.ReadFull(c, n[:]); err != nil {
			return "", err
		}
		name := make([]byte, n[0])
		if _, err := io.ReadFull(c, name); err != nil {
			return "", err
		}
		host = string(name)
	default:
		socksReply(c, socksBadAddrTy)
		return "", fmt.Errorf("socks: address type %d", req[3])
	}
	var port [2]byte
	if _, err := io.ReadFull(c, port[:]); err != nil {
		return "", err
	}
	if req[1] != socksConnect {
		socksReply(c, socksBadCmd)
		return "", fmt.Errorf("socks: command %d", req[1])
	}
	return net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(port[:])))), nil
}

// socksReply answers a request with code, and no bound address: the
// server's own is no use to the client.
func socksReply(c net.Conn, code byte) error {
	_, err := c.Write([]byte{socksVersion, code, 0, socksIPv4, 0, 0, 0, 0, 0, 0})
	return err
}

// socksCode is the reply for a connection the server could not open.
func socksCode(err error) byte {
	var open *ssh.OpenChannelError
	if errors.As(err, &open) {
		switch open.Reason {
		case ssh.Prohibited:
			return socksNotAllow
		case ssh.ConnectionFailed:
			msg := strings.ToLower(open.Message)
			switch {
			case strings.Contains(msg, "refused"):
				return socksRefused
			case strings.Contains(msg, "unreachable"):
				return socksNetUnr
			}
		}
	}
	return socksFailure
}
