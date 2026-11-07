package app

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"finalshelly/internal/config"

	"golang.org/x/crypto/ssh"
)

// fakeSSH is an SSH server in the test that opens shells and refuses
// everything else, counting what the side panel's monitor would ask.
type fakeSSH struct {
	host       string
	port       int
	keepalives atomic.Int32 // keepalive@openssh.com requests
	execs      atomic.Int32 // exec requests

	mu  sync.Mutex
	pty fakePTY // the last pseudo-terminal asked for
}

// fakePTY is what a client asked of a pseudo-terminal.
type fakePTY struct {
	term  string
	erase uint32 // VERASE; 0 when not sent
}

func (srv *fakeSSH) lastPTY() fakePTY {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	return srv.pty
}

// notePTY keeps the pty-req's terminal type and erase character.
func (srv *fakeSSH) notePTY(payload []byte) {
	var req struct {
		Term             string
		Cols, Rows, W, H uint32
		Modes            string
	}
	if ssh.Unmarshal(payload, &req) != nil {
		return
	}
	pty := fakePTY{term: req.Term}
	modes := []byte(req.Modes)
	for len(modes) >= 5 && modes[0] != 0 { // TTY_OP_END
		if modes[0] == ssh.VERASE {
			pty.erase = uint32(modes[1])<<24 | uint32(modes[2])<<16 | uint32(modes[3])<<8 | uint32(modes[4])
		}
		modes = modes[5:]
	}
	srv.mu.Lock()
	srv.pty = pty
	srv.mu.Unlock()
}

func startFakeSSH(t *testing.T) *fakeSSH {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{PasswordCallback: func(_ ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
		if string(pw) != "secret" {
			return nil, errors.New("wrong password")
		}
		return nil, nil
	}}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	srv := &fakeSSH{}
	addr := ln.Addr().(*net.TCPAddr)
	srv.host, srv.port = addr.IP.String(), addr.Port
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go srv.serve(conn, cfg)
		}
	}()
	return srv
}

func (srv *fakeSSH) serve(conn net.Conn, cfg *ssh.ServerConfig) {
	sconn, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		conn.Close()
		return
	}
	defer sconn.Close()
	go func() {
		for r := range reqs {
			if r.Type == "keepalive@openssh.com" {
				srv.keepalives.Add(1)
			}
			r.Reply(true, nil)
		}
	}()
	for nc := range chans {
		if nc.ChannelType() != "session" {
			nc.Reject(ssh.UnknownChannelType, "no")
			continue
		}
		ch, chReqs, err := nc.Accept()
		if err != nil {
			continue
		}
		go io.Copy(io.Discard, ch)
		go func() {
			for r := range chReqs {
				switch r.Type {
				case "pty-req":
					srv.notePTY(r.Payload)
					r.Reply(true, nil)
				case "shell", "window-change":
					r.Reply(true, nil)
				case "exec":
					srv.execs.Add(1)
					r.Reply(false, nil)
				default: // env, subsystem (SFTP)
					r.Reply(false, nil)
				}
			}
		}()
	}
}

// connectFake opens a session for h, trusting the server's key,
// and waits until the session's setup is over: the SFTP step, after the
// monitor would have started, fails on this server.
func connectFake(t *testing.T, a *App, p *fakePlatform, h *config.Host) *Session {
	t.Helper()
	a.passwords[h.ID] = "secret"
	a.openSession(h)
	s := a.current()
	p.until(t, "the shell", func() bool {
		if s.hostKeyQ != nil { // the first time
			s.hostKeyQ.Answer <- true
			s.hostKeyQ = nil
		}
		return s.status == statusConnected || s.status == statusFailed
	})
	if s.status != statusConnected {
		t.Fatal(describeError(s.err))
	}
	p.until(t, "the SFTP step", func() bool { return s.files.err != nil })
	return s
}

func TestMonitorSwitch(t *testing.T) {
	srv := startFakeSSH(t)
	a, p := testApp(t)
	a.saveHosts = config.SaveHosts
	t.Cleanup(a.CloseAll)

	on := &config.Host{ID: "on", Addr: srv.host, Port: srv.port, User: "u", Auth: config.AuthPassword}
	off := &config.Host{ID: "off", Addr: srv.host, Port: srv.port, User: "u", Auth: config.AuthPassword, NoMonitor: true}
	a.hosts.Add(on)
	a.hosts.Add(off)

	before := time.Now()
	sOff := connectFake(t, a, p, off)
	start := time.Now()
	p.until(t, "a while", func() bool { return time.Since(start) > 500*time.Millisecond })
	if n, m := srv.keepalives.Load(), srv.execs.Load(); n != 0 || m != 0 {
		t.Fatalf("monitor off: %d keepalives, %d execs", n, m)
	}
	if sOff.stats.OK || len(sOff.stats.LatHist) != 0 {
		t.Errorf("stats with monitor off: %+v", sOff.stats)
	}

	// Connecting is kept, and saved, for the recent hosts.
	if off.LastConnectedAt.Before(before) || !on.LastConnectedAt.IsZero() {
		t.Errorf("last connected: off %v on %v", off.LastConnectedAt, on.LastConnectedAt)
	}
	saved, err := config.LoadHosts()
	if err != nil || !saved.Host("off").LastConnectedAt.Equal(off.LastConnectedAt) {
		t.Fatalf("saved %+v %v", saved.Host("off"), err)
	}

	sOn := connectFake(t, a, p, on)
	p.until(t, "the monitor", func() bool { return srv.keepalives.Load() > 0 && srv.execs.Load() > 0 && len(sOn.stats.LatHist) > 0 })
	if got := a.hosts.Recent(0); len(got) != 2 || got[0] != on {
		t.Errorf("recent %v", got)
	}
}
