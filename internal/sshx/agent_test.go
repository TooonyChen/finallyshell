package sshx

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"finallyshell/internal/config"
	"finallyshell/internal/sshtest"

	"golang.org/x/crypto/ssh"
)

// agentHost is a host of srv that logs in with an agent.
func agentHost(srv *sshtest.Server) *config.Host {
	return &config.Host{ID: "a", Addr: srv.Host, Port: srv.Port, User: "tester", Auth: config.AuthAgent}
}

// agentOptions log in with the agent at addr, trusting every host key.
func agentOptions(t *testing.T, addr string) Options {
	var asked []string
	opts := testOptions(t, "", &asked)
	opts.Agent = addr
	return opts
}

// TestAgentLogin logs in with the agent's key, and closes the agent's
// connection once logged in.
func TestAgentLogin(t *testing.T) {
	srv, ag := sshtest.Start(t), sshtest.StartAgent(t)
	srv.AcceptKey(ag.AddKey(t, "prod-servers"))
	client, err := Dial(agentHost(srv), agentOptions(t, ag.Addr))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if n := len(srv.KeyLogins()); n != 1 || srv.Logins() != 0 {
		t.Errorf("%d key logins, %d with a password", n, srv.Logins())
	}
	waitFor(t, "the agent's connection closed", func() bool { return ag.Open() == 0 })
	if ag.Conns() != 1 || ag.Signs() != 1 {
		t.Errorf("%d connections, %d signatures", ag.Conns(), ag.Signs())
	}
}

// TestAgentSecondKey tries the agent's keys in turn: the server takes
// the second, and only that one is signed with.
func TestAgentSecondKey(t *testing.T) {
	srv, ag := sshtest.Start(t), sshtest.StartAgent(t)
	ag.AddKey(t, "github")
	second := ag.AddKey(t, "prod-servers")
	ag.AddKey(t, "third")
	srv.AcceptKey(second)
	client, err := Dial(agentHost(srv), agentOptions(t, ag.Addr))
	if err != nil {
		t.Fatal(err)
	}
	client.Close()
	if got := srv.KeyLogins(); !slices.Equal(got, []string{ssh.FingerprintSHA256(second)}) {
		t.Errorf("logged in with %q", got)
	}
	if ag.Signs() != 1 {
		t.Errorf("%d signatures, want 1: a key is signed with once the server would take it", ag.Signs())
	}
}

// TestAgentErrors: no agent at the address, none set, an empty agent,
// every key refused and a signature denied each fail with their error,
// which names the address.
func TestAgentErrors(t *testing.T) {
	srv, ag := sshtest.Start(t), sshtest.StartAgent(t)
	h := agentHost(srv)

	missing := filepath.Join(filepath.Dir(ag.Addr), "none.sock")
	_, err := Dial(h, agentOptions(t, missing))
	var ae *AgentError
	if !errors.As(err, &ae) || ae.Addr != missing || errors.Is(err, ErrAgentNoKeys) {
		t.Errorf("no agent: %v", err)
	}

	t.Setenv("SSH_AUTH_SOCK", "")
	if DefaultAgentAddr() == "" { // not on Windows, whose default is a pipe
		_, err = Dial(h, agentOptions(t, ""))
		if !errors.Is(err, ErrNoAgentAddr) || !errors.As(err, &ae) || ae.Addr != "" {
			t.Errorf("no address: %v", err)
		}
	}

	_, err = Dial(h, agentOptions(t, ag.Addr))
	if !errors.Is(err, ErrAgentNoKeys) || !errors.As(err, &ae) || ae.Addr != ag.Addr {
		t.Errorf("an empty agent: %v", err)
	}

	ag.AddKey(t, "not on the server")
	ag.AddKey(t, "nor this one")
	_, err = Dial(h, agentOptions(t, ag.Addr))
	if !errors.Is(err, ErrAgentRefused) {
		t.Errorf("refused keys: %v", err)
	}

	srv.AcceptKey(ag.AddKey(t, "denied"))
	ag.DenySigning()
	_, err = Dial(h, agentOptions(t, ag.Addr))
	if !errors.Is(err, ErrAgentSign) || !errors.As(err, &ae) || ae.Addr != ag.Addr {
		t.Errorf("signing denied: %v", err)
	}
	waitFor(t, "the agent's connections closed", func() bool { return ag.Open() == 0 })
}

