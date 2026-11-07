package sshx

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"finallyshell/internal/config"
	"finallyshell/internal/sshtest"

	"golang.org/x/crypto/ssh"
)

// testServers are the servers the forwarding tests run against: the one
// in the test's process, and the real one FINALLYSHELL_TEST_SSH names
// (user:password@host:port), which has to allow TCP forwarding.
func testServers(t *testing.T, run func(t *testing.T, client *ssh.Client, drop func())) {
	t.Run("inprocess", func(t *testing.T) {
		srv := sshtest.Start(t)
		run(t, srv.Dial(t), srv.DropConnections)
	})
	t.Run("live", func(t *testing.T) {
		spec := os.Getenv("FINALLYSHELL_TEST_SSH")
		if spec == "" {
			t.Skip("FINALLYSHELL_TEST_SSH not set")
		}
		cred, addr, _ := strings.Cut(spec, "@")
		user, pass, _ := strings.Cut(cred, ":")
		client, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{
			User: user, Auth: []ssh.AuthMethod{ssh.Password(pass)}, HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { client.Close() })
		run(t, client, nil)
	})
}

// startEcho starts a server on addr that sends back what it reads, and
// ends its side after the client ends its own: nil when it cannot listen
// there, as on a machine without IPv6.
func startEcho(t *testing.T, addr string) *net.TCPAddr {
	t.Helper()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Log("cannot listen on", addr, err)
		return nil
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				io.Copy(c, c)
				c.(*net.TCPConn).CloseWrite()
				io.Copy(io.Discard, c)
				c.Close()
			}()
		}
	}()
	return ln.Addr().(*net.TCPAddr)
}

// freePort is a port nothing listens on, on the loopback.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// echoThrough sends msg on c, ends its side, and checks that msg comes
// back and then the end.
func echoThrough(t *testing.T, c net.Conn, msg string) {
	t.Helper()
	defer c.Close()
	c.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := c.Write([]byte(msg)); err != nil {
		t.Fatal(err)
	}
	if err := c.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(c)
	if err != nil || string(got) != msg {
		t.Fatalf("echoed %q, %v; want %q", got, err, msg)
	}
}

