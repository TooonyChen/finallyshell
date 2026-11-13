package app

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"finallyshell/internal/config"
	"finallyshell/internal/sshtest"
	"finallyshell/locales"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// barTerm is a connected session whose terminal writes to conn, shown in
// the main window, with the keyboard in the terminal.
func barTerm(t *testing.T) (*App, *fakePlatform, *Session, *keyConn, *ui.Tester) {
	t.Helper()
	a, p := testApp(t)
	s := demoSession(a, "web-1", "45.56.78.90")
	a.active = 0
	conn := &keyConn{done: make(chan struct{})}
	t.Cleanup(func() { close(conn.done) })
	opts := a.terminalOptions(s.Host)
	opts.Conn = conn
	term, err := terminal.New(opts)
	if err != nil {
		t.Skip("no terminal:", err)
	}
	t.Cleanup(func() { term.Close() })
	s.setTerm(term)
	term.Feed([]byte("[root@web-1 ~]# "))
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	t.Cleanup(a.history.pending.Wait)
	tt.ClickAt(760, 200) // in the terminal
	conn.sent(t, term)
	return a, p, s, conn, tt
}

// focusBar clicks into the command bar.
func focusBar(t *testing.T, tt *ui.Tester) {
	t.Helper()
	if err := tt.Click(locales.CmdBarLabel()); err != nil {
		t.Fatalf("no command bar: %v; texts %q", err, tt.Texts())
	}
}

// Enter sends the bar's line to the shell, as typed there, and keeps it
// in the host's history; Escape gives the keyboard back to the terminal,
// and the shortcut goes back and forth.
func TestCommandBarSends(t *testing.T) {
	a, _, s, conn, tt := barTerm(t)
	saveShot(t, tt, "commandbar-empty-en.png")
	focusBar(t, tt)
	tt.Type("uname -a")
	if got := conn.sent(t, s.term()); got != "" {
		t.Errorf("typing in the bar sent %q", got)
	}
	if s.bar.text != "uname -a" {
		t.Errorf("bar %q", s.bar.text)
	}
	saveShot(t, tt, "commandbar-en.png")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if got := conn.sent(t, s.term()); got != "uname -a\r" {
		t.Errorf("Enter sent %q", got)
	}
	if s.bar.text != "" {
		t.Errorf("the bar kept %q", s.bar.text)
	}
	a.history.pending.Wait()
	if list, _ := config.LoadHistory(s.Host.ID); !slices.Equal(list, []string{"uname -a"}) {
		t.Errorf("history file %q", list)
	}
	tt.Type("df -h")
	tt.Key(0, ui.KeyEnter)
	tt.Type("uname -a")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	conn.sent(t, s.term())
	a.history.pending.Wait()
	if got := a.hostHistory(s.Host.ID).cmds; !slices.Equal(got, []string{"df -h", "uname -a"}) {
		t.Errorf("history %q", got)
	}

	// Escape: the terminal has the keys again.
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	tt.Key(ui.Ctrl, ui.KeyC)
	if got := conn.sent(t, s.term()); got != "\x03" {
		t.Errorf("Ctrl+C after Escape sent %q", got)
	}
	// The shortcut moves the keys to the bar, and back.
	k := a.shortcut(scFocusBar)
	tt.Key(k.Mods, k.Key)
	tt.Frame()
	if !s.bar.focused {
		t.Fatal("the shortcut did not focus the bar")
	}
	tt.Type("ls")
	if got := conn.sent(t, s.term()); got != "" {
		t.Errorf("typing after the shortcut sent %q", got)
	}
	tt.Key(k.Mods, k.Key)
	tt.Frame()
	if s.bar.focused {
		t.Error("the shortcut again kept the bar focused")
	}
	tt.Key(ui.Ctrl, ui.KeyC)
	if got := conn.sent(t, s.term()); got != "\x03" {
		t.Errorf("Ctrl+C after the shortcut sent %q", got)
	}

	p := a.plat.(*fakePlatform)
	p.SetLocale("zh-Hans")
	tt.Frame()
	saveShot(t, tt, "commandbar-zh.png")
	p.SetLocale("en")
}

// A session that is not connected shows the bar disabled.
func TestCommandBarDisabled(t *testing.T) {
	a, p := testApp(t)
	s := demoSession(a, "web-1", "45.56.78.90")
	a.active = 0
	s.status, s.err = statusClosed, errConnectionLost
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	info, ok := tt.Access(locales.CmdBarLabel())
	if !ok || !info.Disabled {
		t.Errorf("bar of a closed session: %+v, found %v", info, ok)
	}
}

