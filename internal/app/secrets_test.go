package app

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"finalshelly/internal/config"
	"finalshelly/internal/sshx"
	"finalshelly/locales"

	"github.com/egoist/mygo/ui"
	"golang.org/x/crypto/ssh"
)

// fakeSecrets stands in for the system's credential store, which the app
// calls on a goroutine. Tests that restart the app hand the same one to
// the new platform.
type fakeSecrets struct {
	mu      sync.Mutex
	stored  map[string]string
	down    bool          // the system has no store
	fail    error         // what every call fails with, as a user denying access
	gate    chan struct{} // when set, calls wait until it is closed
	calls   int
	waiting int // calls waiting at the gate
}

func newFakeSecrets() *fakeSecrets { return &fakeSecrets{stored: map[string]string{}} }

func (f *fakeSecrets) enter() error {
	f.mu.Lock()
	f.calls++
	gate := f.gate
	if gate != nil {
		f.waiting++
	}
	f.mu.Unlock()
	if gate != nil {
		<-gate
		f.mu.Lock()
		f.waiting--
		f.mu.Unlock()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return errors.ErrUnsupported
	}
	return f.fail
}

func (f *fakeSecrets) SecretGet(key string) (string, bool, error) {
	if err := f.enter(); err != nil {
		return "", false, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.stored[key]
	return v, ok, nil
}

func (f *fakeSecrets) SecretSet(key, value string) error {
	if err := f.enter(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stored[key] = value
	return nil
}

func (f *fakeSecrets) SecretDelete(key string) error {
	if err := f.enter(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.stored, key)
	return nil
}

// secrets is what the store keeps now.
func (f *fakeSecrets) secrets() map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return maps.Clone(f.stored)
}

func (f *fakeSecrets) put(key, value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stored[key] = value
}

// callCount is how many calls the store had.
func (f *fakeSecrets) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// atGate is how many calls wait at the gate.
func (f *fakeSecrets) atGate() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.waiting
}

// settled tells whether s connected or failed, trusting the server's key
// when asked.
func settled(s *Session) func() bool {
	return func() bool {
		if s == nil {
			return false
		}
		if s.hostKeyQ != nil {
			s.hostKeyQ.Answer <- true
			s.hostKeyQ = nil
		}
		return s.status == statusConnected || s.status == statusFailed
	}
}

// A password typed in the editor is saved in the store, not in
// hosts.json, and the next run connects with it without asking.
func TestRememberedPasswordSurvivesRestart(t *testing.T) {
	srv := startFakeSSH(t)
	a, p := testApp(t)
	a.saveHosts = config.SaveHosts
	a.newConnection()
	tt := editorWindow(t, a, p)
	typeInto(t, tt, locales.FieldHost(), srv.host)
	typeInto(t, tt, locales.FieldPort(), strconv.Itoa(srv.port))
	typeInto(t, tt, locales.FieldUser(), "u")
	typeInto(t, tt, locales.FieldPassword(), "secret")
	p.until(t, "the store checked", func() bool { return a.secrets.state == storeUp })
	tt.Frame()
	if box, ok := tt.AccessByRole(ui.RoleCheckBox, locales.RememberPassword()); !ok || !box.Checked || box.Disabled {
		t.Errorf("remember box %+v", box)
	}
	if !tt.HasText(locales.SecretStoredNote(storeName())) {
		t.Errorf("no note; texts %q", tt.Texts())
	}
	saveShot(t, tt, "editor-remember-en.png")
	p.SetLocale("zh-Hans")
	tt.Frame()
	if !tt.HasText("记住密码") {
		t.Errorf("Chinese: texts %q", tt.Texts())
	}
	saveShot(t, tt, "editor-remember-zh.png")
	p.SetLocale("")
	if err := tt.Click(locales.Ok()); err != nil {
		t.Fatal(err)
	}
	h := a.hosts.Live()[0]
	key := hostSecretKey(h.ID, secretPassword)
	p.until(t, "the password saved", func() bool { return p.secrets()[key] == "secret" })
	if data, err := os.ReadFile(filepath.Join(config.Dir(), "hosts.json")); err != nil || strings.Contains(string(data), "secret") {
		t.Errorf("hosts.json %s: %v", data, err)
	}

	// The next run, with the same store.
	p2 := newFakePlatform(t)
	p2.fakeSecrets = p.fakeSecrets
	b := New(p2)
	t.Cleanup(b.CloseAll)
	b.openSession(b.hosts.Host(h.ID))
	p2.until(t, "the connection", func() bool { return b.pwOpen || settled(b.current())() })
	if s := b.current(); b.pwOpen || s.status != statusConnected {
		t.Fatalf("prompt %v, session %+v", b.pwOpen, s)
	}
	if b.passwords[h.ID] != "secret" {
		t.Errorf("password %q", b.passwords[h.ID])
	}
}

