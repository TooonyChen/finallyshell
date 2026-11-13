package app

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"finallyshell/internal/config"
	"finallyshell/internal/sshtest"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"golang.org/x/text/encoding/simplifiedchinese"
)

// The text of two files the tests open, and show in the screenshots.
const (
	deployScript = "#!/bin/bash\r\n# Deploy the web app.\r\nset -euo pipefail\r\n\r\nAPP_DIR=/srv/web\r\nfor host in web-1 web-2; do\r\n  echo \"deploying to $host\"\r\n  rsync -az --delete \"$APP_DIR/\" \"$host:$APP_DIR/\"\r\ndone\r\n"
	appYAML      = "# The web app's settings.\nserver:\n  port: 8080\n  workers: 4\nlog:\n  level: info\n  file: /var/log/web/app.log\n"
)

// editorFixture is a session to an in-process server whose SFTP serves
// dir, with the files of the tests in it, and the main window drawn.
type editorFixture struct {
	a   *App
	p   *fakePlatform
	s   *Session
	tt  *ui.Tester
	srv *sshtest.Server
	dir string
}

func newEditorFixture(t *testing.T) *editorFixture {
	t.Helper()
	dir := t.TempDir()
	write := func(name string, data []byte, mode os.FileMode) {
		if err := os.WriteFile(filepath.Join(dir, name), data, mode); err != nil {
			t.Fatal(err)
		}
		os.Chmod(filepath.Join(dir, name), mode)
	}
	write("deploy.sh", []byte(deployScript), 0o750)
	write("app.yaml", []byte(appYAML), 0o640)
	write("a.out", append([]byte("\x7fELF\x02\x01\x01\x00\x00\x00"), bytes.Repeat([]byte{1, 2, 3}, 100)...), 0o755)
	gb, _ := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte("# 配置说明\r\n名称=测试服务器\r\n"))
	write("legacy.ini", gb, 0o644)
	write("big.log", bytes.Repeat([]byte("2026-11-07 12:00:00 INFO request served in 3 ms\n"), 2000), 0o644)

	srv := sshtest.StartSFTP(t, dir)
	a, p := testApp(t)
	h := &config.Host{ID: "files", Name: "web-1", Addr: srv.Host, Port: srv.Port, User: "tester", Auth: config.AuthPassword, NoMonitor: true}
	a.hosts.Add(h)
	a.passwords[h.ID] = sshtest.Password
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	a.openSession(h)
	s := a.current()
	p.until(t, "the files", func() bool {
		if s.hostKeyQ != nil {
			s.hostKeyQ.Answer <- true
			s.hostKeyQ = nil
		}
		return s.status == statusFailed || s.files.cwd == dir && !s.files.loading
	})
	if s.status != statusConnected {
		t.Fatal(describeError(s.err))
	}
	t.Cleanup(a.CloseAll)
	return &editorFixture{a: a, p: p, s: s, tt: tt, srv: srv, dir: dir}
}

// tab waits for the tab of name to be read, and returns it.
func (f *editorFixture) tab(t *testing.T, name string) *editTab {
	t.Helper()
	var tab *editTab
	f.p.until(t, name+" read", func() bool {
		i := slices.IndexFunc(f.a.fe.tabs, func(t *editTab) bool { return t.name() == name })
		if i < 0 {
			return false
		}
		tab = f.a.fe.tabs[i]
		return tab.state != tabLoading
	})
	return tab
}

// window draws the editor's window.
func (f *editorFixture) window(t *testing.T) *ui.Tester {
	t.Helper()
	w := f.p.show(t, WindowFileEditor)
	w.Frame()
	return w
}

// saved waits for tab's save to end.
func (f *editorFixture) saved(t *testing.T, tab *editTab) {
	t.Helper()
	f.p.until(t, tab.name()+" saved", func() bool {
		if w := f.p.windows[WindowFileEditor]; w != nil && w.tt != nil {
			w.tt.Frame()
		}
		return !tab.saving
	})
}

