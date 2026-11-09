package app

import (
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
)

// renameApp opens three sessions, alpha, bravo and charlie, with alpha
// selected, and counts the saves of the hosts and the settings.
func renameApp(t *testing.T) (*App, *ui.Tester, *int) {
	t.Helper()
	a, tt, _ := closeApp(t, 0)
	saves := new(int)
	a.saveHosts = func(*config.Store) error { *saves++; return nil }
	a.saveSettings = func(config.Settings) error { *saves++; return nil }
	return a, tt, saves
}

// renameFromMenu starts renaming the row of s from its context menu.
func renameFromMenu(t *testing.T, tt *ui.Tester, s *Session) {
	t.Helper()
	tt.RightClickAt(rowCenter(t, tt, s.label()))
	if err := tt.ChooseMenuItem(locales.Rename()); err != nil {
		t.Fatalf("%v; menu %q", err, tt.Menu())
	}
	tt.Frame()
}

// checkRenaming fails unless s's row shows the field, with the focus.
func checkRenaming(t *testing.T, a *App, tt *ui.Tester, s *Session) {
	t.Helper()
	if a.rename.s != s {
		t.Fatalf("renaming %v, want %s", a.rename.s, s.Host.Title())
	}
	if !tt.Focused(locales.Rename()) {
		t.Fatal("the field does not have the focus")
	}
}

func TestRenameSession(t *testing.T) {
	for _, how := range []string{"menu", "F2"} {
		t.Run(how, func(t *testing.T) {
			a, tt, saves := renameApp(t)
			s := a.sessions[0]
			if how == "menu" {
				renameFromMenu(t, tt, s)
			} else {
				tt.ClickAt(rowCenter(t, tt, "alpha")) // the list takes the focus
				tt.Key(0, ui.KeyF2)
				tt.Frame()
			}
			checkRenaming(t, a, tt, s)
			saveShot(t, tt, "rename-field-"+how+".png")
			// The name is selected: typing replaces it.
			tt.Type("  build\n")
			tt.Key(0, ui.KeyEnter)
			tt.Frame()
			if s.name != "build" || s.label() != "build" || a.rename.s != nil {
				t.Fatalf("name %q, label %q, renaming %v", s.name, s.label(), a.rename.s != nil)
			}
			if texts := tt.Texts(); !slices.Contains(texts, "build") || slices.Contains(texts, "alpha") {
				t.Errorf("the row does not show the name; texts %q", tt.Texts())
			}
			if a.sessList.Label(0) != "build" {
				t.Errorf("type-to-find reads %q", a.sessList.Label(0))
			}
			if *saves != 0 {
				t.Errorf("%d saves", *saves)
			}
			if s.Host.Name != "alpha" {
				t.Errorf("the host is named %q", s.Host.Name)
			}
		})
	}
}

func TestRenameSessionCancel(t *testing.T) {
	cases := map[string]func(t *testing.T, a *App, tt *ui.Tester){
		"escape": func(t *testing.T, a *App, tt *ui.Tester) { tt.Key(0, ui.KeyEscape) },
		"click another row": func(t *testing.T, a *App, tt *ui.Tester) {
			tt.ClickAt(rowCenter(t, tt, "charlie"))
		},
		"click the terminal": func(t *testing.T, a *App, tt *ui.Tester) { tt.ClickAt(termSpotX, termSpotY) },
		"click a text": func(t *testing.T, a *App, tt *ui.Tester) {
			// The monitor's text, which takes no focus.
			tt.ClickAt(rowCenter(t, tt, locales.LoadAverage("0.28, 0.31, 0.27")))
		},
		"tab": func(t *testing.T, a *App, tt *ui.Tester) { tt.Key(0, ui.KeyTab) },
	}
	for name, cancel := range cases {
		t.Run(name, func(t *testing.T) {
			a, tt, _ := renameApp(t)
			s := a.sessions[0]
			s.name = "old"
			tt.Frame()
			renameFromMenu(t, tt, s)
			checkRenaming(t, a, tt, s)
			tt.Type("new")
			tt.Frame()
			cancel(t, a, tt)
			tt.Frame()
			if a.rename.s != nil || s.name != "old" {
				t.Fatalf("renaming %v, name %q", a.rename.s != nil, s.name)
			}
			if tt.HasText("new") {
				t.Errorf("the field shows; texts %q", tt.Texts())
			}
		})
	}
}

