package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"finallyshell/internal/config"
	"finallyshell/internal/widgets"
	"finallyshell/locales"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// fakeFilePicker stands in for the dialog that picks a file, which the
// app opens on a goroutine: it returns what pick set, and keeps what it
// was asked.
type fakeFilePicker struct {
	mu     sync.Mutex
	picked string
	asked  []filePick
}

type filePick struct {
	parent     WindowKind
	title, dir string
}

func (p *fakeFilePicker) OpenFile(parent WindowKind, title, dir string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.asked = append(p.asked, filePick{parent, title, dir})
	return p.picked, nil
}

// pick is what the next dialogs return: "" for canceled.
func (p *fakeFilePicker) pick(path string) {
	p.mu.Lock()
	p.picked = path
	p.mu.Unlock()
}

// pickers are the dialogs asked for so far.
func (p *fakeFilePicker) pickers() []filePick {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.asked)
}

// editorWindow shows the open editor's window, failing the test when
// there is none.
func editorWindow(t *testing.T, a *App, p *fakePlatform) *ui.Tester {
	t.Helper()
	if !a.windowOpen(WindowEditor) || a.editor == nil {
		t.Fatal("the connection editor is not open")
	}
	tt := p.show(t, WindowEditor)
	tt.Frame()
	return tt
}

// fieldValue is the text of the text field labeled label.
func fieldValue(t *testing.T, tt *ui.Tester, label string) string {
	t.Helper()
	for _, role := range []ui.Role{ui.RoleTextField, ui.RoleComboBox} {
		if f, ok := tt.AccessByRole(role, label); ok {
			return f.Value
		}
	}
	t.Fatalf("no field %q", label)
	return ""
}

