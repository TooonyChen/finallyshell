package sshx

import (
	"context"
	"testing"
	"time"

	"finallyshell/internal/config"
	"finallyshell/internal/sshtest"
)

// closed tells whether wait, a client's Wait, returns within d.
func closed(t *testing.T, wait func() error, d time.Duration) bool {
	t.Helper()
	done := make(chan struct{})
	go func() {
		wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

// TestKeepAlive sends keepalives at the interval, and closes the client
// once the server leaves MaxMissed of them unanswered.
func TestKeepAlive(t *testing.T) {
	srv := sshtest.Start(t)
	client := srv.Dial(t)
	ended := make(chan struct{})
	go func() {
		KeepAlive(context.Background(), client, KeepAliveConfig{Interval: 20 * time.Millisecond, MaxMissed: 3})
		close(ended)
	}()
	waitFor(t, "keepalives", func() bool { return srv.KeepAlives() >= 3 })
	if closed(t, client.Wait, 100*time.Millisecond) {
		t.Fatal("closed while the server answers")
	}

	srv.StopAnswering()
	start := time.Now()
	if !closed(t, client.Wait, 5*time.Second) {
		t.Fatal("still open after the server stopped answering")
	}
	// Three timeouts of 20 ms, the first of which may have started.
	if d := time.Since(start); d < 40*time.Millisecond {
		t.Errorf("closed after %v, before 3 were missed", d)
	}
	<-ended
}

// TestKeepAliveEnds ends with its context, and when the client closes,
// leaving the connection as it was.
func TestKeepAliveEnds(t *testing.T) {
	srv := sshtest.Start(t)
	client := srv.Dial(t)
	ctx, cancel := context.WithCancel(context.Background())
	ended := make(chan struct{})
	go func() {
		KeepAlive(ctx, client, KeepAliveConfig{Interval: time.Hour})
		close(ended)
	}()
	cancel()
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("did not end with its context")
	}
	if _, _, err := client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
		t.Fatalf("connection closed: %v", err)
	}

	ended = make(chan struct{})
	go func() {
		KeepAlive(context.Background(), client, KeepAliveConfig{Interval: time.Hour})
		close(ended)
	}()
	client.Close()
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("did not end with the client")
	}
}

// TestDialKeepAliveJumpHost starts the keepalive on the host's client
// and on the jump host's.
func TestDialKeepAliveJumpHost(t *testing.T) {
	jump, srv := sshtest.Start(t), sshtest.Start(t)
	jumpHost := &config.Host{ID: "j", Addr: jump.Host, Port: jump.Port, User: "j", Auth: config.AuthPassword}
	h := &config.Host{ID: "h", Addr: srv.Host, Port: srv.Port, User: "u", Auth: config.AuthPassword,
		Proxy: &config.ProxyConfig{Kind: config.ProxyJump, JumpHost: "j"}}
	var asked []string
	opts := testOptions(t, sshtest.Password, &asked)
	opts.Jump = &Hop{Host: jumpHost, Options: testOptions(t, sshtest.Password, &asked)}
	opts.KeepAlive = KeepAliveConfig{Interval: 20 * time.Millisecond}
	client, err := Dial(h, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	waitFor(t, "keepalives on both", func() bool { return jump.KeepAlives() >= 2 && srv.KeepAlives() >= 2 })

	// The jump host stops answering: its keepalive closes its client,
	// and the host's connection through it ends.
	jump.StopAnswering()
	if !closed(t, client.Wait, 5*time.Second) {
		t.Fatal("the host's client still open after the jump host stopped answering")
	}
}