func (f *editorFixture) read(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(f.dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestFileEditorOpenEditSave(t *testing.T) {
	f := newEditorFixture(t)
	a, p, tt := f.a, f.p, f.tt

	// A double click on a file opens it; Enter on a folder still opens
	// the folder (as before).
	box, ok := tt.Find("deploy.sh")
	if !ok {
		t.Fatalf("no deploy.sh; texts %q", tt.Texts())
	}
	tt.ClickAt(box.X+4, box.Y+box.H/2)
	tt.ClickAt(box.X+4, box.Y+box.H/2)
	tt.Frame()
	if !a.windowOpen(WindowFileEditor) {
		t.Fatal("a double click did not open the editor")
	}
	sh := f.tab(t, "deploy.sh")
	if sh.state != tabReady || sh.sess != f.s {
		t.Fatalf("state %d err %v", sh.state, sh.err)
	}
	w := f.window(t)
	spec := p.windows[WindowFileEditor].spec
	if p.windows[WindowFileEditor].title != "web-1:"+filepath.Join(f.dir, "deploy.sh") || spec.Parent != "" || spec.Modal || spec.Width < 900 {
		t.Errorf("title %q, spec %+v", p.windows[WindowFileEditor].title, spec)
	}
	for _, want := range []string{"Shell", "CRLF", "UTF-8", locales.EditorLineCol(1, 1), "deploy.sh"} {
		if !w.HasText(want) {
			t.Errorf("missing %q; texts %q", want, w.Texts())
		}
	}
	if got := sh.code.Buffer.String(); got != strings.ReplaceAll(deployScript, "\r\n", "\n") {
		t.Errorf("text %q", got)
	}

	// Editing and saving preserves line endings and mode.
	w.Key(ui.Cmd, ui.KeyEnd)
	w.Type("echo done\n")
	w.Frame()
	if !sh.modified() || a.unsavedFiles() != 1 {
		t.Fatal("typing did not modify it")
	}
	w.Key(ui.Cmd, ui.KeyS)
	f.saved(t, sh)
	if sh.saveErr != nil {
		t.Fatal(sh.saveErr)
	}
	if got := f.read(t, "deploy.sh"); got != deployScript+"echo done\r\n" {
		t.Errorf("saved %q", got)
	}
	if fi, _ := os.Stat(filepath.Join(f.dir, "deploy.sh")); fi.Mode() != 0o750 {
		t.Errorf("mode %v", fi.Mode())
	}
	w.Frame()
	if sh.modified() || !w.HasText(locales.EditorSaved()) {
		t.Errorf("not shown saved; texts %q", w.Texts())
	}
	entries, _ := os.ReadDir(f.dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".finallyshell-") {
			t.Errorf("left %s", e.Name())
		}
	}

	// The context menu's Edit opens another tab; opening a file again
	// selects its tab.
	if err := tt.RightClick("app.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem(locales.Edit()); err != nil {
		t.Fatalf("%v; menu %q", err, tt.Menu())
	}
	yml := f.tab(t, "app.yaml")
	w.Frame()
	if len(a.fe.tabs) != 2 || a.fe.current() != yml || !w.HasText("YAML") || !strings.HasSuffix(p.windows[WindowFileEditor].title, "app.yaml") {
		t.Fatalf("tabs %d; texts %q", len(a.fe.tabs), w.Texts())
	}
	a.openRemoteFile(f.s, filepath.Join(f.dir, "deploy.sh"))
	if len(a.fe.tabs) != 2 || a.fe.current() != sh {
		t.Errorf("opened twice: %d tabs", len(a.fe.tabs))
	}
	w.Key(ui.Ctrl, ui.KeyTab)
	if a.fe.current() != yml {
		t.Error("Ctrl+Tab did not go to the next tab")
	}

	// A change shows as a dot on its tab, until it is saved.
	w.Frame()
	w.Key(ui.Cmd, ui.KeyEnd)
	w.Type("  rotate: daily\n")
	w.Frame()
	if !yml.modified() || yml.code.Buffer.LineEnding() != "\n" {
		t.Fatal("not modified")
	}
	// The script, saved, next to the YAML file with its change.
	w.Key(ui.Ctrl|ui.Shift, ui.KeyTab)
	if a.fe.current() != sh {
		t.Fatal("Ctrl+Shift+Tab did not go back")
	}
	for _, lang := range []string{"en", "zh-Hans"} {
		p.SetLocale(lang)
		w.Frame()
		saveShot(t, w, "editor-"+lang+".png")
	}
	p.SetLocale("")
	w.Key(ui.Ctrl, ui.KeyTab)
	w.Frame()
	if err := w.Click(locales.Save()); err != nil { // the toolbar's
		t.Fatal(err)
	}
	f.saved(t, yml)
	if got := f.read(t, "app.yaml"); got != appYAML+"  rotate: daily\n" {
		t.Errorf("saved %q", got)
	}
	if fi, _ := os.Stat(filepath.Join(f.dir, "app.yaml")); fi.Mode() != 0o640 {
		t.Errorf("mode %v", fi.Mode())
	}
}

func TestFileEditorConflict(t *testing.T) {
	f := newEditorFixture(t)
	a, p := f.a, f.p
	a.openRemoteFile(f.s, filepath.Join(f.dir, "app.yaml"))
	tab := f.tab(t, "app.yaml")
	w := f.window(t)

	// Someone else saves the file meanwhile: the save asks.
	w.Type("# mine\n")
	other := appYAML + "# theirs\n"
	os.WriteFile(filepath.Join(f.dir, "app.yaml"), []byte(other), 0o640)
	w.Key(ui.Cmd, ui.KeyS)
	f.saved(t, tab)
	w.Frame()
	if !tab.conflict || !w.HasText(locales.EditorConflict("app.yaml")) || f.read(t, "app.yaml") != other {
		t.Fatalf("no question; texts %q", w.Texts())
	}
	for _, lang := range []string{"en", "zh-Hans"} {
		p.SetLocale(lang)
		w.Frame()
		saveShot(t, w, "editor-conflict-"+lang+".png")
	}
	p.SetLocale("")
	w.Frame()
	if err := w.Click(locales.Overwrite()); err != nil {
		t.Fatal(err)
	}
	f.saved(t, tab)
	if got := f.read(t, "app.yaml"); got != "# mine\n"+appYAML || tab.modified() {
		t.Errorf("overwrote with %q", got)
	}

	// Again, reloading this time: the server's text, and Undo brings
	// the edit back.
	w.Type("# mine again\n")
	os.WriteFile(filepath.Join(f.dir, "app.yaml"), []byte(other), 0o640)
	w.Key(ui.Cmd, ui.KeyS)
	f.saved(t, tab)
	w.Frame()
	if err := w.Click(locales.EditorReload()); err != nil {
		t.Fatalf("%v; texts %q", err, w.Texts())
	}
	f.saved(t, tab)
	if got := tab.code.Buffer.String(); got != other || tab.modified() {
		t.Errorf("reloaded %q, modified %v", got, tab.modified())
	}
	tab.code.Run(editor.Undo)
	if !strings.Contains(tab.code.Buffer.String(), "# mine again") {
		t.Errorf("undo: %q", tab.code.Buffer.String())
	}
	// A save now replaces it without asking: it is the file as read.
	a.saveTab(tab, false)
	f.saved(t, tab)
	if tab.conflict || !strings.Contains(f.read(t, "app.yaml"), "# mine again") {
		t.Errorf("conflict %v; %q", tab.conflict, f.read(t, "app.yaml"))
	}

	// A file removed meanwhile says so, and Overwrite makes it again.
	os.Remove(filepath.Join(f.dir, "app.yaml"))
	w.Type("x")
	a.saveTab(tab, false)
	f.saved(t, tab)
	w.Frame()
	if !tab.conflictGone || !w.HasText(locales.EditorConflictGone("app.yaml")) {
		t.Fatalf("no question; texts %q", w.Texts())
	}
	w.Click(locales.Overwrite())
	f.saved(t, tab)
	if got := f.read(t, "app.yaml"); got != tab.code.Buffer.String() || !strings.Contains(got, "x") || tab.modified() {
		t.Errorf("made again: %q", f.read(t, "app.yaml"))
	}
}

func TestFileEditorBinaryAndLarge(t *testing.T) {
	f := newEditorFixture(t)
	a, p := f.a, f.p
	a.openRemoteFile(f.s, filepath.Join(f.dir, "a.out"))
	bin := f.tab(t, "a.out")
	w := f.window(t)
	if bin.state != tabBinary || !w.HasText(locales.EditorBinary("a.out")) {
		t.Fatalf("state %d; texts %q", bin.state, w.Texts())
	}
	for _, lang := range []string{"en", "zh-Hans"} {
		p.SetLocale(lang)
		w.Frame()
		saveShot(t, w, "editor-binary-"+lang+".png")
	}
	p.SetLocale("")
	w.Frame()
	if err := w.Click(locales.EditorOpenWithApp()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the copy opened", func() bool { return !bin.opening })
	if bin.openErr != nil || len(p.openedPaths) != 1 {
		t.Fatalf("opened %v, err %v", p.openedPaths, bin.openErr)
	}
	local := p.openedPaths[0]
	if data, _ := os.ReadFile(local); !bytes.Equal(data, []byte(f.read(t, "a.out"))) || !strings.HasPrefix(local, p.TempDir()) {
		t.Errorf("copy %s", local)
	}

	// A file over the limit asks first.
	maxEditSize = 64 << 10
	t.Cleanup(func() { maxEditSize = 10 << 20 })
	a.openRemoteFile(f.s, filepath.Join(f.dir, "big.log"))
	big := f.tab(t, "big.log")
	w.Frame()
	if big.state != tabTooLarge || !w.HasText(locales.EditorTooLarge("big.log", formatSize(big.total))) {
		t.Fatalf("state %d; texts %q", big.state, w.Texts())
	}
	saveShot(t, w, "editor-too-large.png")
	if err := w.Click(locales.EditorOpenAnyway()); err != nil {
		t.Fatal(err)
	}
	f.tab(t, "big.log")
	if big.state != tabReady || big.code.Buffer.LineCount() != 2001 {
		t.Fatalf("state %d err %v", big.state, big.err)
	}

	// A file that is not there fails, in the tab.
	a.openRemoteFile(f.s, filepath.Join(f.dir, "missing.txt"))
	missing := f.tab(t, "missing.txt")
	w.Frame()
	if missing.state != tabFailed || !w.HasText(locales.EditorCannotOpen("missing.txt", locales.EditorNoSuchFile())) {
		t.Errorf("state %d; texts %q", missing.state, w.Texts())
	}
}

func TestFileEditorEncodings(t *testing.T) {
	f := newEditorFixture(t)
	a, p := f.a, f.p
	a.openRemoteFile(f.s, filepath.Join(f.dir, "legacy.ini"))
	tab := f.tab(t, "legacy.ini")
	w := f.window(t)
	if !tab.notUTF8 || !w.HasText(locales.EditorNotUTF8()) {
		t.Fatalf("no suggestion; texts %q", w.Texts())
	}
	saveShot(t, w, "editor-not-utf8.png")
	if err := w.Click(locales.EditorReopenAs("GB18030")); err != nil {
		t.Fatal(err)
	}
	w.Frame()
	if got := tab.code.Buffer.String(); got != "# 配置说明\n名称=测试服务器\n" || tab.modified() || tab.enc != encGB18030 || !w.HasText("GB18030") || !w.HasText("CRLF") {
		t.Fatalf("reopened %q, modified %v", got, tab.modified())
	}
	p.SetLocale("zh-Hans")
	w.Frame()
	saveShot(t, w, "editor-gb18030-zh-Hans.png")
	p.SetLocale("")

	// Saved back in GB18030, with the line endings it had.
	w.Key(ui.Cmd, ui.KeyEnd)
	w.Type("端口=22\n")
	a.saveTab(tab, false)
	f.saved(t, tab)
	want, _ := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte("# 配置说明\r\n名称=测试服务器\r\n端口=22\r\n"))
	if got := f.read(t, "legacy.ini"); got != string(want) || tab.saveErr != nil {
		t.Errorf("saved % x, err %v", got, tab.saveErr)
	}

	// After an edit, another encoding is what it saves in, not a new
	// reading: GBK has no emoji.
	w.Type("🙂")
	a.setEncoding(tab, encGBK)
	if !strings.Contains(tab.code.Buffer.String(), "🙂") || tab.enc != encGBK {
		t.Fatal("switching after an edit read the file again")
	}
	a.saveTab(tab, false)
	var enc *unencodableError
	if !errors.As(tab.saveErr, &enc) || enc.char != '🙂' {
		t.Fatalf("save err %v", tab.saveErr)
	}
	w.Frame()
	if !w.HasText(locales.EditorSaveFailed(tab.saveErr.Error())) {
		t.Errorf("error not shown; texts %q", w.Texts())
	}

	// Switching before an edit reads the bytes again.
	a.openRemoteFile(f.s, filepath.Join(f.dir, "app.yaml"))
	yml := f.tab(t, "app.yaml")
	a.setEncoding(yml, encGBK)
	if yml.code.Buffer.String() != appYAML || yml.modified() {
		t.Error("reading ASCII as GBK changed it")
	}
}

func TestFileEditorUnsavedPrompts(t *testing.T) {
	f := newEditorFixture(t)
	a, p := f.a, f.p
	a.openRemoteFile(f.s, filepath.Join(f.dir, "app.yaml"))
	a.openRemoteFile(f.s, filepath.Join(f.dir, "deploy.sh"))
	sh := f.tab(t, "deploy.sh")
	yml := f.tab(t, "app.yaml")
	w := f.window(t)

	// Closing an unchanged tab asks nothing; the shortcut closes the tab, not
	// the window.
	w.Key(ui.Cmd, ui.KeyW)
	if len(a.fe.tabs) != 1 || a.fe.current() != yml || !a.windowOpen(WindowFileEditor) {
		t.Fatalf("tabs %d", len(a.fe.tabs))
	}
	a.openRemoteFile(f.s, filepath.Join(f.dir, "deploy.sh"))
	sh = f.tab(t, "deploy.sh")
	w.Frame()

	// A changed one asks; Cancel keeps it, Don't Save closes it.
	w.Type("x")
	w.Key(ui.Cmd, ui.KeyW)
	w.Frame()
	if a.fe.askTab != sh || !w.HasText(locales.EditorUnsavedTitle("deploy.sh")) {
		t.Fatalf("no question; texts %q", w.Texts())
	}
	saveShot(t, w, "editor-unsaved.png")
	w.Click(locales.Cancel())
	if len(a.fe.tabs) != 2 || a.fe.askTab != nil {
		t.Fatal("Cancel closed it")
	}
	w.Key(ui.Cmd, ui.KeyW)
	w.Frame()
	w.Click(locales.DontSave())
	if len(a.fe.tabs) != 1 || f.read(t, "deploy.sh") != deployScript {
		t.Fatalf("Don't Save: %d tabs", len(a.fe.tabs))
	}

	// The window's close button asks about all of them; Save All saves
	// and closes the window.
	w.Frame()
	w.Type("# more\n")
	if p.userClose(WindowFileEditor) || !a.fe.askWindow {
		t.Fatal("closed without asking")
	}
	w.Frame()
	if !w.HasText(locales.EditorUnsavedFiles(1)) {
		t.Fatalf("no question; texts %q", w.Texts())
	}
	// Quitting meanwhile asks too, whatever the options say.
	a.settings.NoConfirmClose = true
	if a.QuitRequested() {
		t.Fatal("quit with unsaved changes")
	}
	p.until(t, "the question", func() bool { return len(p.questions()) == 1 })
	if q := p.questions()[0]; q.Detail != locales.ConfirmQuitUnsaved(1) {
		t.Errorf("detail %q", q.Detail)
	}
	if err := w.Click(locales.SaveAll()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the window closed", func() bool { return !a.windowOpen(WindowFileEditor) })
	if f.read(t, "app.yaml") != "# more\n"+appYAML || len(a.fe.tabs) != 0 {
		t.Errorf("saved %q", f.read(t, "app.yaml"))
	}

	// Escape is the editor's: it closes the find bar, and never the
	// window, with nothing left to cancel or with changes.
	a.openRemoteFile(f.s, filepath.Join(f.dir, "app.yaml"))
	tab := f.tab(t, "app.yaml")
	w = f.window(t)
	w.Key(ui.Cmd, ui.KeyF)
	w.Frame()
	w.Type("port")
	w.Frame()
	if n, _ := tab.code.FindMatches(); n != 1 {
		t.Fatalf("find bar: %d matches", n)
	}
	w.Key(0, ui.KeyEscape)
	w.Frame()
	if n, _ := tab.code.FindMatches(); n != 0 || !a.windowOpen(WindowFileEditor) {
		t.Fatalf("Escape in the find bar: %d matches, window open %v", n, a.windowOpen(WindowFileEditor))
	}
	for i := range 3 {
		w.Key(0, ui.KeyEscape)
		w.Frame()
		if !a.windowOpen(WindowFileEditor) || a.fe.askWindow {
			t.Fatalf("Escape %d with nothing to cancel closed the window or asked", i+1)
		}
	}
	w.Type("x")
	w.Key(0, ui.KeyEscape)
	w.Frame()
	if !a.windowOpen(WindowFileEditor) || a.fe.askWindow || !tab.modified() {
		t.Error("Escape with changes closed the window or asked")
	}
}

func TestFileEditorDisconnect(t *testing.T) {
	f := newEditorFixture(t)
	a, p := f.a, f.p
	a.openRemoteFile(f.s, filepath.Join(f.dir, "app.yaml"))
	tab := f.tab(t, "app.yaml")
	w := f.window(t)
	w.Type("# offline\n")

	f.srv.DropConnections()
	p.until(t, "the session lost", func() bool { return f.s.status != statusConnected })
	w.Frame()
	if !w.HasText(locales.EditorDisconnected("web-1")) || a.sftpFor(tab) != nil {
		t.Fatalf("no note; texts %q", w.Texts())
	}
	saveShot(t, w, "editor-disconnected.png")
	w.Key(ui.Cmd, ui.KeyS)
	if tab.saving || !tab.modified() {
		t.Fatal("saved while disconnected")
	}

	a.reconnect(f.s)
	p.until(t, "the new SFTP client", func() bool { return f.s.status == statusConnected && f.s.sftp != nil })
	w.Key(ui.Cmd, ui.KeyS)
	f.saved(t, tab)
	if got := f.read(t, "app.yaml"); got != "# offline\n"+appYAML || tab.saveErr != nil {
		t.Errorf("saved %q, err %v", got, tab.saveErr)
	}

	// A tab whose session closed saves with another to the same host.
	w.Type("# again\n")
	a.closeSession(f.s)
	if a.sftpFor(tab) != nil {
		t.Fatal("a closed session saves")
	}
	a.openSession(f.s.Host)
	s2 := a.current()
	p.until(t, "the second session", func() bool { return s2.status == statusConnected && s2.sftp != nil })
	a.saveTab(tab, false)
	f.saved(t, tab)
	if tab.sess != s2 || !strings.Contains(f.read(t, "app.yaml"), "# again\n") {
		t.Errorf("saved %q through %v", f.read(t, "app.yaml"), tab.sess == s2)
	}
}

// On macOS Cmd+S saves and Cmd+W closes the tab, not the window; hiding
// the main window to the tray keeps an editor with changes open.
func TestFileEditorMacKeysAndTray(t *testing.T) {
	wasMacOS := macOS
	macOS = true
	t.Cleanup(func() { macOS = wasMacOS })
	f := newEditorFixture(t)
	a, p := f.a, f.p
	a.openRemoteFile(f.s, filepath.Join(f.dir, "app.yaml"))
	a.openRemoteFile(f.s, filepath.Join(f.dir, "deploy.sh"))
	f.tab(t, "app.yaml")
	sh := f.tab(t, "deploy.sh")
	w := f.window(t)
	w.Type("# mac\n")
	w.Key(ui.Super, ui.KeyS)
	f.saved(t, sh)
	if !strings.HasPrefix(f.read(t, "deploy.sh"), "# mac\r\n") {
		t.Errorf("Cmd+S: %q", f.read(t, "deploy.sh"))
	}
	w.Key(ui.Super, ui.KeyW)
	if len(a.fe.tabs) != 1 || !a.windowOpen(WindowFileEditor) {
		t.Fatalf("Cmd+W: %d tabs", len(a.fe.tabs))
	}

	a.settings.MinimizeToTray = true
	w.Frame()
	w.Type("x")
	if a.CloseRequested() || !p.inTray || !a.windowOpen(WindowFileEditor) {
		t.Fatalf("tray: in tray %v, editor open %v", p.inTray, a.windowOpen(WindowFileEditor))
	}
	a.fe.current().code.Run(editor.Undo)
	a.openOptions()
	if a.CloseRequested() || a.windowOpen(WindowFileEditor) || a.windowOpen(WindowOptions) {
		t.Error("without changes the editor closes with the others")
	}

	// Closing the last tab closes the window.
	a.openRemoteFile(f.s, filepath.Join(f.dir, "app.yaml"))
	f.tab(t, "app.yaml")
	w = f.window(t)
	w.Key(ui.Super, ui.KeyW)
	if a.windowOpen(WindowFileEditor) {
		t.Error("the last tab closed, the window stayed")
	}
}

// TestSSHFileEditor edits files on the real server FINALLYSHELL_TEST_SSH
// names, in its home directory.
func TestSSHFileEditor(t *testing.T) {
	a, p, s := connectTestServer(t)
	p.until(t, "the home directory", func() bool { return s.sftp != nil && s.files.cwd != "" && !s.files.loading })
	sc, home := s.sftp, s.files.cwd
	name := fmt.Sprintf("finallyshell-edit-%d", time.Now().UnixNano())
	script, target, link := home+"/"+name+".sh", home+"/"+name+".conf", home+"/"+name+"-link.conf"
	legacy := home + "/" + name + ".txt"
	t.Cleanup(func() {
		for _, p := range []string{script, target, link, legacy} {
			sc.Remove(p)
		}
	})
	put := func(p string, data []byte, mode os.FileMode) {
		w, err := sc.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(data)
		w.Close()
		if err := sc.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
	}
	get := func(p string) string {
		r, err := sc.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		var b bytes.Buffer
		r.WriteTo(&b)
		return b.String()
	}
	put(script, []byte(deployScript), 0o754)
	put(target, []byte("a=1\n"), 0o600)
	if err := sc.Symlink(name+".conf", link); err != nil {
		t.Fatal(err)
	}
	gb, _ := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte("名称=测试\n"))
	put(legacy, gb, 0o644)

	// Open, edit and save: CRLF and the mode stay, through posix-rename.
	a.openRemoteFile(s, script)
	f := &editorFixture{a: a, p: p, s: s}
	tab := f.tab(t, name+".sh")
	w := f.window(t)
	w.Key(ui.Cmd, ui.KeyEnd)
	w.Type("echo live\n")
	w.Key(ui.Cmd, ui.KeyS)
	f.saved(t, tab)
	if got := get(script); got != deployScript+"echo live\r\n" || tab.saveErr != nil {
		t.Fatalf("saved %q, err %v", got, tab.saveErr)
	}
	if fi, err := sc.Stat(script); err != nil || fi.Mode().Perm() != 0o754 {
		t.Errorf("mode %v %v", fi.Mode(), err)
	}
	if _, ok := sc.HasExtension("posix-rename@openssh.com"); !ok {
		t.Log("the server has no posix-rename")
	}

	// Someone else changes it: the save asks, and overwrites.
	put(script, []byte("#!/bin/sh\necho theirs\n"), 0o754)
	w.Type("# mine\n")
	a.saveTab(tab, false)
	f.saved(t, tab)
	if !tab.conflict {
		t.Fatal("no conflict")
	}
	a.saveTab(tab, true)
	f.saved(t, tab)
	if got := get(script); !strings.Contains(got, "# mine\r\n") || strings.Contains(got, "theirs") {
		t.Errorf("overwrote with %q", got)
	}

	// Through a link: the link stays, its target changes.
	a.openRemoteFile(s, link)
	lt := f.tab(t, name+"-link.conf")
	w.Frame()
	w.Key(ui.Cmd, ui.KeyEnd)
	w.Type("b=2\n")
	a.saveTab(lt, false)
	f.saved(t, lt)
	if fi, err := sc.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 || get(target) != "a=1\nb=2\n" || lt.saveErr != nil {
		t.Errorf("link: %v %v, target %q, err %v", fi, err, get(target), lt.saveErr)
	}

	// GB18030, both ways.
	a.openRemoteFile(s, legacy)
	gt := f.tab(t, name+".txt")
	a.setEncoding(gt, encGB18030)
	w.Frame()
	w.Key(ui.Cmd, ui.KeyEnd)
	w.Type("端口=22\n")
	a.saveTab(gt, false)
	f.saved(t, gt)
	want, _ := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte("名称=测试\n端口=22\n"))
	if got := get(legacy); got != string(want) {
		t.Errorf("GB18030: % x", got)
	}
	a.CloseAll()
}