// Unchecking "Remember password" removes it from the store and keeps it
// in memory; the host keeps the choice.
func TestUncheckRememberDeletesSecret(t *testing.T) {
	a, p := testApp(t)
	saved := savedHosts(a)
	h := &config.Host{ID: "h1", Addr: "10.0.0.1", User: "root", Auth: config.AuthPassword}
	a.hosts.Add(h)
	key := hostSecretKey("h1", secretPassword)
	p.put(key, "stored")
	a.editConnection(h)
	tt := editorWindow(t, a, p)
	p.until(t, "the saved password read", func() bool { return !a.editor.readingSecret })
	if a.editor.password != "stored" || !a.editor.remember {
		t.Fatalf("editor password %q remember %v", a.editor.password, a.editor.remember)
	}
	if err := tt.Click(locales.RememberPassword()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText(locales.SecretMemoryNote()) {
		t.Errorf("no memory note; texts %q", tt.Texts())
	}
	saveShot(t, tt, "editor-memory-only-en.png")
	if err := tt.Click(locales.Ok()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the password deleted", func() bool { _, ok := p.secrets()[key]; return !ok })
	if h.RemembersPassword() || !strings.Contains(string(*saved), `"noRememberPassword":true`) || a.passwords["h1"] != "stored" {
		t.Errorf("host %+v, saved %s, password %q", h, *saved, a.passwords["h1"])
	}

	// The editor shows the choice again, and checking it saves it back.
	a.editConnection(h)
	tt = editorWindow(t, a, p)
	if box, _ := tt.AccessByRole(ui.RoleCheckBox, locales.RememberPassword()); box.Checked || a.editor.password != "stored" {
		t.Errorf("remember box %+v, password %q", box, a.editor.password)
	}
	if err := tt.Click(locales.RememberPassword()); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(locales.Ok()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the password saved", func() bool { return p.secrets()[key] == "stored" })
	if !h.RemembersPassword() {
		t.Error("remember not saved")
	}
}

// A saved password the server refuses fails with the auth error and
// opens the prompt, whose password, remembered, replaces it.
func TestWrongSavedPasswordAsksAgain(t *testing.T) {
	srv := startFakeSSH(t)
	a, p := testApp(t)
	t.Cleanup(a.CloseAll)
	h := &config.Host{ID: "h1", Name: "web", Addr: srv.host, Port: srv.port, User: "u", Auth: config.AuthPassword}
	a.hosts.Add(h)
	key := hostSecretKey("h1", secretPassword)
	p.put(key, "outdated")
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	a.openSession(h)
	p.until(t, "the prompt", func() bool { settled(a.current())(); return a.pwOpen })
	s := a.current()
	if s.status != statusFailed || describeError(s.err) != locales.ErrAuthFailed() || a.pwNeed.store != key {
		t.Fatalf("session %d %v, prompt for %+v", s.status, s.err, a.pwNeed)
	}
	tt.Frame()
	if !tt.HasText(locales.ErrAuthFailed()) || !tt.HasText(locales.SecretStoredNote(storeName())) {
		t.Errorf("prompt; texts %q", tt.Texts())
	}
	if box, ok := tt.AccessByRole(ui.RoleCheckBox, locales.RememberPassword()); !ok || !box.Checked {
		t.Errorf("remember box %+v", box)
	}
	saveShot(t, tt, "prompt-en.png")
	p.SetLocale("zh-Hans")
	tt.Frame()
	saveShot(t, tt, "prompt-zh.png")
	p.SetLocale("")
	tt.Frame()

	// A typo is refused too, and not stored: the saved one stays.
	tt.Type("typo")
	if err := tt.Click(locales.Connect()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the prompt again", func() bool { return a.pwOpen && s.status == statusFailed })
	p.until(t, "the store's calls", a.secrets.queue.idle)
	if got := p.secrets()[key]; got != "outdated" || a.pwErr == nil {
		t.Fatalf("stored %q after a typo, prompt err %v", got, a.pwErr)
	}

	tt.Frame()
	tt.Type("secret")
	if err := tt.Click(locales.Connect()); err != nil {
		t.Fatal(err)
	}
	if got := p.secrets()[key]; got != "outdated" {
		t.Errorf("stored %q before logging in", got)
	}
	p.until(t, "the connection", settled(s))
	if s.status != statusConnected || len(a.sessions) != 1 || a.pwOpen {
		t.Fatalf("status %d err %v, %d sessions", s.status, s.err, len(a.sessions))
	}
	p.until(t, "the password replaced", func() bool { return p.secrets()[key] == "secret" })
}

// A key's passphrase that is missing or wrong opens the prompt, for the
// passphrase, which reconnects the same session and, remembered, is
// stored once the key logs in.
func TestPassphrasePrompt(t *testing.T) {
	srv := startFakeSSH(t)
	a, p := testApp(t)
	t.Cleanup(a.CloseAll)
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(priv)
	srv.authorize(signer.PublicKey())
	block, err := ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte("phrase"))
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(t.TempDir(), "id_ed25519")
	os.WriteFile(keyPath, pem.EncodeToMemory(block), 0o600)
	h := &config.Host{ID: "h1", Name: "web", Addr: srv.host, Port: srv.port, User: "u", Auth: config.AuthKey, KeyPath: keyPath, NoMonitor: true}
	a.hosts.Add(h)
	key := hostSecretKey("h1", secretPassphrase)
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt

	// None stored: the session fails at once, asking for it.
	a.openSession(h)
	s := a.current()
	p.until(t, "the prompt", func() bool { return a.pwOpen })
	if a.pwNeed.store != key || !a.pwNeed.passphrase || !errors.Is(a.pwErr, sshx.ErrNeedPassphrase) || !a.pwRemember {
		t.Fatalf("prompt for %+v, err %v", a.pwNeed, a.pwErr)
	}
	tt.Frame()
	for _, want := range []string{locales.EnterPassphrase(), locales.RememberPassphrase(), locales.ErrNeedPassphrase()} {
		if !tt.HasText(want) {
			t.Errorf("no %q; texts %q", want, tt.Texts())
		}
	}

	// A wrong one asks again, saying so, and is not stored.
	tt.Type("wrong")
	if err := tt.Click(locales.Connect()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the prompt again", func() bool { return a.pwOpen && a.pwErr != nil && !errors.Is(a.pwErr, sshx.ErrNeedPassphrase) })
	tt.Frame()
	if !tt.HasText(locales.ErrWrongPassphrase()) {
		t.Errorf("no wrong passphrase; texts %q", tt.Texts())
	}
	saveShot(t, tt, "prompt-passphrase-en.png")
	p.SetLocale("zh-Hans")
	tt.Frame()
	if !tt.HasText("输入私钥密码") || !tt.HasText("私钥密码不正确") {
		t.Errorf("Chinese: texts %q", tt.Texts())
	}
	saveShot(t, tt, "prompt-passphrase-zh.png")
	p.SetLocale("")
	tt.Frame()
	p.until(t, "the store's calls", a.secrets.queue.idle)
	if _, ok := p.secrets()[key]; ok {
		t.Fatalf("a wrong passphrase stored: %v", p.secrets())
	}

	tt.Type("phrase")
	if err := tt.Click(locales.Connect()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the connection", settled(s))
	if s.status != statusConnected || len(a.sessions) != 1 || a.pwOpen {
		t.Fatalf("status %d err %v, %d sessions", s.status, s.err, len(a.sessions))
	}
	p.until(t, "the passphrase stored", func() bool { return p.secrets()[key] == "phrase" })
}

// The prompt's "Remember password", unchecked, keeps the password out of
// the store, and the host keeps the choice.
func TestPromptWithoutRemember(t *testing.T) {
	a, p := testApp(t)
	saved := savedHosts(a)
	t.Cleanup(a.CloseAll)
	// A port nothing listens on, to fail at once.
	h := &config.Host{ID: "h1", Addr: "127.0.0.1", Port: 1, User: "u", Auth: config.AuthPassword}
	a.hosts.Add(h)
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	a.openSession(h)
	p.until(t, "the prompt", func() bool { return a.pwOpen })
	if a.pwErr != nil || !a.pwRemember {
		t.Errorf("prompt err %v remember %v", a.pwErr, a.pwRemember)
	}
	tt.Frame()
	tt.Type("typed")
	if err := tt.Click(locales.RememberPassword()); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(locales.Connect()); err != nil {
		t.Fatal(err)
	}
	if a.pwOpen || len(a.sessions) != 1 || a.passwords["h1"] != "typed" {
		t.Fatalf("prompt %v, %d sessions, password %q", a.pwOpen, len(a.sessions), a.passwords["h1"])
	}
	p.until(t, "the store's calls", a.secrets.queue.idle)
	if len(p.secrets()) != 0 || h.RemembersPassword() || !strings.Contains(string(*saved), `"noRememberPassword":true`) {
		t.Errorf("stored %v, host %+v", p.secrets(), h)
	}

	// Connecting again reads nothing from the store: the host does not
	// remember its password.
	delete(a.passwords, "h1")
	a.pwOpen = false
	calls := p.callCount()
	a.openSession(h)
	if !a.pwOpen || p.callCount() != calls {
		t.Errorf("prompt %v, %d calls", a.pwOpen, p.callCount()-calls)
	}
}

// Without a credential store, the editor and the prompt say so once, and
// passwords stay in memory.
func TestSecretsUnavailable(t *testing.T) {
	a, p := testApp(t)
	t.Cleanup(a.CloseAll)
	p.down = true
	a.newConnection()
	tt := editorWindow(t, a, p)
	p.until(t, "the store checked", a.secretsUnavailable)
	tt.Frame()
	note := locales.SecretsUnavailable(storeName())
	if !tt.HasText(note) || tt.HasText(locales.SecretStoredNote(storeName())) {
		t.Errorf("no note; texts %q", tt.Texts())
	}
	if box, ok := tt.AccessByRole(ui.RoleCheckBox, locales.RememberPassword()); !ok || box.Checked || !box.Disabled {
		t.Errorf("remember box %+v", box)
	}
	saveShot(t, tt, "editor-unavailable-en.png")
	p.SetLocale("zh-Hans")
	tt.Frame()
	if !tt.HasText(locales.SecretsUnavailable(storeName())) {
		t.Errorf("Chinese: texts %q", tt.Texts())
	}
	saveShot(t, tt, "editor-unavailable-zh.png")
	p.SetLocale("")
	tt.Frame()
	typeInto(t, tt, locales.FieldHost(), "127.0.0.1")
	typeInto(t, tt, locales.FieldPassword(), "pw")
	calls := p.callCount()
	if err := tt.Click(locales.Ok()); err != nil {
		t.Fatal(err)
	}
	h := a.hosts.Live()[0]
	if a.passwords[h.ID] != "pw" || !h.RemembersPassword() || p.callCount() != calls {
		t.Errorf("password %q, host %+v, %d calls", a.passwords[h.ID], h, p.callCount()-calls)
	}

	// The prompt asks at once, and says so too.
	other := &config.Host{ID: "h2", Name: "db", Addr: "127.0.0.1", Port: 1, User: "u", Auth: config.AuthPassword}
	a.hosts.Add(other)
	main := ui.NewTester(a.View, 1280, 820)
	p.tt = main
	a.openSession(other)
	if !a.pwOpen {
		t.Fatal("no prompt")
	}
	main.Frame()
	if !main.HasText(note) {
		t.Errorf("prompt; texts %q", main.Texts())
	}
	saveShot(t, main, "prompt-unavailable-en.png")
	p.SetLocale("zh-Hans")
	main.Frame()
	saveShot(t, main, "prompt-unavailable-zh.png")
}

// A store that fails otherwise, as when the user denies access, counts
// as having nothing stored, and its error is logged once.
func TestStoreErrorsLoggedOnce(t *testing.T) {
	var out bytes.Buffer
	log.SetOutput(&out)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	a, p := testApp(t)
	t.Cleanup(a.CloseAll)
	p.fail = errors.New("the user denied access")
	for _, id := range []string{"h1", "h2"} {
		h := &config.Host{ID: id, Addr: "127.0.0.1", Port: 1, User: "u", Auth: config.AuthPassword}
		a.hosts.Add(h)
		a.pwOpen = false
		a.openSession(h)
		p.until(t, "the prompt", func() bool { return a.pwOpen && a.pwNeed.host == h })
	}
	if n := strings.Count(out.String(), "the user denied access"); n != 1 || a.secretsUnavailable() {
		t.Errorf("logged %d times: %q", n, out.String())
	}
}

// Deleting a host for good deletes its secrets, and only its.
func TestDeleteHostForgetsSecrets(t *testing.T) {
	a, p := testApp(t)
	saves := 0
	a.saveHosts = func(*config.Store) error { saves++; return nil }
	gone := &config.Host{ID: "h1", Name: "gone", Addr: "10.0.0.1", User: "root", Auth: config.AuthPassword}
	kept := &config.Host{ID: "h2", Name: "kept", Addr: "10.0.0.2", User: "root", Auth: config.AuthPassword}
	a.hosts.Add(gone)
	a.hosts.Add(kept)
	a.passwords["h1"] = "pw"
	a.proxySecrets()[proxySecretKey("h1")] = "ppw"
	p.put(hostSecretKey("h1", secretPassword), "pw")
	p.put(hostSecretKey("h1", secretPassphrase), "phrase")
	p.put(hostSecretKey("h1", secretProxyPassword), "ppw")
	p.put(hostSecretKey("h2", secretPassword), "pw2")
	// As the manager's "Delete permanently" does.
	a.deleteHostForever(gone)
	p.until(t, "the secrets deleted", func() bool { return len(p.secrets()) == 1 })
	if got := p.secrets(); got[hostSecretKey("h2", secretPassword)] != "pw2" {
		t.Errorf("stored %v", got)
	}
	if a.hosts.Host("h1") != nil || saves != 1 {
		t.Errorf("host %v, %d saves", a.hosts.Host("h1"), saves)
	}
	if _, known := a.passwords["h1"]; known {
		t.Error("password still known")
	}
	if _, known := a.proxySecrets()[proxySecretKey("h1")]; known {
		t.Error("proxy password still known")
	}
}

// Duplicating a host in the manager copies what the store keeps for it,
// its password and its proxy's, to the copy's keys; a host that does not
// remember its passwords copies none.
func TestDuplicateCopiesSecrets(t *testing.T) {
	a, p, tt := openTestManager(t)
	h := a.hosts.Host("web-1")
	h.Auth, h.KeyPath = config.AuthPassword, ""
	h.Proxy = &config.ProxyConfig{Kind: config.ProxySOCKS5, Addr: "198.51.100.7", Port: 1080, User: "alice"}
	p.put(hostSecretKey("web-1", secretPassword), "pw")
	p.put(hostSecretKey("web-1", secretProxyPassword), "ppw")
	rowMenu(t, tt, "web-1", locales.Duplicate())
	i := slices.IndexFunc(a.hosts.Hosts, func(x *config.Host) bool { return x.ID == "web-1" })
	dup := a.hosts.Hosts[i+1]
	p.until(t, "the secrets copied", a.secrets.queue.idle)
	got := p.secrets()
	if got[hostSecretKey(dup.ID, secretPassword)] != "pw" || got[hostSecretKey(dup.ID, secretProxyPassword)] != "ppw" ||
		got[hostSecretKey("web-1", secretPassword)] != "pw" {
		t.Fatalf("stored %v", got)
	}
	if _, ok := got[hostSecretKey(dup.ID, secretPassphrase)]; ok {
		t.Errorf("a passphrase made up: %v", got)
	}
	// The copy connects with them, as any host whose secrets are stored.
	if !a.readChainSecrets(dup, func() {}) {
		t.Error("nothing to read for the copy")
	}
	p.until(t, "the copy's secrets read", func() bool {
		return a.passwords[dup.ID] == "pw" && a.proxySecrets()[proxySecretKey(dup.ID)] == "ppw"
	})

	// Without "Remember password", nothing is copied.
	h.NoRememberPassword = true
	rowMenu(t, tt, "web-1", locales.Duplicate())
	other := a.hosts.Hosts[i+1]
	if other == dup || !other.NoRememberPassword {
		t.Fatalf("second copy %+v", other)
	}
	p.until(t, "the store's calls", a.secrets.queue.idle)
	for k := range p.secrets() {
		if strings.HasPrefix(k, "host/"+other.ID+"/") {
			t.Errorf("copied %s", k)
		}
	}
}

// Through a jump host, the prompt's passwords are stored once the session
// logs in through both, and the next run connects without asking.
func TestRememberedJumpHostPassword(t *testing.T) {
	target := startFakeSSH(t)
	jump := startJumpSSH(t)
	a, p := testApp(t)
	a.saveHosts = config.SaveHosts
	t.Cleanup(a.CloseAll)
	j := &config.Host{ID: "j", Name: "jump-1", Addr: jump.host, Port: jump.port, User: "hop", Auth: config.AuthPassword}
	h := &config.Host{ID: "t", Name: "app-1", Addr: target.host, Port: target.port, User: "u", Auth: config.AuthPassword,
		NoMonitor: true, Proxy: jumpTo("j")}
	a.hosts.Add(j)
	a.hosts.Add(h)
	a.saveHosts(a.hosts)
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt

	a.openSession(h)
	for _, id := range []string{"t", "j"} {
		p.until(t, "the prompt for "+id, func() bool { return a.pwOpen && a.pwNeed.key == id })
		tt.Frame()
		if box, ok := tt.AccessByRole(ui.RoleCheckBox, locales.RememberPassword()); !ok || !box.Checked {
			t.Errorf("%s: remember box %+v", id, box)
		}
		if id == "j" {
			saveShot(t, tt, "prompt-jump-host-en.png")
		}
		tt.Type("secret")
		tt.Key(0, ui.KeyEnter)
	}
	if len(p.secrets()) != 0 {
		t.Errorf("stored before logging in: %v", p.secrets())
	}
	s := a.current()
	answerHostKeys(t, p, s)
	if s.status != statusConnected {
		t.Fatal(describeError(s.err))
	}
	p.until(t, "both stored", func() bool {
		got := p.secrets()
		return got[hostSecretKey("t", secretPassword)] == "secret" && got[hostSecretKey("j", secretPassword)] == "secret"
	})

	// The next run, with the same store: no prompt, and the host keys
	// are known.
	p2 := newFakePlatform(t)
	p2.fakeSecrets = p.fakeSecrets
	b := New(p2)
	t.Cleanup(b.CloseAll)
	b.openSession(b.hosts.Host("t"))
	p2.until(t, "the connection", func() bool { return b.pwOpen || settled(b.current())() })
	if s := b.current(); b.pwOpen || s.status != statusConnected {
		t.Fatalf("prompt %v %+v, session %+v", b.pwOpen, b.pwNeed, s)
	}
}

// Of two jump hosts with the same title, the prompt asks again for the
// password of the one that refused it, told apart by its ID.
func TestRefusedJumpHostOfTwoAlike(t *testing.T) {
	target := startFakeSSH(t)
	inner, outer := startJumpSSH(t), startJumpSSH(t)
	a, p := testApp(t)
	t.Cleanup(a.CloseAll)
	j2 := &config.Host{ID: "j2", Name: "bastion", Addr: outer.host, Port: outer.port, User: "hop", Auth: config.AuthPassword}
	j1 := &config.Host{ID: "j1", Name: "bastion", Addr: inner.host, Port: inner.port, User: "hop", Auth: config.AuthPassword,
		Proxy: jumpTo("j2")}
	h := &config.Host{ID: "t", Name: "app-1", Addr: target.host, Port: target.port, User: "u", Auth: config.AuthPassword,
		NoMonitor: true, Proxy: jumpTo("j1")}
	a.hosts.Add(j2)
	a.hosts.Add(j1)
	a.hosts.Add(h)
	// The outer one, which connects first, is wrong; a title would name
	// the inner one, first on the way from h.
	a.passwords["t"], a.passwords["j1"], a.passwords["j2"] = "secret", "secret", "wrong"
	p.put(hostSecretKey("j2", secretPassword), "wrong")

	a.openSession(h)
	s := a.current()
	answerHostKeys(t, p, s)
	var hop *sshx.HopError
	if s.status != statusFailed || !errors.As(s.err, &hop) || hop.HostID != "j2" {
		t.Fatalf("status %d, err %v", s.status, s.err)
	}
	if !a.pwOpen || a.pwNeed.key != "j2" || a.pwNeed.store != hostSecretKey("j2", secretPassword) {
		t.Fatalf("prompt %v for %+v", a.pwOpen, a.pwNeed)
	}
	if _, known := a.passwords["j1"]; !known {
		t.Error("the inner jump host's password was forgotten")
	}

	// The right one connects the same session, and replaces the stored one.
	a.pwText = "secret"
	a.passwordEntered()
	answerHostKeys(t, p, s)
	if s.status != statusConnected || len(a.sessions) != 1 {
		t.Fatalf("status %d err %v", s.status, s.err)
	}
	p.until(t, "the password replaced", func() bool { return p.secrets()[hostSecretKey("j2", secretPassword)] == "secret" })
}

// A proxy's password is saved from the editor's Proxy page, under the
// host's proxy-password, read before connecting, and deleted with
// "Remember password" off.
func TestProxyPasswordStored(t *testing.T) {
	a, p := testApp(t)
	t.Cleanup(a.CloseAll)
	h := &config.Host{ID: "h1", Addr: "127.0.0.1", Port: 1, User: "u", Auth: config.AuthKey, KeyPath: "k",
		Proxy: &config.ProxyConfig{Kind: config.ProxySOCKS5, Addr: "127.0.0.1", Port: 1, User: "alice"}}
	a.hosts.Add(h)
	key := hostSecretKey("h1", secretProxyPassword)
	a.editConnection(h)
	tt := editorWindow(t, a, p)
	p.until(t, "the store read", func() bool { return !a.editor.readingProxySecret })
	if err := tt.Click(locales.EditorPageProxy()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText(locales.SecretStoredNote(storeName())) {
		t.Errorf("no note; texts %q", tt.Texts())
	}
	typeInto(t, tt, locales.FieldPassword(), "pa55")
	if err := tt.Click(locales.Ok()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the proxy password stored", func() bool { return p.secrets()[key] == "pa55" })

	// The next run reads it before connecting: nothing is asked.
	p2 := newFakePlatform(t)
	p2.fakeSecrets = p.fakeSecrets
	b := New(p2)
	t.Cleanup(b.CloseAll)
	hb := &config.Host{ID: "h1", Addr: "127.0.0.1", Port: 1, User: "u", Auth: config.AuthKey, KeyPath: "k",
		Proxy: &config.ProxyConfig{Kind: config.ProxySOCKS5, Addr: "127.0.0.1", Port: 1, User: "alice"}}
	b.hosts.Add(hb)
	b.openSession(hb)
	p2.until(t, "the session", func() bool { return b.pwOpen || settled(b.current())() })
	if b.pwOpen || b.proxySecrets()[proxySecretKey("h1")] != "pa55" {
		t.Fatalf("prompt %v %+v, proxy password %q", b.pwOpen, b.pwNeed, b.proxySecrets()[proxySecretKey("h1")])
	}

	// Without "Remember password", it leaves the store.
	b.editConnection(hb)
	tt = editorWindow(t, b, p2)
	if err := tt.Click(locales.RememberPassphrase()); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(locales.Ok()); err != nil {
		t.Fatal(err)
	}
	p2.until(t, "the proxy password deleted", func() bool { _, ok := p.secrets()[key]; return !ok })
	if b.proxySecrets()[proxySecretKey("h1")] != "pa55" {
		t.Error("not kept in memory")
	}
}

// The store's calls never block the UI thread, however long the system
// takes to answer.
func TestSecretsOffTheUIThread(t *testing.T) {
	a, p := testApp(t)
	t.Cleanup(a.CloseAll)
	gate := make(chan struct{})
	p.gate = gate
	h := &config.Host{ID: "h1", Addr: "127.0.0.1", Port: 1, User: "u", Auth: config.AuthPassword}
	a.hosts.Add(h)
	a.openSession(h) // returns at once
	a.editConnection(h)
	tt := editorWindow(t, a, p)
	tt.Frame()
	p.until(t, "a call waiting", func() bool { return p.atGate() == 1 })
	if a.pwOpen || len(a.sessions) != 0 || !a.editor.readingSecret {
		t.Fatalf("before the answer: prompt %v, %d sessions", a.pwOpen, len(a.sessions))
	}
	close(gate)
	p.until(t, "the prompt", func() bool { return a.pwOpen && !a.editor.readingSecret })
}

// A key's passphrase is saved, and read as the session connects.
func TestStoredPassphrase(t *testing.T) {
	srv := startFakeSSH(t)
	a, p := testApp(t)
	t.Cleanup(a.CloseAll)
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	block, err := ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte("phrase"))
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(t.TempDir(), "id_ed25519")
	os.WriteFile(keyPath, pem.EncodeToMemory(block), 0o600)
	h := &config.Host{ID: "h1", Addr: srv.host, Port: srv.port, User: "u", Auth: config.AuthKey, KeyPath: keyPath}
	a.hosts.Add(h)
	a.editConnection(h)
	tt := editorWindow(t, a, p)
	p.until(t, "the store read", func() bool { return !a.editor.readingSecret })
	tt.Frame()
	if !tt.HasText(locales.RememberPassphrase()) {
		t.Errorf("no remember box; texts %q", tt.Texts())
	}
	typeInto(t, tt, locales.FieldPassphrase(), "phrase")
	if err := tt.Click(locales.Ok()); err != nil {
		t.Fatal(err)
	}
	key := hostSecretKey("h1", secretPassphrase)
	p.until(t, "the passphrase saved", func() bool { return p.secrets()[key] == "phrase" })

	// The next run reads it as it connects: the key opens, and the
	// server, which takes passwords alone, refuses it.
	p2 := newFakePlatform(t)
	p2.fakeSecrets = p.fakeSecrets
	b := New(p2)
	t.Cleanup(b.CloseAll)
	b.hosts.Add(&config.Host{ID: "h1", Addr: srv.host, Port: srv.port, User: "u", Auth: config.AuthKey, KeyPath: keyPath})
	b.openSession(b.hosts.Host("h1"))
	s := b.current()
	if s == nil || s.status != statusConnecting {
		t.Fatal("no session connecting")
	}
	p2.until(t, "the login", settled(s))
	if sshx.Classify(s.err) != "auth" || b.passwords["h1"] != "phrase" || b.pwOpen {
		t.Errorf("err %v, passphrase %q", s.err, b.passwords["h1"])
	}
}

// TestSSHRememberedPassword runs against the server FINALSHELLY_TEST_SSH
// names, with the fake store: a wrong saved password asks for the right
// one, which replaces it, and the next run connects without asking.
func TestSSHRememberedPassword(t *testing.T) {
	spec := os.Getenv("FINALSHELLY_TEST_SSH")
	if spec == "" {
		t.Skip("FINALSHELLY_TEST_SSH not set")
	}
	cred, addr, _ := strings.Cut(spec, "@")
	user, pass, _ := strings.Cut(cred, ":")
	hostname, portStr, _ := strings.Cut(addr, ":")
	port, _ := strconv.Atoi(portStr)

	a, p := testApp(t)
	a.saveHosts = config.SaveHosts
	t.Cleanup(a.CloseAll)
	h := &config.Host{ID: "test", Addr: hostname, Port: port, User: user, Auth: config.AuthPassword}
	a.hosts.Add(h)
	a.saveHosts(a.hosts)
	key := hostSecretKey(h.ID, secretPassword)
	p.put(key, "wrong-"+pass)
	a.openSession(h)
	p.until(t, "the prompt", func() bool { settled(a.current())(); return a.pwOpen })
	s := a.current()
	if s.status != statusFailed || sshx.Classify(s.err) != "auth" || !a.pwRemember {
		t.Fatalf("status %d err %v remember %v", s.status, s.err, a.pwRemember)
	}
	// A typo is not stored.
	a.pwText = "typo-" + pass
	a.passwordEntered()
	p.until(t, "the prompt again", func() bool { return a.pwOpen && s.status == statusFailed })
	p.until(t, "the store's calls", a.secrets.queue.idle)
	if got := p.secrets()[key]; got != "wrong-"+pass {
		t.Fatalf("stored %q after a typo", got)
	}
	a.pwText = pass
	a.passwordEntered()
	p.until(t, "the connection", settled(s))
	if s.status != statusConnected {
		t.Fatal(describeError(s.err))
	}
	p.until(t, "the password replaced", func() bool { return p.secrets()[key] == pass })

	p2 := newFakePlatform(t)
	p2.fakeSecrets = p.fakeSecrets
	b := New(p2)
	t.Cleanup(b.CloseAll)
	b.openSession(b.hosts.Host(h.ID))
	p2.until(t, "the connection", func() bool { return b.pwOpen || settled(b.current())() })
	if s := b.current(); b.pwOpen || s.status != statusConnected {
		t.Fatalf("prompt %v, session %+v", b.pwOpen, s)
	}
}
