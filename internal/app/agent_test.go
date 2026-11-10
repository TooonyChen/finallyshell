package app

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"finallyshell/internal/config"
	"finallyshell/internal/sshtest"
	"finallyshell/internal/sshx"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
	"golang.org/x/crypto/ssh"
)

// testAgent is an agent with the keys of the mockup, whose second the
// server takes, at the address of a's settings.
func testAgent(t *testing.T, a *App) (*sshtest.Agent, ssh.PublicKey) {
	ag := sshtest.StartAgent(t)
	ag.AddKey(t, "prod-servers")
	key := ag.AddKey(t, "github")
	ag.AddRSAKey(t, 2048, "legacy-box")
	a.settings.SSHAgent = ag.Addr
	return ag, key
}

// TestAgentEditor chooses SSH agent in the editor: the key file,
// passphrase, password and "Remember" go, Check lists the agent's keys,
// and OK saves the host with nothing to remember.
func TestAgentEditor(t *testing.T) {
	a, p := testApp(t)
	ag, _ := testAgent(t, a)
	a.newConnection()
	tt := editorWindow(t, a, p)
	typeInto(t, tt, locales.FieldName(), "web-1")
	typeInto(t, tt, locales.FieldHost(), "45.56.78.90")
	if err := tt.Click(locales.AuthPassword()); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(locales.AuthAgent()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.editor.auth != config.AuthAgent {
		t.Fatalf("auth %q", a.editor.auth)
	}
	for _, gone := range []string{locales.FieldKeyFile(), locales.FieldPassphrase(), locales.FieldPassword(), locales.RememberPassword(), locales.RememberPassphrase()} {
		if slices.Contains(tt.Texts(), gone) {
			t.Errorf("%q shows with SSH agent; texts %q", gone, tt.Texts())
		}
	}
	for _, s := range []string{locales.FieldAgent(), locales.AgentUsesOptions(), locales.AgentEditorNote()} {
		if !tt.HasText(s) {
			t.Errorf("no %q; texts %q", s, tt.Texts())
		}
	}

	if err := tt.Click(locales.AgentCheckLabel()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the agent's keys", func() bool { return a.editor.agentCheck.done })
	tt.Frame()
	for _, s := range []string{locales.AgentConnectedAt(3, ag.Addr), "ED25519", "RSA 2048", "prod-servers", "github", "legacy-box", locales.AgentTriedInTurn()} {
		if !tt.HasText(s) {
			t.Errorf("no %q; texts %q", s, tt.Texts())
		}
	}
	if ag.Signs() != 0 {
		t.Error("checking asked the agent to sign")
	}
	tt.SetSize(680, 780) // the whole page, for the screenshots
	tt.Frame()
	saveShot(t, tt, "editor-agent-en.png")
	p.SetLocale("zh-Hans")
	tt.Frame()
	for _, s := range []string{"SSH Agent", "使用 选项 › 常规 里的地址", "检测", "有 3 把私钥", "连接时依次尝试，直到服务器接受其中一把。"} {
		if !tt.HasText(s) && !strings.Contains(strings.Join(tt.Texts(), "\n"), s) {
			t.Errorf("no %q in Chinese; texts %q", s, tt.Texts())
		}
	}
	saveShot(t, tt, "editor-agent-zh-Hans.png")
	p.SetLocale("")

	a.passwords["stale"] = "x"
	if err := tt.Click(locales.Ok()); err != nil {
		t.Fatal(err)
	}
	if len(a.hosts.Hosts) != 1 {
		t.Fatalf("not saved; texts %q", tt.Texts())
	}
	h := a.hosts.Hosts[0]
	if h.Auth != config.AuthAgent || h.KeyPath != "" {
		t.Errorf("host %+v", h)
	}
	if _, ok := a.passwords[h.ID]; ok {
		t.Error("a secret kept for an agent host")
	}

	// Opened again, it is still an agent host.
	a.editConnection(h)
	tt = editorWindow(t, a, p)
	if a.editor.auth != config.AuthAgent || !tt.HasText(locales.AgentUsesOptions()) {
		t.Errorf("reopened as %q", a.editor.auth)
	}
}

// TestAgentEditorFromPassword turns a password host into an agent one:
// its stored password is deleted.
func TestAgentEditorFromPassword(t *testing.T) {
	a, p := testApp(t)
	h := &config.Host{ID: "h", Addr: "example.com", User: "root", Auth: config.AuthPassword}
	a.hosts.Add(h)
	a.passwords["h"] = "pw"
	p.SecretSet(hostSecretKey("h", secretPassword), "pw")
	a.editConnection(h)
	tt := editorWindow(t, a, p)
	if err := tt.Click(locales.AuthPassword()); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(locales.AuthAgent()); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(locales.Ok()); err != nil {
		t.Fatal(err)
	}
	if h.Auth != config.AuthAgent {
		t.Fatalf("auth %q", h.Auth)
	}
	p.until(t, "the secret deleted", func() bool {
		_, ok, _ := p.SecretGet(hostSecretKey("h", secretPassword))
		return !ok && a.secrets.queue.idle()
	})
	if _, ok := a.passwords["h"]; ok {
		t.Error("password kept")
	}
}

// TestAgentOption sets the agent's address on the General page, saved
// for this computer only, and Check lists its keys.
func TestAgentOption(t *testing.T) {
	a, p := testApp(t)
	ag, _ := testAgent(t, a)
	a.settings.SSHAgent = ""
	a.openOptions()
	tt := p.show(t, WindowOptions)
	for _, s := range []string{locales.OptionSSHAgent(), locales.OptionSSHAgentNote(), "~/.bitwarden-ssh-agent.sock", locales.OptionThisComputerOnly()} {
		if !tt.HasText(s) {
			t.Errorf("no %q; texts %q", s, tt.Texts())
		}
	}
	typeInto(t, tt, locales.OptionSSHAgent(), ag.Addr)
	if a.settings.SSHAgent != ag.Addr || config.LoadSettings().SSHAgent != ag.Addr {
		t.Fatalf("address %q, saved %q", a.settings.SSHAgent, config.LoadSettings().SSHAgent)
	}
	if err := tt.Click(locales.AgentCheckLabel()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the agent's keys", func() bool { return a.agentOptionCheck.done })
	tt.Frame()
	if !tt.HasText(locales.AgentConnectedAt(3, ag.Addr)) || !tt.HasText("legacy-box") {
		t.Errorf("texts %q", tt.Texts())
	}
	// Changing the address forgets what was found.
	typeInto(t, tt, locales.OptionSSHAgent(), ag.Addr+"x")
	if a.agentOptionCheck.done || tt.HasText("legacy-box") {
		t.Error("the check stays after the address changed")
	}
	if err := tt.Click(locales.AgentCheckLabel()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the check", func() bool { return a.agentOptionCheck.done })
	tt.Frame()
	if want := locales.ErrAgentNotFound(ag.Addr+"x", ""); !strings.Contains(strings.Join(tt.Texts(), "\n"), strings.TrimSuffix(want, ": ")) {
		t.Errorf("no %q; texts %q", want, tt.Texts())
	}

	// Empty again: the default, which the placeholder says.
	typeInto(t, tt, locales.OptionSSHAgent(), "")
	if a.settings.SSHAgent != "" {
		t.Errorf("address %q; texts %q", a.settings.SSHAgent, tt.Texts())
	}
	tt.SetSize(760, 1060) // the whole page, for the screenshots
	tt.Frame()
	saveShot(t, tt, "options-agent-en.png")
	p.SetLocale("zh-Hans")
	tt.Frame()
	for _, s := range []string{"SSH Agent 地址", "检测", "程序", "只对这台电脑生效，不同步。"} {
		if !tt.HasText(s) {
			t.Errorf("no %q in Chinese", s)
		}
	}
	saveShot(t, tt, "options-agent-zh-Hans.png")
}

// TestAgentLogin connects to a host with the agent's keys, the server
// taking the second, without asking for a password; and through a jump
// host that logs in with the agent too.
func TestAgentLogin(t *testing.T) {
	srv, jump := sshtest.Start(t), sshtest.Start(t)
	a, p := testApp(t)
	t.Cleanup(a.CloseAll)
	ag, key := testAgent(t, a)
	srv.AcceptKey(key)
	jump.AcceptKey(key)
	h := &config.Host{ID: "h", Name: "web-1", Addr: srv.Host, Port: srv.Port, User: "tester", Auth: config.AuthAgent, NoMonitor: true}
	a.hosts.Add(h)
	if need := a.missingSecret(h); need != nil {
		t.Fatalf("an agent host needs %+v", need)
	}
	a.openSession(h)
	s := a.current()
	if a.pwOpen {
		t.Fatal("asked for a password")
	}
	if answerHostKeys(t, p, s); s.status != statusConnected {
		t.Fatal(describeError(s.err))
	}
	// The server records the login after its side of the handshake,
	// which may end after the client's.
	p.until(t, "the server's key login", func() bool { return len(srv.KeyLogins()) == 1 })
	if got := srv.KeyLogins(); len(got) != 1 || got[0] != ssh.FingerprintSHA256(key) || srv.Logins() != 0 {
		t.Errorf("logged in with %q, %d passwords", got, srv.Logins())
	}
	p.until(t, "the agent's connection closed", func() bool { return ag.Open() == 0 })

	// Through a jump host that logs in with the agent.
	j := &config.Host{ID: "j", Name: "jump-1", Addr: jump.Host, Port: jump.Port, User: "hop", Auth: config.AuthAgent, NoMonitor: true}
	a.hosts.Add(j)
	h.Proxy = jumpTo("j")
	if need := a.missingSecret(h); need != nil {
		t.Fatalf("an agent jump host needs %+v", need)
	}
	a.reconnect(s)
	if a.pwOpen {
		t.Fatal("asked for a password")
	}
	if answerHostKeys(t, p, s); s.status != statusConnected {
		t.Fatal(describeError(s.err))
	}
	p.until(t, "the servers' key logins", func() bool { return len(jump.KeyLogins()) == 1 && len(srv.KeyLogins()) == 2 })
	if len(jump.KeyLogins()) != 1 || len(srv.KeyLogins()) != 2 {
		t.Errorf("key logins: jump host %d, host %d", len(jump.KeyLogins()), len(srv.KeyLogins()))
	}
}

// TestAgentLoginErrors: an empty agent, no agent, and keys the server
// refuses fail with their error and a button to the setting, asking for
// no password.
func TestAgentLoginErrors(t *testing.T) {
	srv := sshtest.Start(t)
	a, p := testApp(t)
	t.Cleanup(a.CloseAll)
	mainWin := ui.NewTester(a.View, 1280, 820)
	p.tt = mainWin
	ag := sshtest.StartAgent(t)
	a.settings.SSHAgent = ag.Addr
	h := &config.Host{ID: "h", Name: "web-1", Addr: srv.Host, Port: srv.Port, User: "tester", Auth: config.AuthAgent, NoMonitor: true}
	a.hosts.Add(h)

	a.openSession(h)
	s := a.current()
	answerHostKeys(t, p, s)
	mainWin.Frame()
	if s.status != statusFailed || !errors.Is(s.err, sshx.ErrAgentNoKeys) || a.pwOpen {
		t.Fatalf("empty agent: status %d, %v, prompt %v", s.status, s.err, a.pwOpen)
	}
	if !mainWin.HasText(locales.ErrAgentNoKeys(ag.Addr)) || !mainWin.HasText(locales.AgentSettings()) {
		t.Errorf("texts %q", mainWin.Texts())
	}
	saveShot(t, mainWin, "agent-error-en.png")
	p.SetLocale("zh-Hans")
	mainWin.Frame()
	if !mainWin.HasText("SSH Agent 里没有私钥（地址："+ag.Addr+"）") || !mainWin.HasText("SSH Agent 设置") {
		t.Errorf("in Chinese: %q", mainWin.Texts())
	}
	saveShot(t, mainWin, "agent-error-zh-Hans.png")
	p.SetLocale("")
	mainWin.Frame()

	// The button opens the General page.
	a.optionsNav.Page = pageTerminal
	if err := mainWin.Click(locales.AgentSettings()); err != nil {
		t.Fatal(err)
	}
	if !a.windowOpen(WindowOptions) || a.optionsNav.Page != pageGeneral {
		t.Errorf("options open %v at %q", a.windowOpen(WindowOptions), a.optionsNav.Page)
	}

	// Keys the server refuses.
	ag.AddKey(t, "not on the server")
	a.reconnect(s)
	answerHostKeys(t, p, s)
	mainWin.Frame()
	if !errors.Is(s.err, sshx.ErrAgentRefused) || a.pwOpen || !mainWin.HasText(locales.ErrAgentRefused()) {
		t.Errorf("refused: %v, prompt %v; texts %q", s.err, a.pwOpen, mainWin.Texts())
	}

	// No agent at the address.
	a.settings.SSHAgent = ag.Addr + ".gone"
	a.reconnect(s)
	answerHostKeys(t, p, s)
	mainWin.Frame()
	var ae *sshx.AgentError
	if !errors.As(s.err, &ae) || ae.Addr != ag.Addr+".gone" || a.pwOpen || !strings.HasPrefix(describeError(s.err), "No SSH agent at "+ag.Addr+".gone: ") {
		t.Errorf("no agent: %v, prompt %v: %q", s.err, a.pwOpen, describeError(s.err))
	}
	p.SetLocale("zh-Hans")
	if got := describeError(s.err); !strings.HasPrefix(got, "找不到 SSH Agent（地址："+ag.Addr+".gone）") {
		t.Errorf("in Chinese: %q", got)
	}
}

// TestAgentNames names the agents whose address tells, and shortens
// fingerprints for the Check's list.
func TestAgentNames(t *testing.T) {
	for addr, want := range map[string]string{
		"~/.bitwarden-ssh-agent.sock":                                      "Bitwarden",
		"~/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock": "1Password",
		`\\.\pipe\openssh-ssh-agent`:                                       "",
		"/tmp/ssh-XXXX/agent.123":                                          "",
	} {
		if got := agentName(addr); got != want {
			t.Errorf("agentName(%q) = %q, want %q", addr, got, want)
		}
	}
	if got := shortFingerprint("SHA256:8f2cAbCdEfGhIjKlMnOpQrStUvWxYz0123456789Qw"); got != "SHA256:8f2cAb…89Qw" {
		t.Errorf("short fingerprint %q", got)
	}
	if got := shortFingerprint("MD5:aa:bb"); got != "MD5:aa:bb" {
		t.Errorf("not SHA256: %q", got)
	}
}