// barServer is a session to an in-process server whose SFTP serves home,
// a made-up home folder, shown in the main window with the keyboard in
// the bar.
func barServer(t *testing.T) (*App, *fakePlatform, *Session, *ui.Tester, string) {
	t.Helper()
	home := t.TempDir()
	for _, d := range []string{"etc/ssh", "etc/nginx", "docs", ".config"} {
		os.MkdirAll(filepath.Join(home, d), 0o755)
	}
	for name, data := range map[string]string{
		"etc/hosts": "", "etc/hosts.allow": "", "etc/hostname": "", "etc/.secret": "", "etc/fstab": "",
		"notes.txt": "", "my file.txt": "", "deploy.sh": "",
		".bash_history": "ls -la\n#1700000000\nyum -y install epel-release\ncd /etc\n",
		".zsh_history":  ": 1700000000:0;git status\n",
	} {
		os.WriteFile(filepath.Join(home, name), []byte(data), 0o644)
	}
	srv := sshtest.StartSFTP(t, home)
	a, p := testApp(t)
	t.Cleanup(a.history.pending.Wait)
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
		return s.status == statusFailed || s.files.cwd == home && !s.files.loading
	})
	if s.status != statusConnected {
		t.Fatal(describeError(s.err))
	}
	t.Cleanup(a.CloseAll)
	s.term().Feed([]byte("tester@web-1:~$ "))
	focusBar(t, tt)
	return a, p, s, tt, home
}

// popupDone waits for what the popup reads.
func popupDone(t *testing.T, p *fakePlatform, a *App, s *Session) popupContent {
	t.Helper()
	var v popupContent
	p.until(t, "the popup", func() bool {
		v = a.popupView(s, s.bar.text, s.bar.caret)
		return !v.loading
	})
	p.tt.Frame()
	return a.popupView(s, s.bar.text, s.bar.caret)
}

func itemNames(v popupContent) []string {
	var names []string
	for _, it := range v.items {
		n := it.name
		if it.dir {
			n += "/"
		}
		names = append(names, n)
	}
	return names
}