// typeInto replaces the text of the text field labeled label.
func typeInto(t *testing.T, tt *ui.Tester, label, text string) {
	t.Helper()
	f, ok := tt.AccessByRole(ui.RoleTextField, label)
	if !ok {
		f, ok = tt.AccessByRole(ui.RoleComboBox, label)
	}
	if !ok {
		t.Fatalf("no field %q", label)
	}
	b := f.Bounds
	tt.ClickAt(b.X+b.W/2, b.Y+b.H/2)
	if !tt.Focused(label) {
		t.Fatalf("%s not focused", label)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Key(0, ui.KeyBackspace)
	if text != "" {
		tt.Type(text)
	}
}

// savedHosts keeps what the app saves, as JSON.
func savedHosts(a *App) *[]byte {
	var saved []byte
	a.saveHosts = func(s *config.Store) error {
		var err error
		saved, err = json.Marshal(s)
		return err
	}
	return &saved
}

func TestEditorNewFromManager(t *testing.T) {
	a, p := testApp(t)
	saved := savedHosts(a)
	mainWin := ui.NewTester(a.View, 1280, 820)
	p.tt = mainWin
	// With no hosts, the manager opens on a new one.
	mainWin.Key(ui.Ctrl|ui.Shift, ui.KeyO)
	if !a.windowOpen(WindowManager) {
		t.Fatal("manager not open")
	}
	// Modal to the manager, which it was opened from.
	spec := p.windows[WindowEditor].spec
	if spec.Title() != locales.NewConnection() || spec.Parent != WindowManager || !spec.Modal {
		t.Errorf("spec %+v", spec)
	}
	tt := editorWindow(t, a, p)
	if !tt.Focused(locales.FieldHost()) {
		t.Error("the host does not have the focus")
	}

	// OK with nothing filled in says what is missing, next to the field.
	if err := tt.Click(locales.Ok()); err != nil {
		t.Fatal(err)
	}
	if len(a.hosts.Hosts) != 0 || !a.windowOpen(WindowEditor) {
		t.Fatalf("an empty host saved: %v", a.hosts.Hosts)
	}
	host, _ := tt.AccessByRole(ui.RoleTextField, locales.FieldHost())
	if !tt.HasText(locales.ErrHostRequired()) || !host.Invalid {
		t.Errorf("no error on the host: %+v; texts %q", host, tt.Texts())
	}
	typeInto(t, tt, locales.FieldUser(), "")
	typeInto(t, tt, locales.FieldPort(), "70000")
	for _, want := range []string{locales.ErrHostRequired(), locales.ErrPortInvalid(), locales.ErrUserRequired()} {
		if !tt.HasText(want) {
			t.Errorf("missing %q; texts %q", want, tt.Texts())
		}
	}
	saveShot(t, tt, "editor-errors-en.png")
	p.SetLocale("zh-Hans")
	tt.Frame()
	if !tt.HasText("请填写主机") || !tt.HasText("端口应为 1 到 65535") || !tt.HasText("确定") {
		t.Errorf("errors not in Chinese; texts %q", tt.Texts())
	}
	saveShot(t, tt, "editor-errors-zh.png")
	p.SetLocale("")
	tt.Frame()

	// The errors follow the fields as they are fixed.
	typeInto(t, tt, locales.FieldHost(), "example.com")
	typeInto(t, tt, locales.FieldPort(), "2222")
	if tt.HasText(locales.ErrHostRequired()) || tt.HasText(locales.ErrPortInvalid()) || !tt.HasText(locales.ErrUserRequired()) {
		t.Errorf("errors after fixing; texts %q", tt.Texts())
	}
	typeInto(t, tt, locales.FieldUser(), "deploy")
	typeInto(t, tt, locales.FieldPassword(), "pw")
	redraws := p.redraws
	if err := tt.Click(locales.Ok()); err != nil {
		t.Fatal(err)
	}
	if a.windowOpen(WindowEditor) {
		t.Fatal("the editor stayed open")
	}
	hosts := a.hosts.Live()
	if len(hosts) != 1 {
		t.Fatalf("hosts %v", a.hosts.Hosts)
	}
	h := hosts[0]
	if h.ID == "" || h.Addr != "example.com" || h.Port != 2222 || h.User != "deploy" || h.Auth != config.AuthPassword ||
		h.Name != "" || !h.MonitorEnabled() || h.TermType != "" || h.Backspace != config.BackspaceDefault || h.Delete != config.DeleteDefault || a.passwords[h.ID] != "pw" {
		t.Errorf("host %+v", h)
	}
	if !strings.Contains(string(*saved), `"addr":"example.com"`) {
		t.Errorf("saved %s", *saved)
	}
	// The manager behind shows it, chosen.
	if p.redraws == redraws || !a.windowOpen(WindowManager) {
		t.Errorf("redraws %d, manager %v", p.redraws-redraws, a.windowOpen(WindowManager))
	}
	manager := p.show(t, WindowManager)
	manager.Frame()
	if it, _ := a.manager.chosen(); it.host != h.ID || !manager.HasText("example.com") || !manager.HasText("2222") || !manager.HasText("deploy") {
		t.Errorf("manager does not list it (chosen %v); texts %q", it, manager.Texts())
	}

	// New from the manager opens an empty editor again.
	if err := manager.Click(locales.NewConnection()); err != nil {
		t.Fatal(err)
	}
	if tt := editorWindow(t, a, p); a.editor.id != "" || a.editor.port != "22" || fieldValue(t, tt, locales.FieldHost()) != "" {
		t.Errorf("editor %+v", a.editor)
	}
}

func TestEditorEditKeepsFields(t *testing.T) {
	a, p := testApp(t)
	saved := savedHosts(a)
	var h config.Host
	err := json.Unmarshal([]byte(`{"id": "h1", "name": "web", "addr": "10.0.0.1", "port": 2200, "user": "root", "auth": "key",
		"keyPath": "~/.ssh/id_rsa", "folder": "f1", "notes": "main box\nrack 4", "noMonitor": true,
		"lastConnectedAt": "2026-11-07T10:00:00Z", "termType": "vt220", "backspace": "ctrl-h",
		"proxy": {"kind": "socks5", "addr": "10.0.0.9", "port": 1081, "user": "alice", "timeout": 5}}`), &h)
	if err != nil {
		t.Fatal(err)
	}
	a.hosts.Add(&h)
	a.passwords["h1"] = "phrase"
	a.proxySecrets()["proxy/h1"] = "pp"
	mainWin := ui.NewTester(a.View, 1280, 820)
	p.tt = mainWin
	a.openManager()
	if a.windowOpen(WindowEditor) {
		t.Fatal("the editor opened with hosts to list")
	}
	manager := p.show(t, WindowManager)
	manager.Frame()
	manager.RightClickAt(rowCenter(t, manager, "web"))
	if err := manager.ChooseMenuItem(locales.Edit()); err != nil {
		t.Fatal(err)
	}
	if spec := p.windows[WindowEditor].spec; spec.Title() != locales.EditConnection() {
		t.Errorf("title %q", spec.Title())
	}
	tt := editorWindow(t, a, p)
	ed := a.editor
	want := connEditor{id: "h1", name: "web", addr: "10.0.0.1", port: "2200", notes: "main box\nrack 4",
		auth: config.AuthKey, user: "root", keyPath: "~/.ssh/id_rsa", passphrase: "phrase",
		remember: true, monitor: false, termType: "vt220", backspace: config.BackspaceCtrlH,
		proxy:   proxyFields{kind: config.ProxySOCKS5, addr: "10.0.0.9", port: "1081", user: "alice", password: "pp", saved: h.Proxy, hosts: a.hosts},
		tunnels: newTunnelEditor(&h)}
	fields := *ed
	fields.nav = widgets.NavState{}
	if !reflect.DeepEqual(fields, want) || ed.nav.Page != editorSSH {
		t.Errorf("editor on %q\n%+v\nwant\n%+v", ed.nav.Page, fields, want)
	}
	for label, want := range map[string]string{
		locales.FieldName(): "web", locales.FieldHost(): "10.0.0.1", locales.FieldPort(): "2200",
		locales.FieldNotes(): "main box\nrack 4", locales.FieldUser(): "root", locales.FieldKeyFile(): "~/.ssh/id_rsa",
	} {
		if got := fieldValue(t, tt, label); got != want {
			t.Errorf("%s shows %q, want %q", label, got, want)
		}
	}
	if !tt.HasText(locales.Browse()) {
		t.Errorf("no Browse; texts %q", tt.Texts())
	}
	if box, ok := tt.AccessByRole(ui.RoleCheckBox, locales.MonitorChannel()); !ok || box.Checked {
		t.Errorf("monitor box %+v", box)
	}
	saveShot(t, tt, "editor-ssh-key-en.png")
	if err := tt.Click(locales.EditorPageTerminal()); err != nil {
		t.Fatal(err)
	}
	if fieldValue(t, tt, locales.FieldTermType()) != "vt220" || !tt.HasText(locales.KeySeqCtrlH()) || !tt.HasText(locales.KeySeqDefault("UTF-8")) {
		t.Errorf("terminal page; texts %q", tt.Texts())
	}

	if err := tt.Click(locales.EditorPageSSH()); err != nil {
		t.Fatal(err)
	}
	typeInto(t, tt, locales.FieldName(), "web-1")
	if err := tt.Click(locales.Ok()); err != nil {
		t.Fatal(err)
	}
	if a.windowOpen(WindowEditor) {
		t.Fatal("editor still open")
	}
	var file struct {
		Hosts []map[string]any `json:"hosts"`
	}
	if err := json.Unmarshal(*saved, &file); err != nil || len(file.Hosts) != 1 {
		t.Fatalf("saved %s: %v", *saved, err)
	}
	got := file.Hosts[0]
	for k, want := range map[string]any{
		"id": "h1", "name": "web-1", "addr": "10.0.0.1", "port": 2200.0, "user": "root", "auth": "key",
		"keyPath": "~/.ssh/id_rsa", "folder": "f1", "notes": "main box\nrack 4", "noMonitor": true,
		"lastConnectedAt": "2026-11-07T10:00:00Z", "termType": "vt220", "backspace": "ctrl-h",
		"proxy": map[string]any{"kind": "socks5", "addr": "10.0.0.9", "port": 1081.0, "user": "alice", "timeout": 5.0},
	} {
		if !reflect.DeepEqual(got[k], want) {
			t.Errorf("%s = %v, want %v", k, got[k], want)
		}
	}
	if len(got) != 14 {
		t.Errorf("saved keys %v", got)
	}
	if a.passwords["h1"] != "phrase" || a.proxySecrets()["proxy/h1"] != "pp" || a.hosts.Host("h1") != &h {
		t.Errorf("passphrase %q", a.passwords["h1"])
	}
}

func TestEditorChangesEveryField(t *testing.T) {
	a, p := testApp(t)
	h := &config.Host{ID: "h1", Addr: "10.0.0.1", User: "root", Auth: config.AuthKey, KeyPath: "~/.ssh/id_rsa", Folder: "f1"}
	a.hosts.Add(h)
	a.editConnection(h)
	tt := editorWindow(t, a, p)
	typeInto(t, tt, locales.FieldName(), "db")
	typeInto(t, tt, locales.FieldHost(), "db.example.com")
	typeInto(t, tt, locales.FieldPort(), "2201")
	typeInto(t, tt, locales.FieldNotes(), "line 1")
	tt.Key(0, ui.KeyEnter) // a new line, not OK
	tt.Type("line 2")
	if !a.windowOpen(WindowEditor) {
		t.Fatal("Enter in the notes closed the editor")
	}
	// From a key to a password.
	if err := tt.Click(locales.AuthKey()); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(locales.AuthPassword()); err != nil {
		t.Fatal(err)
	}
	if a.editor.auth != config.AuthPassword || tt.HasText(locales.Browse()) {
		t.Fatalf("auth %q", a.editor.auth)
	}
	typeInto(t, tt, locales.FieldUser(), "admin")
	typeInto(t, tt, locales.FieldPassword(), "s3cret")
	if err := tt.Click(locales.MonitorChannel()); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(locales.EditorPageTerminal()); err != nil {
		t.Fatal(err)
	}
	typeInto(t, tt, locales.FieldTermType(), "vt100")
	tt.Key(0, ui.KeyEscape) // closes the suggestions, not the window
	if !a.windowOpen(WindowEditor) {
		t.Fatal("Escape in the suggestions closed the editor")
	}
	// Both keys start at "Default", naming what the settings send.
	for _, k := range [][2]string{
		{locales.KeySeqDefault("UTF-8"), "GBK"},
		{locales.KeySeqDefault(locales.KeySeqDEL()), locales.KeySeqCtrlH()},
		{locales.KeySeqDefault(locales.KeySeqVT220()), locales.KeySeqDEL()},
	} {
		if err := tt.Click(k[0]); err != nil {
			t.Fatal(err)
		}
		if err := tt.Click(k[1]); err != nil {
			t.Fatal(err)
		}
	}
	if a.editor.backspace != config.BackspaceCtrlH || a.editor.delete != config.DeleteDEL || a.editor.encoding != config.EncodingGBK {
		t.Fatalf("backspace %q, delete %q, encoding %q", a.editor.backspace, a.editor.delete, a.editor.encoding)
	}
	if err := tt.Click(locales.Ok()); err != nil {
		t.Fatal(err)
	}
	want := &config.Host{ID: "h1", Name: "db", Addr: "db.example.com", Port: 2201, User: "admin", Auth: config.AuthPassword,
		Folder: "f1", Notes: "line 1\nline 2", NoMonitor: true, TermType: "vt100", Backspace: config.BackspaceCtrlH, Delete: config.DeleteDEL, Encoding: config.EncodingGBK}
	if a.windowOpen(WindowEditor) || !reflect.DeepEqual(h, want) || a.passwords["h1"] != "s3cret" {
		t.Errorf("host\n%+v\nwant\n%+v", h, want)
	}

	// The default terminal type is saved as none.
	a.editConnection(h)
	tt = editorWindow(t, a, p)
	a.editor.termType = config.DefaultTermType
	a.editor.password = "" // asked for at the next connection
	tt.Key(0, ui.KeyEnter)
	if a.windowOpen(WindowEditor) || h.TermType != "" {
		t.Errorf("term type %q", h.TermType)
	}
	if _, known := a.passwords["h1"]; known {
		t.Error("an empty password is known")
	}
}

func TestEditorCancelAndEscape(t *testing.T) {
	a, p := testApp(t)
	h := &config.Host{ID: "h1", Name: "web", Addr: "10.0.0.1", User: "root", Auth: config.AuthPassword}
	a.hosts.Add(h)
	saves := 0
	a.saveHosts = func(*config.Store) error { saves++; return nil }
	before := *h

	a.editConnection(h)
	tt := editorWindow(t, a, p)
	typeInto(t, tt, locales.FieldName(), "changed")
	if err := tt.Click(locales.Cancel()); err != nil {
		t.Fatal(err)
	}
	if a.windowOpen(WindowEditor) || !reflect.DeepEqual(*h, before) || saves != 0 {
		t.Fatalf("Cancel: open %v, host %+v, %d saves", a.windowOpen(WindowEditor), h, saves)
	}

	a.editConnection(h)
	tt = editorWindow(t, a, p)
	if a.editor.name != "web" {
		t.Errorf("the editor kept the canceled name %q", a.editor.name)
	}
	typeInto(t, tt, locales.FieldName(), "changed")
	tt.Key(0, ui.KeyEscape)
	if a.windowOpen(WindowEditor) || !reflect.DeepEqual(*h, before) || saves != 0 {
		t.Fatalf("Escape: open %v, host %+v, %d saves", a.windowOpen(WindowEditor), h, saves)
	}

	// A new host canceled is not added.
	a.newConnection()
	tt = editorWindow(t, a, p)
	tt.Type("new.example.com")
	tt.Key(0, ui.KeyEscape)
	if len(a.hosts.Hosts) != 1 || saves != 0 {
		t.Errorf("hosts %v", a.hosts.Hosts)
	}
}

func TestEditorEnterSaves(t *testing.T) {
	a, p := testApp(t)
	a.newConnection()
	tt := editorWindow(t, a, p)
	tt.Type("example.com") // the host has the focus
	tt.Key(0, ui.KeyEnter)
	if a.windowOpen(WindowEditor) || len(a.hosts.Hosts) != 1 || a.hosts.Hosts[0].Addr != "example.com" || a.hosts.Hosts[0].User != "root" {
		t.Fatalf("Enter: open %v, hosts %v", a.windowOpen(WindowEditor), a.hosts.Hosts)
	}

	// Enter with an error shows it, and saves nothing.
	a.newConnection()
	tt = editorWindow(t, a, p)
	tt.Key(0, ui.KeyEnter)
	if !a.windowOpen(WindowEditor) || len(a.hosts.Hosts) != 1 || !tt.HasText(locales.ErrHostRequired()) {
		t.Errorf("Enter on an empty host: open %v, hosts %v", a.windowOpen(WindowEditor), a.hosts.Hosts)
	}
}

func TestEditorErrorOnAnotherPage(t *testing.T) {
	a, p := testApp(t)
	a.newConnection()
	tt := editorWindow(t, a, p)
	tt.Type("example.com")
	if err := tt.Click(locales.EditorPageTerminal()); err != nil {
		t.Fatal(err)
	}
	typeInto(t, tt, locales.FieldTermType(), "bad term")
	tt.Key(0, ui.KeyEscape) // the suggestions
	if err := tt.Click(locales.EditorPageProxy()); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(locales.Ok()); err != nil {
		t.Fatal(err)
	}
	if a.editor.nav.Page != editorTerminal || !tt.HasText(locales.ErrTermTypeInvalid()) || len(a.hosts.Hosts) != 0 {
		t.Errorf("page %q; texts %q", a.editor.nav.Page, tt.Texts())
	}
	// An error on the page shown keeps it.
	typeInto(t, tt, locales.FieldTermType(), "xterm")
	tt.Key(0, ui.KeyEscape)
	if err := tt.Click(locales.EditorPageSSH()); err != nil {
		t.Fatal(err)
	}
	typeInto(t, tt, locales.FieldUser(), "")
	if err := tt.Click(locales.Ok()); err != nil {
		t.Fatal(err)
	}
	if a.editor.nav.Page != editorSSH || !tt.HasText(locales.ErrUserRequired()) {
		t.Errorf("page %q; texts %q", a.editor.nav.Page, tt.Texts())
	}
}

func TestEditorKeyFile(t *testing.T) {
	a, p := testApp(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
	a.newConnection()
	tt := editorWindow(t, a, p)
	tt.Type("example.com")
	if err := tt.Click(locales.AuthPassword()); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(locales.AuthKey()); err != nil {
		t.Fatal(err)
	}
	// Key auth needs the key. (Enter would open the select, which has
	// the focus.)
	if err := tt.Click(locales.Ok()); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText(locales.ErrKeyFileRequired()) || len(a.hosts.Hosts) != 0 {
		t.Fatalf("no key error; texts %q", tt.Texts())
	}
	saveShot(t, tt, "editor-key-error-en.png")

	// Browse fills it in, starting in ~/.ssh.
	key := filepath.Join(home, "keys", "id_ed25519")
	p.pick(key)
	if err := tt.Click(locales.Browse()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the key path", func() bool { return a.editor.keyPath == key })
	if asked := p.pickers(); len(asked) != 1 || asked[0] != (filePick{WindowEditor, locales.ChooseKeyFile(), filepath.Join(home, ".ssh")}) {
		t.Errorf("asked %+v", asked)
	}
	tt.Frame()
	if fieldValue(t, tt, locales.FieldKeyFile()) != key || tt.HasText(locales.ErrKeyFileRequired()) {
		t.Errorf("texts %q", tt.Texts())
	}

	// Again, from the key's folder; canceling keeps the path.
	p.pick("")
	if err := tt.Click(locales.Browse()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the second dialog", func() bool { return len(p.pickers()) == 2 })
	if dir := p.pickers()[1].dir; dir != filepath.Join(home, "keys") {
		t.Errorf("second dialog in %q", dir)
	}
	typeInto(t, tt, locales.FieldPassphrase(), "phrase")
	tt.Key(0, ui.KeyEnter)
	if len(a.hosts.Hosts) != 1 {
		t.Fatal("not saved")
	}
	if h := a.hosts.Hosts[0]; h.Auth != config.AuthKey || h.KeyPath != key || a.passwords[h.ID] != "phrase" {
		t.Errorf("host %+v", h)
	}
}

func TestEditorOneAtATime(t *testing.T) {
	a, p := testApp(t)
	x := &config.Host{ID: "x", Addr: "x.example.com", User: "root", Auth: config.AuthPassword}
	y := &config.Host{ID: "y", Addr: "y.example.com", User: "root", Auth: config.AuthPassword}
	a.hosts.Add(x)
	a.hosts.Add(y)
	a.editConnection(x)
	a.editor.name = "typed"
	a.editConnection(y)
	a.newConnection()
	if a.editor.id != "x" || a.editor.name != "typed" || len(p.focused) != 2 {
		t.Errorf("editor %q %q, focused %v", a.editor.id, a.editor.name, p.focused)
	}
}

// A host deleted in the manager while its editor is open is not saved
// back: OK says so and keeps the editor open.
func TestEditorHostDeleted(t *testing.T) {
	for _, how := range []string{"removed", "soft-deleted"} {
		t.Run(how, func(t *testing.T) {
			a, p := testApp(t)
			saves := 0
			a.saveHosts = func(*config.Store) error { saves++; return nil }
			h := &config.Host{ID: "x", Name: "web", Addr: "x.example.com", User: "root", Auth: config.AuthPassword}
			a.hosts.Add(h)
			a.editConnection(h)
			tt := editorWindow(t, a, p)
			typeInto(t, tt, locales.FieldName(), "typed")
			if how == "removed" {
				a.hosts.Remove("x")
			} else {
				a.hosts.SoftDelete("x", time.Now())
			}
			if err := tt.Click(locales.Ok()); err != nil {
				t.Fatal(err)
			}
			if !a.windowOpen(WindowEditor) || saves != 0 || h.Name != "web" || !tt.HasText(locales.ErrHostDeleted()) {
				t.Fatalf("open %v, %d saves, name %q; texts %q", a.windowOpen(WindowEditor), saves, h.Name, tt.Texts())
			}
			if how == "removed" && len(a.hosts.Hosts) != 0 {
				t.Errorf("added back: %v", a.hosts.Hosts)
			}
			if how == "soft-deleted" {
				p.SetLocale("zh-Hans")
				tt.Frame()
				if !tt.HasText("此连接已被删除") {
					t.Errorf("not in Chinese; texts %q", tt.Texts())
				}
				saveShot(t, tt, "editor-deleted-zh.png")
			}
			// Cancel still closes it.
			if err := tt.Click(locales.Cancel()); err != nil {
				t.Fatal(err)
			}
			if a.windowOpen(WindowEditor) {
				t.Error("still open")
			}
		})
	}
}

func TestEditorSaveFails(t *testing.T) {
	a, p := testApp(t)
	a.saveHosts = func(*config.Store) error { return errors.New("disk full") }
	a.newConnection()
	tt := editorWindow(t, a, p)
	tt.Type("example.com")
	tt.Key(0, ui.KeyEnter)
	if !a.windowOpen(WindowEditor) || !tt.HasText(locales.ErrSaveFailed("disk full")) {
		t.Errorf("open %v; texts %q", a.windowOpen(WindowEditor), tt.Texts())
	}
	// Trying again saves the same host, not another.
	a.saveHosts = func(*config.Store) error { return nil }
	tt.Key(0, ui.KeyEnter)
	if a.windowOpen(WindowEditor) || len(a.hosts.Hosts) != 1 {
		t.Errorf("open %v, hosts %v", a.windowOpen(WindowEditor), a.hosts.Hosts)
	}
}

// TestEditorPages shows every page in both languages, for screenshots.
func TestEditorPages(t *testing.T) {
	for _, lang := range []string{"en", "zh-Hans"} {
		t.Run(lang, func(t *testing.T) {
			a, p := testApp(t)
			p.SetLocale(lang)
			h := &config.Host{ID: "h1", Name: "web-1", Addr: "203.0.113.10", Port: 22, User: "root", Auth: config.AuthPassword,
				Notes: "Production web server\nRack 4"}
			a.hosts.Add(h)
			a.editConnection(h)
			tt := editorWindow(t, a, p)
			if got := p.windows[WindowEditor].spec.Title(); lang == "zh-Hans" && got != "编辑连接" {
				t.Errorf("title %q", got)
			}
			wants := map[string][]string{
				editorSSH: {locales.SectionGeneral(), locales.SectionAuth(), locales.SectionAdvanced(), locales.FieldNotes(), locales.MonitorChannel()},
				editorTerminal: {locales.FieldTermType(), locales.FieldEncoding(), locales.FieldBackspace(), locales.FieldDelete(), locales.KeySeqDefault("UTF-8"), locales.EncodingHint(),
					locales.KeySeqDefault(locales.KeySeqDEL()), locales.KeySeqDefault(locales.KeySeqVT220())},
				editorProxy:   {locales.FieldProxyType(), locales.ProxyNone(), locales.ProxyNoneHint()},
				editorTunnels: {locales.TunnelsIntro(), locales.TunnelsEmpty(), locales.TunnelAdd()},
			}
			labels := map[string]string{
				editorSSH: locales.EditorPageSSH(), editorTerminal: locales.EditorPageTerminal(),
				editorProxy: locales.EditorPageProxy(), editorTunnels: locales.EditorPageTunnels(),
			}
			for _, page := range []string{editorSSH, editorTerminal, editorProxy, editorTunnels} {
				if err := tt.Click(labels[page]); err != nil {
					t.Fatal(err)
				}
				if a.editor.nav.Page != page {
					t.Fatalf("page %q, want %q", a.editor.nav.Page, page)
				}
				for _, want := range wants[page] {
					if !tt.HasText(want) {
						t.Errorf("%s: missing %q; texts %q", page, want, tt.Texts())
					}
				}
				saveShot(t, tt, "editor-"+page+"-"+lang+".png")
			}
			if lang == "zh-Hans" {
				for _, want := range []string{"确定", "取消", "SSH 连接", "终端", "代理", "隧道"} {
					if !tt.HasText(want) {
						t.Errorf("missing %q", want)
					}
				}
			}
		})
	}
}

// Backspace and Delete send what the host's choices say; other keys,
// and both keys by default, are the terminal's.
func TestKeySequences(t *testing.T) {
	if keySequences(config.BackspaceDEL, config.DeleteVT220, nil) != nil {
		t.Fatal("a key handler for the defaults")
	}
	for _, tc := range []struct {
		name string
		bs   config.BackspaceKey
		del  config.DeleteKey
		keys []keyCase
	}{
		{"backspace ^H", config.BackspaceCtrlH, config.DeleteVT220, []keyCase{
			{"Backspace", 0, ui.KeyBackspace, "\x08"},
			{"Alt+Backspace", ui.Alt, ui.KeyBackspace, "\x1b\x08"},
			{"Ctrl+H", ui.Ctrl, ui.KeyH, "\x08"},
			{"Delete", 0, ui.KeyDelete, "\x1b[3~"},
			{"Enter", 0, ui.KeyEnter, "\r"},
		}},
		{"delete ^?", config.BackspaceDEL, config.DeleteDEL, []keyCase{
			{"Delete", 0, ui.KeyDelete, "\x7f"},
			{"Ctrl+Delete", ui.Ctrl, ui.KeyDelete, "\x1b[3;5~"},
			{"Backspace", 0, ui.KeyBackspace, "\x7f"},
		}},
		{"delete ^H", config.BackspaceDEL, config.DeleteCtrlH, []keyCase{
			{"Delete", 0, ui.KeyDelete, "\x08"},
			{"Shift+Delete", ui.Shift, ui.KeyDelete, "\x1b[3;2~"},
			{"Backspace", 0, ui.KeyBackspace, "\x7f"},
		}},
		{"both ^H", config.BackspaceCtrlH, config.DeleteCtrlH, []keyCase{
			{"Backspace", 0, ui.KeyBackspace, "\x08"},
			{"Delete", 0, ui.KeyDelete, "\x08"},
			{"Left", 0, ui.KeyLeft, "\x1b[D"},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := &keyConn{done: make(chan struct{})}
			defer close(conn.done)
			var term *terminal.Terminal
			term, err := terminal.New(terminal.Options{Conn: conn, OnKey: keySequences(tc.bs, tc.del, func() *terminal.Terminal { return term })})
			if err != nil {
				t.Skip("no terminal:", err)
			}
			defer term.Close()
			tt := ui.NewTester(func(c *ui.Context) { terminal.View(c, term).Fill().AutoFocus() }, 400, 200)
			tt.Frame()
			for _, k := range tc.keys {
				tt.Key(k.mods, k.key)
				if got := conn.sent(t, term); got != k.want {
					t.Errorf("%s sent %q, want %q", k.name, got, k.want)
				}
			}
		})
	}
}

// The terminal type and the erase character reach the server's
// pseudo-terminal.
func TestTerminalOptionsReachServer(t *testing.T) {
	srv := startFakeSSH(t)
	a, p := testApp(t)
	t.Cleanup(a.CloseAll)
	plain := &config.Host{ID: "plain", Addr: srv.host, Port: srv.port, User: "u", Auth: config.AuthPassword, NoMonitor: true}
	old := &config.Host{ID: "old", Addr: srv.host, Port: srv.port, User: "u", Auth: config.AuthPassword, NoMonitor: true,
		TermType: "vt220", Backspace: config.BackspaceCtrlH}
	a.hosts.Add(plain)
	a.hosts.Add(old)
	connectFake(t, a, p, plain)
	if pty := srv.lastPTY(); pty.term != "xterm-256color" || pty.erase != 0x7f {
		t.Errorf("default pty %+v", pty)
	}
	connectFake(t, a, p, old)
	if pty := srv.lastPTY(); pty.term != "vt220" || pty.erase != 0x08 {
		t.Errorf("pty %+v", pty)
	}

	// With ^H in the settings, a host on "Default" takes it, and one that
	// chose ^? keeps it.
	a.settings.TermBackspace = config.BackspaceCtrlH
	del := &config.Host{ID: "del", Addr: srv.host, Port: srv.port, User: "u", Auth: config.AuthPassword, NoMonitor: true,
		Backspace: config.BackspaceDEL}
	a.hosts.Add(del)
	connectFake(t, a, p, plain)
	if pty := srv.lastPTY(); pty.erase != 0x08 {
		t.Errorf("default host with ^H in the settings: pty %+v", pty)
	}
	connectFake(t, a, p, del)
	if pty := srv.lastPTY(); pty.erase != 0x7f {
		t.Errorf("host that chose ^?: pty %+v", pty)
	}
}

// A host's encoding converts what the server writes before the terminal
// shows it, and what is typed and pasted before the server reads it.
func TestEncodingReachesServer(t *testing.T) {
	srv := startFakeSSH(t)
	// "你好，世界" in GBK.
	srv.greet([]byte{0xc4, 0xe3, 0xba, 0xc3, 0xa3, 0xac, 0xca, 0xc0, 0xbd, 0xe7, '\r', '\n'})
	a, p := testApp(t)
	t.Cleanup(a.CloseAll)
	a.settings.TermEncoding = config.EncodingGBK
	h := &config.Host{ID: "gbk", Addr: srv.host, Port: srv.port, User: "u", Auth: config.AuthPassword, NoMonitor: true}
	a.hosts.Add(h)
	s := connectFake(t, a, p, h)
	p.until(t, "the greeting", func() bool { return strings.Contains(s.term.Text(), "你好，世界") })

	s.term.Send([]byte("中文"))
	want := []byte{0xd6, 0xd0, 0xce, 0xc4}
	p.until(t, "the input", func() bool { return bytes.Contains(srv.typed(), want) })

	// A host that chose UTF-8 is not converted.
	srv.greet([]byte("é\r\n"))
	h.Encoding = config.EncodingUTF8
	s = connectFake(t, a, p, h)
	p.until(t, "the UTF-8 greeting", func() bool { return strings.Contains(s.term.Text(), "é") })
}