// TestRenameSessionReset gives the host's name back: an empty name, the
// host's title, and Reset name.
func TestRenameSessionReset(t *testing.T) {
	a, tt, _ := renameApp(t)
	s := a.sessions[1]
	for _, typed := range []string{"", "   ", "bravo", " bravo "} {
		s.name = "custom"
		tt.Frame()
		renameFromMenu(t, tt, s)
		checkRenaming(t, a, tt, s)
		tt.Key(0, ui.KeyBackspace)
		tt.Type(typed)
		tt.Key(0, ui.KeyEnter)
		tt.Frame()
		if s.name != "" || s.label() != "bravo" {
			t.Errorf("typed %q: name %q, label %q", typed, s.name, s.label())
		}
	}

	// Reset name shows only while the tab has a name of its own.
	tt.RightClickAt(rowCenter(t, tt, "bravo"))
	for _, item := range tt.Menu() {
		if item == locales.ResetName() {
			t.Errorf("Reset name without a name; menu %q", tt.Menu())
		}
	}
	tt.CloseMenu()
	s.name = "custom"
	tt.Frame()
	tt.RightClickAt(rowCenter(t, tt, "custom"))
	if err := tt.ChooseMenuItem(locales.ResetName()); err != nil {
		t.Fatalf("%v; menu %q", err, tt.Menu())
	}
	tt.Frame()
	if s.name != "" || !tt.HasText("bravo") {
		t.Errorf("name %q; texts %q", s.name, tt.Texts())
	}
}

// TestRenameSessionKeys checks that the field takes the keys and the
// presses the list would: the arrows and Page keys move no choice, the
// letters choose no row, Enter is the field's, and a drag from the field
// selects its text rather than moving the row.
func TestRenameSessionKeys(t *testing.T) {
	a, tt, _ := renameApp(t)
	s := a.sessions[0]
	tt.ClickAt(rowCenter(t, tt, "alpha"))
	tt.Key(0, ui.KeyF2)
	tt.Frame()
	checkRenaming(t, a, tt, s)
	for _, k := range []ui.Key{ui.KeyDown, ui.KeyUp, ui.KeyPageDown, ui.KeyPageUp, ui.KeyEnd, ui.KeyHome} {
		tt.Key(0, k)
		tt.Frame()
		if a.active != 0 || a.rename.s != s {
			t.Fatalf("%v: selected row %d, renaming %v", k, a.active, a.rename.s != nil)
		}
	}
	tt.Type("charlie")
	tt.Frame()
	if a.active != 0 || a.rename.text != "charliealpha" {
		t.Fatalf("selected row %d, text %q", a.active, a.rename.text)
	}
	// F2 again leaves the field as it is.
	tt.Key(0, ui.KeyF2)
	tt.Frame()
	if a.rename.s != s || a.rename.text != "charliealpha" {
		t.Fatalf("F2 again: text %q", a.rename.text)
	}

	field, ok := tt.Find(locales.Rename())
	if !ok {
		t.Fatalf("no field; texts %q", tt.Texts())
	}
	_, y := rowCenter(t, tt, "charlie")
	tt.Press(field.X+4, field.Y+field.H/2)
	for _, to := range []float32{field.Y + 20, field.Y + 40, y} {
		tt.Move(field.X+40, to)
		tt.Frame()
	}
	tt.Release(field.X+40, y)
	tt.Frame()
	if a.sessions[0] != s || a.active != 0 || a.rename.s != s {
		t.Fatalf("the drag moved the row: %q, selected %d, renaming %v", a.sessList.Label(0), a.active, a.rename.s != nil)
	}

	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if s.name != "charliealpha" || a.active != 0 {
		t.Fatalf("name %q, selected %d", s.name, a.active)
	}
	// The list has the keys again.
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if a.active != 1 {
		t.Errorf("Down after renaming: selected row %d", a.active)
	}
}