// TestAgentDefaultAddr uses SSH_AUTH_SOCK when no address is set, and
// expands ~ in one that is.
func TestAgentDefaultAddr(t *testing.T) {
	srv, ag := sshtest.Start(t), sshtest.StartAgent(t)
	srv.AcceptKey(ag.AddKey(t, "k"))
	t.Setenv("SSH_AUTH_SOCK", ag.Addr)
	client, err := Dial(agentHost(srv), agentOptions(t, " "))
	if err != nil {
		t.Fatal(err)
	}
	client.Close()

	t.Setenv("HOME", filepath.Dir(ag.Addr))
	t.Setenv("SSH_AUTH_SOCK", "")
	client, err = Dial(agentHost(srv), agentOptions(t, "~/"+filepath.Base(ag.Addr)))
	if err != nil {
		t.Fatal(err)
	}
	client.Close()
}

// TestAgentJumpHost logs in to a jump host and the host behind it, each
// with the agent, on a connection of its own.
func TestAgentJumpHost(t *testing.T) {
	j, target, ag := sshtest.Start(t), sshtest.Start(t), sshtest.StartAgent(t)
	key := ag.AddKey(t, "k")
	target.AcceptKey(key)
	j.AcceptKey(key)
	opts := agentOptions(t, ag.Addr)
	jh := &config.Host{ID: "j", Name: "jump-1", Addr: j.Host, Port: j.Port, User: "hop", Auth: config.AuthAgent}
	opts.Jump = &Hop{Host: jh, Options: agentOptions(t, ag.Addr)}
	opts.Jump.Options.KnownHosts = opts.KnownHosts
	h := agentHost(target)
	h.Proxy = &config.ProxyConfig{Kind: config.ProxyJump, JumpHost: "j"}
	client, err := Dial(h, opts)
	if err != nil {
		t.Fatal(err)
	}
	client.Close()
	if len(j.KeyLogins()) != 1 || len(target.KeyLogins()) != 1 {
		t.Errorf("key logins: jump host %d, host %d", len(j.KeyLogins()), len(target.KeyLogins()))
	}
	if ag.Conns() != 2 {
		t.Errorf("%d connections to the agent, want one each", ag.Conns())
	}

	// The jump host refusing the keys says so, naming it.
	j2 := sshtest.Start(t)
	opts.Jump.Host = &config.Host{ID: "j2", Name: "jump-2", Addr: j2.Host, Port: j2.Port, User: "hop", Auth: config.AuthAgent}
	_, err = Dial(h, opts)
	var hop *HopError
	if !errors.As(err, &hop) || hop.JumpHost != "jump-2" || !errors.Is(err, ErrAgentRefused) {
		t.Errorf("refused at the jump host: %v", err)
	}
}

// TestListAgentKeys lists the keys as ssh-add -l does.
func TestListAgentKeys(t *testing.T) {
	ag := sshtest.StartAgent(t)
	addr, keys, err := ListAgentKeys(ag.Addr)
	if err != nil || addr != ag.Addr || len(keys) != 0 {
		t.Fatalf("empty: %q %v %v", addr, keys, err)
	}
	ed := ag.AddKey(t, "prod-servers")
	rsa := ag.AddRSAKey(t, 2048, "legacy-box")
	_, keys, err = ListAgentKeys(ag.Addr)
	if err != nil {
		t.Fatal(err)
	}
	want := []AgentKey{
		{Type: "ED25519", Fingerprint: ssh.FingerprintSHA256(ed), Comment: "prod-servers"},
		{Type: "RSA 2048", Fingerprint: ssh.FingerprintSHA256(rsa), Comment: "legacy-box"},
	}
	if !slices.Equal(keys, want) {
		t.Errorf("keys %+v, want %+v", keys, want)
	}
	if ag.Signs() != 0 {
		t.Error("listing asked for a signature")
	}
	waitFor(t, "the agent's connections closed", func() bool { return ag.Open() == 0 })

	_, _, err = ListAgentKeys(filepath.Join(os.TempDir(), "no-such-agent.sock"))
	var ae *AgentError
	if !errors.As(err, &ae) || !strings.HasSuffix(ae.Addr, "no-such-agent.sock") {
		t.Errorf("no agent: %v", err)
	}
}