func TestDecodeEncodeText(t *testing.T) {
	d := decodeText([]byte("a\r\nb\r\nc\n"), encUTF8)
	if d.text != "a\nb\nc\n" || d.ending != "\r\n" {
		t.Errorf("mixed: %+v", d)
	}
	d = decodeText([]byte("\xef\xbb\xbfhi\n"), encUTF8)
	if d.text != "hi\n" || !d.bom {
		t.Errorf("bom: %+v", d)
	}
	if out, _ := encodeText(d.text, encUTF8, "\n", true); string(out) != "\xef\xbb\xbfhi\n" {
		t.Errorf("bom kept: %q", out)
	}
	// Not UTF-8: the bytes stay as they are.
	raw := []byte("caf\xe9\n")
	if out, _ := encodeText(decodeText(raw, encUTF8).text, encUTF8, "\n", false); !bytes.Equal(out, raw) {
		t.Errorf("latin-1 round trip %q", out)
	}
	_, err := encodeText("ok\n中文\n🙂", encGBK, "\n", false)
	var enc *unencodableError
	if !errors.As(err, &enc) || enc.line != 3 || enc.char != '🙂' {
		t.Errorf("err %v", err)
	}
	if l := languageOf("/usr/local/bin/backup", "#!/usr/bin/env bash\n"); l == nil || l.Name != "Shell" {
		t.Errorf("shebang: %v", l)
	}
	if l := languageOf("/etc/systemd/system/web.service", "[Unit]\n"); l != nil {
		t.Errorf("unit file: %v", l.Name)
	}
}