func dial(t *testing.T, addr net.Addr) net.Conn {
	t.Helper()
	port := addr.(*net.TCPAddr).Port
	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// socksDial asks the SOCKS5 proxy at addr to connect to host:port,
// with atyp the address type, and returns the reply code.
func socksDial(t *testing.T, addr net.Addr, cmd, atyp byte, host string, port int) (net.Conn, byte) {
	t.Helper()
	c := dial(t, addr)
	c.SetDeadline(time.Now().Add(10 * time.Second))
	c.Write([]byte{5, 1, 0})
	var choice [2]byte
	if _, err := io.ReadFull(c, choice[:]); err != nil || choice != [2]byte{5, 0} {
		t.Fatalf("method %v %v", choice, err)
	}
	req := []byte{5, cmd, 0, atyp}
	switch atyp {
	case 1:
		req = append(req, net.ParseIP(host).To4()...)
	case 4:
		req = append(req, net.ParseIP(host).To16()...)
	case 3:
		req = append(req, byte(len(host)))
		req = append(req, host...)
	}
	req = binary.BigEndian.AppendUint16(req, uint16(port))
	c.Write(req)
	var reply [10]byte
	if _, err := io.ReadFull(c, reply[:]); err != nil {
		t.Fatalf("reply: %v", err)
	}
	c.SetDeadline(time.Time{})
	return c, reply[1]
}

// noLeaks waits until the goroutines are back to base, as before the
// tunnels started.
func noLeaks(t *testing.T, base int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for runtime.NumGoroutine() > base {
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<20)
			t.Fatalf("%d goroutines, %d before:\n%s", runtime.NumGoroutine(), base, buf[:runtime.Stack(buf, true)])
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTunnels(t *testing.T) {
	testServers(t, func(t *testing.T, client *ssh.Client, drop func()) {
		echo := startEcho(t, "127.0.0.1:0")
		echo6 := startEcho(t, "[::1]:0")
		if echo == nil {
			t.Fatal("no echo server")
		}
		// The client starts two goroutines, for good, the first time it
		// listens on the server.
		if ln, err := client.Listen("tcp", "127.0.0.1:0"); err == nil {
			ln.Close()
		}
		time.Sleep(50 * time.Millisecond)
		base := runtime.NumGoroutine()

		var notified atomic.Int32
		notify := func() { notified.Add(1) }

		t.Run("local", func(t *testing.T) {
			tun, err := StartTunnel(client, config.Tunnel{Kind: config.TunnelLocal, TargetHost: "127.0.0.1", TargetPort: echo.Port}, notify)
			if err != nil {
				t.Fatal(err)
			}
			defer tun.Close()
			if a := tun.Addr().(*net.TCPAddr); !a.IP.IsLoopback() || a.Port == 0 {
				t.Errorf("listens on %v", a)
			}
			echoThrough(t, dial(t, tun.Addr()), "hello local")
			echoThrough(t, dial(t, tun.Addr()), strings.Repeat("x", 300_000))
			waitFor(t, "the stats", func() bool {
				st := tun.Stats()
				return st.Active == 0 && st.Total == 2 && st.Up == 300_011 && st.Down == 300_011
			})
			waitFor(t, "a notification", func() bool { return notified.Load() > 0 })

			// A target that is not there fails the connection, not the tunnel.
			bad, err := StartTunnel(client, config.Tunnel{Kind: config.TunnelLocal, TargetHost: "127.0.0.1", TargetPort: freePort(t)}, nil)
			if err != nil {
				t.Fatal(err)
			}
			c := dial(t, bad.Addr())
			c.SetDeadline(time.Now().Add(10 * time.Second))
			if n, err := c.Read(make([]byte, 1)); n != 0 || err == nil {
				t.Errorf("read %d, %v", n, err)
			}
			c.Close()
			waitFor(t, "the dial error", func() bool { return bad.DialErr() != nil })
			select {
			case <-bad.Done():
				t.Error("the tunnel stopped")
			default:
			}
			bad.Close()
		})

		t.Run("remote", func(t *testing.T) {
			tun, err := StartTunnel(client, config.Tunnel{Kind: config.TunnelRemote, TargetHost: "127.0.0.1", TargetPort: echo.Port}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if tun.Addr().(*net.TCPAddr).Port == 0 {
				t.Fatalf("listens on %v", tun.Addr())
			}
			// The server is on this machine: its port is ours.
			echoThrough(t, dial(t, tun.Addr()), "hello remote")
			waitFor(t, "the stats", func() bool {
				st := tun.Stats()
				return st.Active == 0 && st.Total == 1 && st.Up == 12 && st.Down == 12
			})
			port := tun.Addr().(*net.TCPAddr).Port
			tun.Close()
			// Closed: the server listens no more, and the port can be asked
			// for again.
			waitFor(t, "the server to stop listening", func() bool {
				c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
				if err == nil {
					c.Close()
				}
				return err != nil
			})
			again, err := StartTunnel(client, config.Tunnel{Kind: config.TunnelRemote, ListenPort: port, TargetHost: "127.0.0.1", TargetPort: echo.Port}, nil)
			if err != nil {
				t.Fatal(err)
			}
			echoThrough(t, dial(t, again.Addr()), "again")
			again.Close()
		})

		t.Run("dynamic", func(t *testing.T) {
			tun, err := StartTunnel(client, config.Tunnel{Kind: config.TunnelDynamic}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tun.Close()
			type target struct {
				name string
				atyp byte
				host string
				port int
			}
			targets := []target{{"IPv4", 1, "127.0.0.1", echo.Port}, {"name", 3, "localhost", echo.Port}}
			if echo6 != nil {
				targets = append(targets, target{"IPv6", 4, "::1", echo6.Port})
			} else {
				t.Log("no IPv6: an IPv6 address is not tried")
			}
			for _, tc := range targets {
				c, code := socksDial(t, tun.Addr(), 1, tc.atyp, tc.host, tc.port)
				if code != 0 {
					t.Fatalf("%s: reply %d", tc.name, code)
				}
				echoThrough(t, c, "hello "+tc.name)
			}
			c, code := socksDial(t, tun.Addr(), 1, 1, "127.0.0.1", freePort(t))
			c.Close()
			if code == 0 || drop != nil && code != 5 {
				t.Errorf("closed port: reply %d", code)
			}
			c, code = socksDial(t, tun.Addr(), 2, 1, "127.0.0.1", echo.Port) // BIND
			c.Close()
			if code != 7 {
				t.Errorf("BIND: reply %d", code)
			}
			// Not SOCKS5: closed.
			c = dial(t, tun.Addr())
			c.SetDeadline(time.Now().Add(10 * time.Second))
			c.Write([]byte{4, 1, 0, 80, 127, 0, 0, 1, 0})
			if n, _ := c.Read(make([]byte, 8)); n != 0 {
				t.Errorf("SOCKS4 answered %d bytes", n)
			}
			c.Close()
			waitFor(t, "the stats", func() bool { st := tun.Stats(); return st.Active == 0 && st.Total == int64(len(targets)+3) })
		})

		t.Run("in use", func(t *testing.T) {
			taken, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer taken.Close()
			port := taken.Addr().(*net.TCPAddr).Port
			for _, kind := range []config.TunnelKind{config.TunnelLocal, config.TunnelDynamic, config.TunnelRemote} {
				_, err := StartTunnel(client, config.Tunnel{Kind: kind, ListenPort: port, TargetHost: "127.0.0.1", TargetPort: echo.Port}, nil)
				var te *TunnelError
				if !errors.As(err, &te) || te.Addr != "127.0.0.1:"+strconv.Itoa(port) || te.Remote != (kind == config.TunnelRemote) || te.InUse != (kind != config.TunnelRemote) {
					t.Errorf("%s: %#v", kind, err)
				}
			}
			if _, err := StartTunnel(client, config.Tunnel{Kind: "x11"}, nil); !errors.Is(err, ErrUnknownTunnel) {
				t.Errorf("unknown kind: %v", err)
			}
			// The connection still works.
			tun, err := StartTunnel(client, config.Tunnel{Kind: config.TunnelLocal, TargetHost: "127.0.0.1", TargetPort: echo.Port}, nil)
			if err != nil {
				t.Fatal(err)
			}
			echoThrough(t, dial(t, tun.Addr()), "still")
			tun.Close()
		})

		t.Run("remote listen host", func(t *testing.T) {
			// A name is refused before the server is asked: x/crypto would
			// look it up here. localhost is the loopback.
			for _, host := range []string{"example.com", "my-server", "localhost.localdomain"} {
				_, err := StartTunnel(client, config.Tunnel{Kind: config.TunnelRemote, ListenHost: host, TargetHost: "127.0.0.1", TargetPort: echo.Port}, nil)
				var te *TunnelError
				if !errors.Is(err, ErrRemoteListenHost) || !errors.As(err, &te) || !te.Remote || te.Addr != net.JoinHostPort(host, "0") {
					t.Errorf("%s: %#v", host, err)
				}
			}
			for _, host := range []string{"localhost", "LOCALHOST", "127.0.0.1", ""} {
				tun, err := StartTunnel(client, config.Tunnel{Kind: config.TunnelRemote, ListenHost: host, TargetHost: "127.0.0.1", TargetPort: echo.Port}, nil)
				if err != nil {
					t.Fatalf("%q: %v", host, err)
				}
				if a := tun.Addr().(*net.TCPAddr); !a.IP.Equal(net.IPv4(127, 0, 0, 1)) {
					t.Errorf("%q listens on %v", host, a)
				}
				echoThrough(t, dial(t, tun.Addr()), "hello "+host)
				tun.Close()
			}
			for host, want := range map[string]string{"::": "[::]:9000", "0.0.0.0": "0.0.0.0:9000", "localhost": "127.0.0.1:9000"} {
				if got, err := RemoteListenAddr(config.Tunnel{ListenHost: host, ListenPort: 9000}); got != want || err != nil {
					t.Errorf("%q: %q, %v", host, got, err)
				}
			}
			// Local tunnels listen here: a name is looked up where it is
			// meant.
			tun, err := StartTunnel(client, config.Tunnel{Kind: config.TunnelLocal, ListenHost: "localhost", TargetHost: "127.0.0.1", TargetPort: echo.Port}, nil)
			if err != nil {
				t.Fatal(err)
			}
			tun.Close()
		})

		t.Run("close", func(t *testing.T) {
			// Closing ends the connections open through the tunnel, and
			// frees the port.
			for _, kind := range []config.TunnelKind{config.TunnelLocal, config.TunnelRemote, config.TunnelDynamic} {
				tun, err := StartTunnel(client, config.Tunnel{Kind: kind, TargetHost: "127.0.0.1", TargetPort: echo.Port}, nil)
				if err != nil {
					t.Fatal(err)
				}
				var c net.Conn
				if kind == config.TunnelDynamic {
					var code byte
					if c, code = socksDial(t, tun.Addr(), 1, 1, "127.0.0.1", echo.Port); code != 0 {
						t.Fatalf("reply %d", code)
					}
				} else {
					c = dial(t, tun.Addr())
				}
				c.SetDeadline(time.Now().Add(10 * time.Second))
				c.Write([]byte("ping"))
				buf := make([]byte, 4)
				if _, err := io.ReadFull(c, buf); err != nil || string(buf) != "ping" {
					t.Fatalf("%s: %q %v", kind, buf, err)
				}
				waitFor(t, "the connection", func() bool { return tun.Stats().Active == 1 })
				addr := tun.Addr().(*net.TCPAddr)
				tun.Close()
				tun.Close() // again: nothing
				if n, err := c.Read(buf); n != 0 || err == nil {
					t.Errorf("%s: read %d, %v after closing", kind, n, err)
				}
				c.Close()
				if tun.Err() != nil || tun.Stats().Active != 0 {
					t.Errorf("%s: err %v, stats %+v", kind, tun.Err(), tun.Stats())
				}
				select {
				case <-tun.Done():
				default:
					t.Errorf("%s: not done", kind)
				}
				if kind != config.TunnelRemote {
					ln, err := net.Listen("tcp", addr.String())
					if err != nil {
						t.Fatalf("%s: port not freed: %v", kind, err)
					}
					ln.Close()
				}
			}
		})

		noLeaks(t, base)

		if drop == nil {
			return
		}
		t.Run("connection lost", func(t *testing.T) {
			// A remote tunnel stops by itself as the connection goes; a
			// local one listens until closed.
			remote, err := StartTunnel(client, config.Tunnel{Kind: config.TunnelRemote, TargetHost: "127.0.0.1", TargetPort: echo.Port}, nil)
			if err != nil {
				t.Fatal(err)
			}
			drop()
			select {
			case <-remote.Done():
			case <-time.After(10 * time.Second):
				t.Fatal("the remote tunnel did not stop")
			}
			if remote.Err() == nil {
				t.Error("no error")
			}
			remote.Close()
		})
	})
}