// TestRenameSessionWindowFocus keeps the field as the window loses the
// keyboard, as the user switches apps, and gets it back.
func TestRenameSessionWindowFocus(t *testing.T) {
	a, tt, _ := renameApp(t)
	s := a.sessions[2]
	renameFromMenu(t, tt, s)
	tt.Type("x")
	tt.SetFocused(false)
	tt.Frame()
	tt.SetFocused(true)
	tt.Frame()
	if a.rename.s != s {
		t.Fatal("the field went with the window's focus")
	}
	tt.Type("y")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if s.name != "xy" {
		t.Errorf("name %q", s.name)
	}
}

// TestRenameSessionF2Terminal leaves F2 to the terminal: it renames only
// while the session list has the focus.
func TestRenameSessionF2Terminal(t *testing.T) {
	a, tt, _ := renameApp(t)
	tt.ClickAt(termSpotX, termSpotY)
	tt.Key(0, ui.KeyF2)
	tt.Frame()
	if a.rename.s != nil {
		t.Fatal("F2 in the terminal renames the tab")
	}
}

// TestRenameNewTab offers no Rename on a new tab, which has no host.
func TestRenameNewTab(t *testing.T) {
	a, tt, _ := renameApp(t)
	a.NewTab()
	tt.Frame()
	tt.RightClickAt(rowCenter(t, tt, locales.NewTab()))
	for _, item := range tt.Menu() {
		if item == locales.Rename() {
			t.Errorf("Rename on a new tab; menu %q", tt.Menu())
		}
	}
	tt.CloseMenu()
	tt.Key(0, ui.KeyF2)
	tt.Frame()
	if a.rename.s != nil {
		t.Error("F2 renames a new tab")
	}
}

// TestRenamedSessionNames checks what follows the name: the tooltip of the
// row, the transfers, a file editor's new tabs, and the session closed.
func TestRenamedSessionNames(t *testing.T) {
	a, tt, _ := renameApp(t)
	s := a.sessions[0]
	s.name = "build"
	if got, want := renamedTooltip(s), "alpha\n"+s.Host.Target(); got != want {
		t.Errorf("tooltip %q, want %q", got, want)
	}
	tt.Frame()
	tt.Move(rowCenter(t, tt, "build"))
	tt.Advance(2 * time.Second)
	tt.Frame()
	if !slices.Contains(tt.Texts(), renamedTooltip(s)) {
		t.Errorf("no tooltip; texts %q", tt.Texts())
	}
	// Closing the tab forgets it: a new session to the host has its name.
	h := s.Host
	a.closeSession(s)
	if n := newSession(h); n.label() != "alpha" {
		t.Errorf("a new session is %q", n.label())
	}
}

// TestRenameSessionScreenshots draws a row being renamed and two renamed
// tabs to the same host, in both languages, with SHOT_DIR set.
func TestRenameSessionScreenshots(t *testing.T) {
	if os.Getenv("SHOT_DIR") == "" {
		t.Skip("SHOT_DIR not set")
	}
	a, p := testApp(t)
	web := coloredSession(t, a, "web-1", 0)
	coloredSession(t, a, "db-1", 1)
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	for _, lang := range []string{"en", "zh-Hans"} {
		p.SetLocale(lang)
		a.sessions = a.sessions[:2]
		web.name = ""
		a.active = 0
		tt.Frame()
		renameFromMenu(t, tt, web)
		tt.Type("web-1 build")
		tt.Frame()
		saveShot(t, tt, "rename-row-"+lang+".png")
		tt.Key(0, ui.KeyEnter)
		tt.Frame()
		logs := demoSession(a, "web-1", "web-1.example") // in a.sessions
		logs.Host, logs.term, logs.name = web.Host, web.term, "web-1 logs"
		a.active = 2
		tt.Frame()
		tt.Move(rowCenter(t, tt, "web-1 build"))
		tt.Advance(2 * time.Second)
		tt.Frame()
		saveShot(t, tt, "rename-two-tabs-"+lang+".png")
		tt.Move(600, 600)
		tt.Frame()
	}
}

