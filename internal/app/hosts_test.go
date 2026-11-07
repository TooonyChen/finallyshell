package app

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"finalshelly/internal/config"
	"finalshelly/locales"

	"github.com/egoist/mygo/ui"
	"golang.org/x/crypto/ssh"
)

// fakeSSH is an SSH server in the test that opens shells and refuses
// everything else, counting what the side panel's monitor would ask.
type fakeSSH struct {
	host       string
	port       int
	keepalives atomic.Int32 // keepalive@openssh.com requests
	execs      atomic.Int32 // exec requests
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
				case "pty-req", "shell", "window-change":
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

func TestManagerHidesDeletedHosts(t *testing.T) {
	a, _ := testApp(t)
	a.hosts.Add(&config.Host{ID: "gone", Name: "gone-host", Addr: "10.0.0.9", User: "root", Auth: config.AuthPassword, DeletedAt: time.Now()})
	a.hosts.Add(&config.Host{ID: "kept", Name: "kept-host", Addr: "10.0.0.1", User: "root", Auth: config.AuthPassword})
	tt := ui.NewTester(a.View, 1280, 820)
	a.openManager()
	tt.Frame()
	if a.formOpen || !tt.HasText("kept-host") || tt.HasText("gone-host") {
		t.Fatalf("form %v; texts %q", a.formOpen, tt.Texts())
	}
	// Row 0 is the first host shown, not the first saved.
	a.hostRow = 0
	tt.Frame()
	if err := tt.Click(locales.Delete()); err != nil {
		t.Fatal(err)
	}
	if len(a.hosts.Hosts) != 1 || a.hosts.Host("gone") == nil {
		t.Fatalf("deleted the wrong host: %v", a.hosts.Hosts)
	}
	// With only deleted hosts left, the manager starts with a new host.
	a.managerOpen = false
	a.openManager()
	tt.Frame()
	if !a.formOpen || tt.HasText("gone-host") {
		t.Errorf("form %v; texts %q", a.formOpen, tt.Texts())
	}
}

func TestEditKeepsHostFields(t *testing.T) {
	a, _ := testApp(t)
	var saved []byte
	a.saveHosts = func(s *config.Store) error {
		var err error
		saved, err = json.Marshal(s)
		return err
	}
	var h config.Host
	err := json.Unmarshal([]byte(`{"id": "h1", "name": "web", "addr": "10.0.0.1", "user": "root", "auth": "password",
		"folder": "f1", "notes": "main box", "noMonitor": true, "lastConnectedAt": "2026-11-07T10:00:00Z",
		"proxy": {"kind": "socks5"}}`), &h)
	if err != nil {
		t.Fatal(err)
	}
	a.hosts.Add(&h)
	tt := ui.NewTester(a.View, 1280, 820)
	a.openManager()
	a.hostRow = 0
	tt.Frame()
	if err := tt.Click(locales.Edit()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	a.form.name = "web-1"
	if err := tt.Click(locales.Save()); err != nil {
		t.Fatal(err)
	}
	if a.form.err != nil {
		t.Fatal(a.form.err())
	}
	var file struct {
		Hosts []map[string]any `json:"hosts"`
	}
	if err := json.Unmarshal(saved, &file); err != nil || len(file.Hosts) != 1 {
		t.Fatalf("saved %s: %v", saved, err)
	}
	got := file.Hosts[0]
	for k, want := range map[string]any{
		"name": "web-1", "folder": "f1", "notes": "main box", "noMonitor": true,
		"lastConnectedAt": "2026-11-07T10:00:00Z", "proxy": map[string]any{"kind": "socks5"},
	} {
		if !reflect.DeepEqual(got[k], want) {
			t.Errorf("%s = %v, want %v", k, got[k], want)
		}
	}
}