// Tab on a path lists the server's folder in columns; typing filters it,
// Backspace in an empty filter goes up, Enter takes the entry.
func TestCommandBarPaths(t *testing.T) {
	a, p, s, tt, home := barServer(t)
	tt.Type("cat etc/")
	tt.Key(0, ui.KeyTab)
	if s.bar.popup != popupPaths {
		t.Fatalf("Tab on a path: popup %d", s.bar.popup)
	}
	v := popupDone(t, p, a, s)
	want := []string{"nginx/", "ssh/", "fstab", "hostname", "hosts", "hosts.allow", ".secret"}
	if got := itemNames(v); !slices.Equal(got, want) || v.title != filepath.ToSlash(filepath.Join(home, "etc")) {
		t.Errorf("etc: %q in %q, want %q", got, v.title, want)
	}
	for _, name := range []string{"nginx/", "hosts.allow", ".secret"} {
		if !tt.HasText(name) {
			t.Errorf("the picker shows no %q; texts %q", name, tt.Texts())
		}
	}
	saveShot(t, tt, "commandbar-paths-en.png")
	p.SetLocale("zh-Hans")
	tt.Frame()
	saveShot(t, tt, "commandbar-paths-zh.png")
	p.SetLocale("en")

	tt.Type("ho")
	tt.Frame()
	v = a.popupView(s, s.bar.text, s.bar.caret)
	if got := itemNames(v); !slices.Equal(got, []string{"hostname", "hosts", "hosts.allow"}) {
		t.Errorf("filtered by ho: %q", got)
	}
	tt.Key(0, ui.KeyRight) // hosts
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if s.bar.text != "cat etc/hosts " || s.bar.popup != popupNone {
		t.Errorf("Enter on hosts: %q, popup %d", s.bar.text, s.bar.popup)
	}

	// Backspace in an empty filter goes up, from etc/ssh/ to etc/, then
	// to the home folder.
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("ls etc/ssh/")
	tt.Key(0, ui.KeyTab)
	popupDone(t, p, a, s)
	tt.Key(0, ui.KeyBackspace)
	tt.Frame()
	if s.bar.text != "ls etc/" || s.bar.popup != popupPaths {
		t.Errorf("Backspace up: %q, popup %d", s.bar.text, s.bar.popup)
	}
	tt.Key(0, ui.KeyBackspace)
	tt.Frame()
	if s.bar.text != "ls " || s.bar.popup != popupPaths {
		t.Errorf("Backspace up again: %q, popup %d", s.bar.text, s.bar.popup)
	}
	v = popupDone(t, p, a, s)
	if got := itemNames(v); !slices.Contains(got, "docs/") || !slices.Contains(got, "my file.txt") || got[len(got)-1] != ".zsh_history" {
		t.Errorf("home: %q", got)
	}
	// A folder taken lists its entries; a name with a blank is escaped.
	tt.Type("do")
	tt.Key(0, ui.KeyTab)
	tt.Frame()
	if s.bar.text != "ls docs/" || s.bar.popup != popupPaths {
		t.Errorf("Tab on docs: %q, popup %d", s.bar.text, s.bar.popup)
	}
	tt.Key(0, ui.KeyBackspace) // up from docs/
	tt.Type("my")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if s.bar.text != `ls my\ file.txt ` {
		t.Errorf("a name with a blank: %q", s.bar.text)
	}

	// Escape closes the popup, and Escape again leaves the bar.
	tt.Type("/e")
	tt.Key(0, ui.KeyTab)
	if s.bar.popup != popupPaths {
		t.Fatal("no picker for /e")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if s.bar.popup != popupNone || !s.bar.focused {
		t.Errorf("Escape: popup %d, focused %v", s.bar.popup, s.bar.focused)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if s.bar.focused {
		t.Error("Escape twice kept the bar focused")
	}
}

// Tab on the first word lists the commands; over SFTP when the host
// allows no exec channel, as the in-process server has none.
func TestCommandBarCommands(t *testing.T) {
	a, p, s, tt, _ := barServer(t)
	tt.Type("l")
	tt.Key(0, ui.KeyTab)
	if s.bar.popup != popupCommands {
		t.Fatalf("Tab on the first word: popup %d", s.bar.popup)
	}
	v := popupDone(t, p, a, s)
	if !slices.Contains(itemNames(v), "ls") {
		t.Fatalf("no ls in %q (%s)", itemNames(v), v.err)
	}
	saveShot(t, tt, "commandbar-commands-en.png")
	p.SetLocale("zh-Hans")
	tt.Frame()
	saveShot(t, tt, "commandbar-commands-zh.png")
	p.SetLocale("en")
	tt.Type("s")
	tt.Frame()
	v = a.popupView(s, s.bar.text, s.bar.caret)
	if names := itemNames(v); len(names) == 0 || names[0] != "ls" {
		t.Errorf("ls: %q", names)
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if s.bar.text != "ls " || s.bar.popup != popupNone {
		t.Errorf("Enter: %q, popup %d", s.bar.text, s.bar.popup)
	}
	// Tab on a later word is the path picker of the current folder.
	tt.Key(0, ui.KeyTab)
	if s.bar.popup != popupPaths {
		t.Errorf("Tab on the second word: popup %d", s.bar.popup)
	}
	if v := popupDone(t, p, a, s); !slices.Contains(itemNames(v), "notes.txt") {
		t.Errorf("current folder: %q", itemNames(v))
	}
}

// Up lists the commands sent before that start with what is typed, the
// host's then the server's; Left and Right pick a field.
func TestCommandBarHistory(t *testing.T) {
	a, p, s, tt, home := barServer(t)
	config.AddHistory(s.Host.ID, "yum update")
	config.AddHistory(s.Host.ID, "systemctl status nginx")
	tt.Key(0, ui.KeyUp)
	if s.bar.popup != popupHistory {
		t.Fatalf("Up: popup %d", s.bar.popup)
	}
	v := popupDone(t, p, a, s)
	want := []string{"systemctl status nginx", "yum update", "git status", "cd /etc", "yum -y install epel-release", "ls -la"}
	if got := itemNames(v); !slices.Equal(got, want) {
		t.Errorf("history %q, want %q", got, want)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()

	// The History button at the bar's end opens it too, the keys staying
	// in the bar, and again closes it.
	if err := tt.Click(locales.CmdBarHistory()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.bar.popup != popupHistory || !s.bar.focused {
		t.Fatalf("History button: popup %d, focused %v", s.bar.popup, s.bar.focused)
	}
	tt.Key(0, ui.KeyUp)
	tt.Frame()
	if s.bar.sel != 1 {
		t.Errorf("Up after the button: sel %d", s.bar.sel)
	}
	saveShot(t, tt, "commandbar-history-button-en.png")
	if err := tt.Click(locales.CmdBarHistory()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.bar.popup != popupNone {
		t.Errorf("History button again: popup %d", s.bar.popup)
	}

	tt.Type("yum")
	tt.Key(ui.Alt, ui.KeyH)
	tt.Frame()
	if s.bar.popup != popupHistory {
		t.Fatalf("Alt+H: popup %d", s.bar.popup)
	}
	v = popupDone(t, p, a, s)
	if got := itemNames(v); !slices.Equal(got, []string{"yum update", "yum -y install epel-release"}) {
		t.Errorf("yum: %q", got)
	}
	tt.Key(0, ui.KeyUp)    // the older one
	tt.Key(0, ui.KeyLeft)  // its last field
	tt.Key(0, ui.KeyRight) // round to the whole command
	tt.Key(0, ui.KeyLeft)
	tt.Frame()
	if s.bar.sel != 1 || s.bar.field != 3 || !tt.HasText("epel-release") {
		t.Errorf("sel %d field %d; texts %q", s.bar.sel, s.bar.field, tt.Texts())
	}
	saveShot(t, tt, "commandbar-history-en.png")
	p.SetLocale("zh-Hans")
	tt.Frame()
	saveShot(t, tt, "commandbar-history-zh.png")
	p.SetLocale("en")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if s.bar.text != "epel-release" || s.bar.popup != popupNone {
		t.Errorf("Enter on a field: %q, popup %d", s.bar.text, s.bar.popup)
	}
	tt.Key(ui.Ctrl, ui.KeyA)
	tt.Type("rpm -q ")
	tt.Key(0, ui.KeyUp)
	tt.Key(0, ui.KeyEnter) // nothing starts with it: sent, as without it
	tt.Frame()
	if s.bar.text != "" || s.bar.popup != popupNone {
		t.Errorf("Enter on no matches: %q, popup %d", s.bar.text, s.bar.popup)
	}

	// The whole command, with Enter on none.
	tt.Key(ui.Ctrl, ui.KeyA)
	tt.Type("sys")
	tt.Key(0, ui.KeyUp)
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if s.bar.text != "systemctl status nginx" {
		t.Errorf("Enter on a command: %q", s.bar.text)
	}

	// Without the server's history, only the host's.
	a.changeSettings(func() { a.settings.NoServerHistory = true })
	tt.Key(ui.Ctrl, ui.KeyA)
	tt.Key(0, ui.KeyBackspace)
	tt.Key(0, ui.KeyUp)
	v = popupDone(t, p, a, s)
	if got := itemNames(v); !slices.Equal(got, []string{"rpm -q", "systemctl status nginx", "yum update"}) {
		t.Errorf("without the server's: %q", got)
	}
	a.changeSettings(func() { a.settings.NoServerHistory = false })

	// Clear list empties it, and the file.
	tt.Frame()
	if err := tt.Click(locales.CmdBarClearList()); err != nil {
		t.Fatal(err)
	}
	a.history.pending.Wait()
	tt.Frame()
	if v := a.popupView(s, s.bar.text, s.bar.caret); len(v.items) != 0 {
		t.Errorf("after Clear list: %q", itemNames(v))
	}
	if list, _ := config.LoadHistory(s.Host.ID); list != nil {
		t.Errorf("the file after Clear list: %q", list)
	}
	if _, err := os.Stat(filepath.Join(home, ".bash_history")); err != nil {
		t.Errorf("the server's history went: %v", err)
	}
}

// The switches apply at once: no bar, no popups, nothing read.
func TestCommandBarSwitches(t *testing.T) {
	a, _, s, conn, tt := barTerm(t)
	a.changeSettings(func() { a.settings.NoCommandBarPaths, a.settings.NoCommandBarCommands = true, true })
	focusBar(t, tt)
	tt.Type("cd /")
	tt.Key(0, ui.KeyTab)
	tt.Frame()
	if s.bar.popup != popupNone || s.bar.text != "cd /" {
		t.Errorf("Tab with both off: popup %d, %q", s.bar.popup, s.bar.text)
	}
	if !tt.HasText(locales.CmdBarHistory()) {
		t.Error("no History button")
	}
	a.changeSettings(func() { a.settings.NoCommandBarHistory = true })
	tt.Key(0, ui.KeyUp)
	tt.Key(ui.Alt, ui.KeyH)
	tt.Frame()
	if tt.HasText(locales.CmdBarHistory()) {
		t.Error("the History button shows with the history off")
	}
	if s.bar.popup != popupNone {
		t.Errorf("Up with the history off: popup %d", s.bar.popup)
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if got := conn.sent(t, s.term()); got != "cd /\r" {
		t.Errorf("Enter sent %q", got)
	}
	a.history.pending.Wait()
	if list, _ := config.LoadHistory(s.Host.ID); list != nil {
		t.Errorf("kept with the history off: %q", list)
	}

	// An open popup closes as its switch goes off.
	a.changeSettings(func() { a.settings.NoCommandBarHistory = false })
	tt.Key(0, ui.KeyUp)
	tt.Frame()
	if s.bar.popup != popupHistory {
		t.Fatalf("Up: popup %d", s.bar.popup)
	}
	a.changeSettings(func() { a.settings.NoCommandBarHistory = true })
	tt.Frame()
	if s.bar.popup != popupNone {
		t.Errorf("history off with its picker open: popup %d", s.bar.popup)
	}

	// The bar off: gone, its shortcut the terminal's, nothing read.
	a.history.pending.Wait()
	a.history.hosts = nil
	s.bar = cmdBar{}
	a.changeSettings(func() { a.settings.NoCommandBar = true })
	tt.Frame()
	if _, ok := tt.Access(locales.CmdBarLabel()); ok {
		t.Error("the bar shows while off")
	}
	tt.ClickAt(760, 200)
	k := a.shortcut(scFocusBar)
	tt.Key(k.Mods, k.Key)
	tt.Frame()
	if s.bar.focus || s.bar.focused {
		t.Error("the shortcut focused a hidden bar")
	}
	tt.Key(ui.Ctrl, ui.KeyC)
	if got := conn.sent(t, s.term()); !strings.HasSuffix(got, "\x03") {
		t.Errorf("the terminal lost the keys: %q", got)
	}
	if a.history.hosts != nil || s.bar.data.cmds != nil || s.bar.data.server != nil || s.bar.data.dirs != nil {
		t.Error("read something with the bar off")
	}
	a.changeSettings(func() { a.settings.NoCommandBar = false })
	tt.Frame()
	if _, ok := tt.Access(locales.CmdBarLabel()); !ok {
		t.Error("no bar once on again")
	}
}

// The Command bar page turns the bar and its parts off, and clears the
// history of every host once asked.
func TestCommandBarPage(t *testing.T) {
	a, p := testApp(t)
	t.Cleanup(a.history.pending.Wait)
	a.openOptions()
	tt := p.show(t, WindowOptions)
	if err := tt.Click(locales.OptionsCommandBar()); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{locales.OptionShowCommandBar(), locales.OptionCmdBarPaths(), locales.OptionCmdBarHistory(),
		locales.OptionCmdBarCommands(), locales.OptionServerHistory(), locales.ClearAllHistory()} {
		if !tt.HasText(s) {
			t.Errorf("no %q on the page; texts %q", s, tt.Texts())
		}
	}
	// The keys, with the focus shortcut as it is bound.
	for _, s := range []string{locales.CmdBarKeys(), locales.CmdBarKeyEnter(), locales.CmdBarKeyFields(), a.shortcutLabel(scFocusBar)} {
		if !tt.HasText(s) {
			t.Errorf("no %q among the keys; texts %q", s, tt.Texts())
		}
	}
	rebind(&a.settings.Shortcuts, runtime.GOOS, scFocusBar, keyCombo{ui.Ctrl | ui.Alt, ui.KeyJ})
	tt.Frame()
	if l := a.shortcutLabel(scFocusBar); !tt.HasText(l) {
		t.Errorf("the rebound key %q is not listed", l)
	}
	a.settings.Shortcuts = config.Shortcuts{}
	tt.Frame()
	if got := a.commandBarKeys("darwin"); got[0].keys != "↩" || got[2].keys != "↑ / ⌥H" {
		t.Errorf("macOS keys %q", got)
	}
	saveShot(t, tt, "options-commandbar-en.png")
	tt.Scroll(500, 300, 0, 1000) // down to the history
	tt.Frame()
	saveShot(t, tt, "options-commandbar-more-en.png")
	p.SetLocale("zh-Hans")
	tt.Frame()
	saveShot(t, tt, "options-commandbar-more-zh.png")
	tt.Scroll(500, 300, 0, -1000)
	tt.Frame()
	saveShot(t, tt, "options-commandbar-zh.png")
	p.SetLocale("en")
	tt.Frame()

	for _, c := range []struct {
		label string
		got   func() bool
	}{
		{locales.OptionCmdBarPaths(), func() bool { return a.settings.NoCommandBarPaths }},
		{locales.OptionCmdBarHistory(), func() bool { return a.settings.NoCommandBarHistory }},
		{locales.OptionCmdBarCommands(), func() bool { return a.settings.NoCommandBarCommands }},
		{locales.OptionShowCommandBar(), func() bool { return a.settings.NoCommandBar }},
	} {
		if err := tt.Click(c.label); err != nil {
			t.Fatal(err)
		}
		if !c.got() {
			t.Errorf("%q did not turn it off", c.label)
		}
	}
	if saved := config.LoadSettings(); !saved.NoCommandBar || !saved.NoCommandBarPaths {
		t.Errorf("saved %+v", saved)
	}
	// With the bar off, its parts can't change.
	if info, _ := tt.Access(locales.OptionCmdBarPaths()); !info.Disabled {
		t.Errorf("paths with the bar off: %+v", info)
	}
	tt.Click(locales.OptionShowCommandBar())
	tt.Click(locales.OptionServerHistory()) // disabled, as the history is off
	if a.settings.NoServerHistory {
		t.Error("server history changed with the history picker off")
	}
	tt.Click(locales.OptionCmdBarHistory())
	tt.Click(locales.OptionServerHistory())
	if !a.settings.NoServerHistory {
		t.Error("server history not turned off")
	}

	tt.Scroll(500, 300, 0, 1000) // down to the button
	tt.Frame()
	config.AddHistory("h1", "ls")
	config.AddHistory("h2", "df")
	p.answer = false
	if err := tt.Click(locales.ClearAllHistory()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the question", func() bool { return len(p.questions()) == 1 })
	if list, _ := config.LoadHistory("h1"); list == nil {
		t.Error("cleared without a yes")
	}
	p.answer = true
	tt.Click(locales.ClearAllHistory())
	p.until(t, "the clearing", func() bool {
		a.history.pending.Wait()
		_, err := os.Stat(config.HistoryDir())
		return os.IsNotExist(err)
	})
	if q := p.questions()[1]; q.Message != locales.ClearAllHistoryTitle() {
		t.Errorf("question %+v", q)
	}
}

// A host deleted for good takes its history with it.
// The history files are written in the order asked: a command sent
// before a Clear or a host's deletion does not come back after it.
func TestCommandBarHistoryOrder(t *testing.T) {
	a, _ := testApp(t)
	t.Cleanup(a.history.pending.Wait)
	for range 20 {
		a.addHistory("h1", "first")
		a.addHistory("h1", "second")
		a.clearAllHistory()
		a.addHistory("h1", "last")
		a.history.pending.Wait()
		if list, _ := config.LoadHistory("h1"); !slices.Equal(list, []string{"last"}) {
			t.Fatalf("add, clear, add: %q", list)
		}
		a.addHistory("h2", "ls")
		a.deleteHostHistory("h2")
		a.history.pending.Wait()
		if _, err := os.Stat(filepath.Join(config.HistoryDir(), "h2.txt")); !os.IsNotExist(err) {
			t.Fatalf("add, delete the host: %v", err)
		}
		for _, c := range []string{"a", "b", "c", "a"} {
			a.addHistory("h3", c)
		}
		a.history.pending.Wait()
		if list, _ := config.LoadHistory("h3"); !slices.Equal(list, []string{"b", "c", "a"}) {
			t.Fatalf("in order: %q", list)
		}
		config.ClearAllHistory()
		a.history.hosts = nil
	}
}

// A remote name with a control character, as a newline that would run
// what follows it as a command, shows in the picker but can't be
// inserted; history lines with one are dropped.
func TestCommandBarControlNames(t *testing.T) {
	a, p, s, tt, home := barServer(t)
	for _, name := range []string{"innocent\nid", "a\x1b[2Jb"} {
		if err := os.WriteFile(filepath.Join(home, name), nil, 0o644); err != nil {
			t.Skip("the file system takes no such name:", err)
		}
	}
	os.WriteFile(filepath.Join(home, ".bash_history"), []byte("ls\necho \x1b[2Jgone\nprintf '\\x1b'\n"), 0o644)
	tt.Type("cat ")
	tt.Key(0, ui.KeyTab)
	v := popupDone(t, p, a, s)
	bad := map[string]bool{}
	for _, it := range v.items {
		if it.bad {
			bad[it.name] = true
		}
	}
	if !bad["innocent\nid"] || !bad["a\x1b[2Jb"] || len(bad) != 2 {
		t.Fatalf("bad names %v in %q", bad, itemNames(v))
	}
	if !tt.HasText(`innocent\nid`) {
		t.Errorf("the name is not shown escaped; texts %q", tt.Texts())
	}
	saveShot(t, tt, "commandbar-control-en.png")
	tt.Type("inn")
	tt.Frame()
	for _, k := range []ui.Key{ui.KeyEnter, ui.KeyTab} {
		tt.Key(0, k)
		tt.Frame()
		if s.bar.text != "cat inn" || s.bar.popup != popupPaths {
			t.Errorf("%v on a bad name: %q, popup %d", k, s.bar.text, s.bar.popup)
		}
	}
	if err := tt.Click(`innocent\nid`); err == nil {
		tt.Frame()
	}
	if s.bar.text != "cat inn" {
		t.Errorf("a click on a bad name: %q", s.bar.text)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Key(0, ui.KeyBackspace)
	tt.Key(0, ui.KeyUp)
	v = popupDone(t, p, a, s)
	if got := itemNames(v); !slices.Equal(got, []string{"git status", `printf '\x1b'`, "ls"}) {
		t.Errorf("history with an escape sequence: %q", got)
	}
	if got := commandNameList([]string{"ls", "evil\x1b[2J", "x\u0085y"}); !slices.Equal(got, []string{"ls"}) {
		t.Errorf("command names %q", got)
	}
}

func TestCommandBarHistoryDeletedWithHost(t *testing.T) {
	a, _ := testApp(t)
	h := &config.Host{ID: config.NewHostID(), Name: "web-1", Addr: "10.0.0.1", User: "root"}
	a.hosts.Add(h)
	a.addHistory(h.ID, "ls")
	a.history.pending.Wait()
	if list, _ := config.LoadHistory(h.ID); len(list) != 1 {
		t.Fatalf("history %q", list)
	}
	a.deleteHostForever(h)
	a.history.pending.Wait()
	if _, err := os.Stat(filepath.Join(config.HistoryDir(), h.ID+".txt")); !os.IsNotExist(err) {
		t.Errorf("the file is still there: %v", err)
	}
}

func TestCommandBarWords(t *testing.T) {
	for _, c := range []struct {
		text         string
		caret        int
		start, end   int
		dir, filter  string
		parent       string
		parentAtRoot bool
	}{
		{"cd /etc/ss", 10, 3, 10, "/etc/", "ss", "/", false},
		{"cd /etc/", 8, 3, 8, "/etc/", "", "/", false},
		{"ls my\\ fi", 9, 3, 9, "", "my fi", "../", false},
		{"ls a\\ b/c", 9, 3, 9, "a\\ b/", "c", "", false},
		{"cat ../x", 8, 4, 8, "../", "x", "../../", false},
		{"vi ~/.bashrc", 12, 3, 12, "~/", ".bashrc", "", true},
		{"cd /", 4, 3, 4, "/", "", "", true},
		{"ls dir/sub/ more", 11, 3, 11, "dir/sub/", "", "dir/", false},
		{"ls dir/sub/more", 7, 3, 15, "dir/", "", "", false},
	} {
		w := parsePathWord([]rune(c.text), c.caret)
		if w.start != c.start || w.end != c.end || w.dirRaw != c.dir || w.filter != c.filter {
			t.Errorf("%q at %d: %+v", c.text, c.caret, w)
		}
		up, ok := (&App{}).parentDir(&Session{}, w)
		if c.parentAtRoot {
			if ok {
				t.Errorf("%q: up to %q at the top", c.text, up)
			}
		} else if !ok || up != c.parent {
			t.Errorf("%q: up %q %v, want %q", c.text, up, ok, c.parent)
		}
	}
	if got := escapePath(`my file (1)$.txt`); got != `my\ file\ \(1\)\$.txt` {
		t.Errorf("escaped %q", got)
	}
	for cmd, want := range map[string][]string{
		"yum -y install epel-release":   {"yum", "-y", "install", "epel-release"},
		`echo "a b"  'c d' e\ f`:        {"echo", `"a b"`, `'c d'`, `e\ f`},
		"  ls  ":                        {"ls"},
		`trail\`:                        {`trail\`},
		"":                              nil,
		`grep -r "TODO" --include=*.go`: {"grep", "-r", `"TODO"`, "--include=*.go"},
	} {
		var got []string
		for _, f := range commandFields(cmd) {
			got = append(got, string([]rune(cmd)[f.start:f.end]))
		}
		if !slices.Equal(got, want) {
			t.Errorf("fields of %q: %q, want %q", cmd, got, want)
		}
	}
	if got := commandNameList([]string{"ls", " cd ", "", "ls", "a b", "x/y", "awk"}); !slices.Equal(got, []string{"awk", "cd", "ls"}) {
		t.Errorf("names %q", got)
	}
	for in, want := range map[string]string{"/": "~", "": "~", "/etc/": "~/etc", "x/../y": "~/y"} {
		if got := homeKey(in); got != want {
			t.Errorf("homeKey(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSSHCommandBar runs the bar against a real server, named by
// FINALLYSHELL_TEST_SSH as TestSSH's: the folders, the command names
// over an exec channel, the server's history, and sending.
func TestSSHCommandBar(t *testing.T) {
	a, p, s := connectTestServer(t)
	t.Cleanup(a.history.pending.Wait)
	t.Cleanup(a.CloseAll)
	// The shell writes no history of the test's commands as it ends.
	s.term().Send([]byte("unset HISTFILE\r"))
	p.until(t, "SFTP", func() bool { return s.sftp != nil && s.files.cwd != "" && !s.files.loading })
	home := s.files.cwd

	// A history file of the test's; the user's own is put back after.
	hist := home + "/.bash_history"
	old, readErr := readRemote(s, hist)
	t.Cleanup(func() {
		sc, err := sftpTo()
		if err != nil {
			t.Errorf("putting back %s: %v", hist, err)
			return
		}
		defer sc.Close()
		if readErr != nil {
			sc.Remove(hist)
			return
		}
		f, err := sc.Create(hist)
		if err == nil {
			_, err = f.Write(old)
			f.Close()
		}
		if err != nil {
			t.Errorf("putting back %s: %v", hist, err)
		}
	})
	f, err := s.sftp.Create(hist)
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("#1700000000\necho finallyshell-marker-104\nls -la /etc\n"))
	f.Close()

	// Folders.
	etc := a.listing(s, "/etc")
	homeDir := a.listing(s, "~")
	p.until(t, "the listings", func() bool { return !etc.loading && !homeDir.loading })
	var names []string
	for _, e := range etc.entries {
		names = append(names, e.name)
	}
	if etc.err != "" || !slices.Contains(names, "passwd") || !slices.Contains(names, "ssh") {
		t.Errorf("/etc: %q, %s", names, etc.err)
	}
	if homeDir.dir != home || homeDir.err != "" {
		t.Errorf("~: %q, %s", homeDir.dir, homeDir.err)
	}

	// Command names, from bash's compgen: its builtins are there.
	cmds := a.commandNames(s)
	p.until(t, "the command names", func() bool { return !cmds.loading })
	for _, want := range []string{"ls", "compgen", "cd", "grep"} {
		if !slices.Contains(cmds.names, want) {
			t.Errorf("no %q in %d names (%s)", want, len(cmds.names), cmds.err)
		}
	}

	// The server's history, after the host's.
	a.addHistory(s.Host.ID, "uptime")
	p.until(t, "the history", func() bool {
		_, loading := a.historyFor(s)
		return !loading
	})
	if got, _ := a.historyFor(s); len(got) < 3 || got[0] != "uptime" || !slices.Contains(got, "echo finallyshell-marker-104") {
		t.Errorf("history %q", got)
	}

	// Enter sends the line to the shell.
	a.sendBar(s, barEdit{text: "echo bar-$((6*7))", set: func(string) {}})
	p.until(t, "the output", func() bool { return strings.Contains(s.term().Text(), "bar-42") })
}

// readRemote reads the file p of s's server.
func readRemote(s *Session, p string) ([]byte, error) {
	f, err := s.sftp.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// sftpTo is an SFTP client of its own to the server of
// FINALLYSHELL_TEST_SSH, to put back what a test changed once the app's
// connection is gone.
func sftpTo() (*sftp.Client, error) {
	cred, addr, _ := strings.Cut(os.Getenv("FINALLYSHELL_TEST_SSH"), "@")
	user, pass, _ := strings.Cut(cred, ":")
	client, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{
		User: user, Auth: []ssh.AuthMethod{ssh.Password(pass)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	})
	if err != nil {
		return nil, err
	}
	sc, err := sftp.NewClient(client)
	if err != nil {
		client.Close()
		return nil, err
	}
	return sc, nil
}