// TestRenameSessionReconnect keeps the name through Disconnect and
// Reconnect, and gives a duplicate the host's.
func TestRenameSessionReconnect(t *testing.T) {
	srv := sshtest.Start(t)
	a, p := testApp(t)
	t.Cleanup(a.CloseAll)
	a.saveHosts = config.SaveHosts // to the test's FINALLYSHELL_DIR
	h := &config.Host{ID: "srv", Name: "server1", Addr: srv.Host, Port: srv.Port, User: "tester", Auth: config.AuthPassword, NoMonitor: true}
	a.hosts.Add(h)
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	tt.Frame()
	s := connectFake(t, a, p, h)
	tt.Frame()
	renameFromMenu(t, tt, s)
	tt.Type("deploy")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if s.label() != "deploy" {
		t.Fatalf("label %q", s.label())
	}

	tt.RightClickAt(rowCenter(t, tt, "deploy"))
	if err := tt.ChooseMenuItem(locales.Disconnect()); err != nil {
		t.Fatalf("%v; menu %q", err, tt.Menu())
	}
	tt.Frame()
	if s.status != statusClosed || s.label() != "deploy" {
		t.Fatalf("status %d, label %q", s.status, s.label())
	}

	tt.RightClickAt(rowCenter(t, tt, "deploy"))
	if err := tt.ChooseMenuItem(locales.Reconnect()); err != nil {
		t.Fatalf("%v; menu %q", err, tt.Menu())
	}
	p.until(t, "the shell again", func() bool { return s.status == statusConnected || s.status == statusFailed })
	if s.status != statusConnected || s.label() != "deploy" || !tt.HasText("deploy") {
		t.Fatalf("status %d (%v), label %q", s.status, s.err, s.label())
	}

	tt.RightClickAt(rowCenter(t, tt, "deploy"))
	if err := tt.ChooseMenuItem(locales.DuplicateSession()); err != nil {
		t.Fatalf("%v; menu %q", err, tt.Menu())
	}
	tt.Frame()
	if len(a.sessions) != 2 || a.sessions[1].label() != "server1" || s.label() != "deploy" {
		t.Fatalf("labels %q, %q", a.sessions[0].label(), a.sessions[len(a.sessions)-1].label())
	}
	// What connecting saved, as the time of the last connection, has
	// the host's name alone.
	dir := os.Getenv("FINALLYSHELL_DIR")
	files, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(files) == 0 {
		t.Fatal("nothing saved")
	}
	for _, f := range files {
		if data, err := os.ReadFile(f); err == nil && strings.Contains(string(data), "deploy") {
			t.Errorf("%s has the name: %s", filepath.Base(f), data)
		}
	}
}

// TestRenamedSessionEditorTab names a file editor's new tab after the
// session's name; a tab open already keeps the name it had.
func TestRenamedSessionEditorTab(t *testing.T) {
	f := newEditorFixture(t)
	f.a.openRemoteFile(f.s, filepath.Join(f.dir, "app.yaml"))
	before := f.tab(t, "app.yaml")
	f.s.setName(" deploy ")
	f.a.openRemoteFile(f.s, filepath.Join(f.dir, "deploy.sh"))
	after := f.tab(t, "deploy.sh")
	if before.host != "web-1" || after.host != "deploy" {
		t.Errorf("tabs of %q and %q", before.host, after.host)
	}
}
