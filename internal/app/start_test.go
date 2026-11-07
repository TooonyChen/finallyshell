package app

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"finalshelly/internal/config"
	"finalshelly/locales"

	"github.com/egoist/mygo/ui"
)

// startHosts saves hosts as a user who connected to some of them:
// db-1 last, then web-1, then backup; old-web, which is deleted, too.
// The others never connected: new-app was added last, ci-runner before
// it, and staging and legacy were saved before the time a host is added
// was kept. They log in with a key that is not there, so connecting asks
// nothing and fails at once.
func startHosts(a *App) map[string]*config.Host {
	at := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	hosts := map[string]*config.Host{}
	for _, h := range []struct {
		name, addr, user, notes string
		connected               time.Duration // before at; 0 for never
		created                 time.Duration // before at; 0 for unknown
		deleted                 bool
	}{
		{"web-1", "203.0.113.10", "root", "nginx, the shop", 2 * time.Hour, 0, false},
		{"staging", "staging.example.com", "deploy", "", 0, 0, false},
		{"ci-runner", "ci.example.net", "runner", "", 0, 72 * time.Hour, false},
		{"db-1", "10.0.0.12", "postgres", "primary database", time.Minute, 30 * 24 * time.Hour, false},
		{"backup", "10.0.0.30", "admin", "", 24 * time.Hour, 0, false},
		{"new-app", "app.example.net", "ubuntu", "", 0, 10 * time.Minute, false},
		{"old-web", "203.0.113.99", "root", "", time.Hour, time.Minute, true},
		{"legacy", "legacy.example.org", "admin", "", 0, 0, false},
	} {
		x := &config.Host{ID: "id-" + h.name, Name: h.name, Addr: h.addr, User: h.user, Auth: config.AuthKey, KeyPath: "/nonexistent", Notes: h.notes}
		if h.connected != 0 {
			x.LastConnectedAt = at.Add(-h.connected)
		}
		if h.created != 0 {
			x.CreatedAt = at.Add(-h.created)
		}
		if h.deleted {
			x.DeletedAt = at
		}
		a.hosts.Add(x)
		hosts[h.name] = x
	}
	return hosts
}

func titles(hosts []*config.Host) []string {
	var s []string
	for _, h := range hosts {
		s = append(s, h.Title())
	}
	return s
}

func TestQuickHosts(t *testing.T) {
	a, _ := testApp(t)
	startHosts(a)
	all := []string{"db-1", "web-1", "backup", "new-app", "ci-runner", "staging", "legacy"}
	for _, k := range []struct {
		query  string
		want   []string
		recent int
	}{
		// Recent, the last first; then the others, the last added first,
		// and those added at a time unknown in the order they are kept.
		{"", all, 3},
		{"  ", all, 3},
		{"web", []string{"web-1"}, 1},             // the deleted host left out
		{"10.0.0", []string{"db-1", "backup"}, 2}, // by address
		{"DEPLOY", []string{"staging"}, 0},        // by user, never connected
		{"database", []string{"db-1"}, 1},         // by notes
		{"i", []string{"db-1", "web-1", "backup", "ci-runner", "staging", "legacy"}, 3}, // in the same order
		{"example", []string{"new-app", "ci-runner", "staging", "legacy"}, 0},
		{"root shop", []string{"web-1"}, 1}, // every word
		{"nothing", nil, 0},
	} {
		got, recent := quickHosts(a.hosts, k.query)
		if !slices.Equal(titles(got), k.want) || recent != k.recent {
			t.Errorf("%q: %q, %d recent; want %q, %d", k.query, titles(got), recent, k.want, k.recent)
		}
	}

	// Clearing the recent list leaves every host to the others: by when
	// they were added, db-1 a month ago.
	a.hosts.ClearRecents()
	got, recent := quickHosts(a.hosts, "")
	if want := []string{"new-app", "ci-runner", "db-1", "web-1", "staging", "backup", "legacy"}; !slices.Equal(titles(got), want) || recent != 0 {
		t.Errorf("after clearing: %q, %d recent; want %q", titles(got), recent, want)
	}
}

func TestQuickRows(t *testing.T) {
	a, _ := testApp(t)
	startHosts(a)
	show := func(rows []quickRow) []string {
		var s []string
		for _, r := range rows {
			if r.host == nil {
				s = append(s, fmt.Sprint("#", int(r.header)))
			} else {
				s = append(s, r.host.Title())
			}
		}
		return s
	}
	for _, k := range []struct {
		query string
		want  []string
	}{
		// A header over each part when there are both.
		{"", []string{"#0", "db-1", "web-1", "backup", "#1", "new-app", "ci-runner", "staging", "legacy"}},
		{"10.0.0", []string{"db-1", "backup"}},
		{"example", []string{"new-app", "ci-runner", "staging", "legacy"}},
		{"nothing", nil},
	} {
		rows := quickRows(quickHosts(a.hosts, k.query))
		if got := show(rows); !slices.Equal(got, k.want) {
			t.Errorf("%q: %q, want %q", k.query, got, k.want)
		}
	}
	rows := quickRows(quickHosts(a.hosts, ""))
	if got := hostRows(rows); !slices.Equal(got, []int{1, 2, 3, 5, 6, 7, 8}) {
		t.Errorf("host rows %v", got)
	}
}

// startApp is the app as it starts, on a new tab, with the hosts of
// startHosts, drawn by a tester.
func startApp(t *testing.T) (*App, *fakePlatform, *ui.Tester, map[string]*config.Host) {
	t.Helper()
	a, p := testApp(t)
	hosts := startHosts(a)
	a.NewTab()
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	t.Cleanup(a.CloseAll)
	tt.Frame()
	return a, p, tt, hosts
}

// shownInOrder fails unless the texts show, one under the other.
func shownInOrder(t *testing.T, tt *ui.Tester, texts ...string) {
	t.Helper()
	var last float32 = -1
	for _, s := range texts {
		r, ok := tt.Find(s)
		if !ok {
			t.Fatalf("%q not shown; texts %q", s, tt.Texts())
		}
		if r.Y <= last {
			t.Errorf("%q is not under the text before it", s)
		}
		last = r.Y
	}
}

func TestStartPageListsRecentAndClears(t *testing.T) {
	a, _, tt, _ := startApp(t)
	if len(a.sessions) != 1 || !a.sessions[0].isNewTab() || a.active != 0 {
		t.Fatalf("not on a new tab: %d rows, active %d", len(a.sessions), a.active)
	}
	shownInOrder(t, tt, locales.RecentHosts(), "db-1", "web-1", "backup", locales.OtherHosts(), "new-app", "ci-runner", "staging", "legacy")
	for _, s := range []string{locales.QuickConnect(), "10.0.0.12", "postgres", locales.NewTab(), "IP -", locales.ClearRecents()} {
		if !tt.HasText(s) {
			t.Errorf("missing %q", s)
		}
	}
	if tt.HasText("old-web") {
		t.Error("lists a deleted host")
	}

	saved := 0
	a.saveHosts = func(*config.Store) error { saved++; return nil }
	if err := tt.Click(locales.ClearRecents()); err != nil {
		t.Fatal(err)
	}
	if saved != 1 {
		t.Errorf("saved %d times", saved)
	}
	if n := len(a.hosts.Recent(0)); n != 0 {
		t.Errorf("%d recent hosts left", n)
	}
	if n := len(a.hosts.Live()); n != 7 {
		t.Errorf("%d hosts left, want 7", n)
	}
	// The hosts stay listed, by when they were added, without headers.
	tt.Frame()
	shownInOrder(t, tt, "new-app", "ci-runner", "db-1", "web-1", "staging", "backup", "legacy")
	if tt.HasText(locales.RecentHosts()) || tt.HasText(locales.OtherHosts()) {
		t.Errorf("headers with no recent hosts; texts %q", tt.Texts())
	}
	if b, ok := tt.AccessByRole(ui.RoleButton, locales.ClearRecents()); !ok || !b.Disabled {
		t.Errorf("Clear not disabled: %+v", b)
	}
	// Searching still finds them.
	if err := tt.Click(locales.SearchHosts()); err != nil {
		t.Fatal(err)
	}
	tt.Type("db")
	if !tt.HasText("db-1") || tt.HasText("new-app") {
		t.Errorf("search not filtered; texts %q", tt.Texts())
	}
}

// TestStartPageShowsNewHost saves a host from the connection editor
// opened on the start page: the page shows it at once, first of the
// hosts not connected to, and so does a duplicate made in the manager.
func TestStartPageShowsNewHost(t *testing.T) {
	a, p, tt, hosts := startApp(t)
	savedHosts(a)
	a.newConnection()
	ed := editorWindow(t, a, p)
	typeInto(t, ed, locales.FieldName(), "just-added")
	typeInto(t, ed, locales.FieldHost(), "fresh.example.com")
	typeInto(t, ed, locales.FieldPassword(), "pw")
	redraws := p.redraws
	if err := ed.Click(locales.Ok()); err != nil {
		t.Fatal(err)
	}
	if a.windowOpen(WindowEditor) {
		t.Fatal("the editor stayed open")
	}
	h := a.hosts.Host(a.editor.id)
	if h == nil || h.Name != "just-added" || time.Since(h.CreatedAt) > time.Minute || !h.LastConnectedAt.IsZero() {
		t.Fatalf("host %+v", h)
	}
	if p.redraws == redraws {
		t.Error("the main window was not redrawn")
	}
	tt.Frame()
	shownInOrder(t, tt, "backup", locales.OtherHosts(), "just-added", "new-app")

	// Duplicated, the copy is newer still.
	a.duplicateHost(hosts["staging"])
	dup := a.hosts.Live()[slices.Index(a.hosts.Live(), hosts["staging"])+1]
	if time.Since(dup.CreatedAt) > time.Minute || dup.CreatedAt.Before(h.CreatedAt) {
		t.Errorf("duplicate created %v, the host %v", dup.CreatedAt, h.CreatedAt)
	}
	tt.Frame()
	shownInOrder(t, tt, locales.OtherHosts(), dup.Title(), "just-added", "new-app")
}

// TestConnectFromNewTab connects from the start page by a click, by
// Enter in the search, and after the password: each time the new tab
// becomes the session, on its row, selected.
func TestConnectFromNewTab(t *testing.T) {
	connectBy := map[string]func(t *testing.T, a *App, tt *ui.Tester){
		"click": func(t *testing.T, a *App, tt *ui.Tester) {
			tt.ClickAt(rowCenter(t, tt, "web-1"))
		},
		"enter": func(t *testing.T, a *App, tt *ui.Tester) {
			tt.Type("shop") // the search has the focus
			tt.Key(0, ui.KeyEnter)
		},
		"arrows": func(t *testing.T, a *App, tt *ui.Tester) {
			tt.Type("0.") // db-1, web-1, backup, db-1 chosen
			tt.Key(0, ui.KeyDown)
			tt.Key(0, ui.KeyDown)
			tt.Key(0, ui.KeyUp)
			tt.Key(0, ui.KeyEnter)
		},
		"arrows past a header": func(t *testing.T, a *App, tt *ui.Tester) {
			// Recent: db-1, web-1; other connections: new-app and more.
			tt.Type("e")
			tt.Key(0, ui.KeyDown)
			tt.Key(0, ui.KeyDown)
			if st := &a.current().start; st.row < 0 || quickRows(quickHosts(a.hosts, st.query))[st.row].host.Name != "new-app" {
				t.Errorf("Down past the header chose row %d", st.row)
			}
			tt.Key(0, ui.KeyUp)
			tt.Key(0, ui.KeyEnter)
		},
		"password": func(t *testing.T, a *App, tt *ui.Tester) {
			h := a.hosts.Host("id-web-1")
			// A port nothing listens on, to fail at once.
			h.Auth, h.KeyPath, h.Addr, h.Port = config.AuthPassword, "", "127.0.0.1", 1
			tt.ClickAt(rowCenter(t, tt, "web-1"))
			// Asked once the credential store has none.
			a.plat.(*fakePlatform).until(t, "the password prompt", func() bool { return a.pwOpen })
			if a.sessions[1].Host != nil {
				t.Fatalf("no password asked (open %v)", a.pwOpen)
			}
			tt.Frame()
			tt.Type("secret")
			if err := tt.Click(locales.Connect()); err != nil {
				t.Fatal(err)
			}
		},
		"manager": func(t *testing.T, a *App, tt *ui.Tester) {
			if err := tt.Click(locales.ConnectionManager()); err != nil {
				t.Fatal(err)
			}
			manager := a.plat.(*fakePlatform).show(t, WindowManager)
			manager.Frame()
			manager.ClickAt(rowCenter(t, manager, "web-1"))
			manager.Key(0, ui.KeyEnter)
			if a.windowOpen(WindowManager) {
				t.Error("the manager stayed open")
			}
		},
	}
	for how, connect := range connectBy {
		// The Options' "select new tabs" leaves the new tab selected: no
		// tab opens.
		for _, noAuto := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/noAutoSelect=%v", how, noAuto), func(t *testing.T) {
				a, _, tt, _ := startApp(t)
				a.settings.NoAutoSelectTab = noAuto
				connectFromNewTab(t, a, tt, how, connect)
			})
		}
	}
}

func connectFromNewTab(t *testing.T, a *App, tt *ui.Tester, how string, connect func(t *testing.T, a *App, tt *ui.Tester)) {
	{
		{
			other := demoSession(a, "other", "198.51.100.7")
			a.sessions[0], a.sessions[1] = other, a.sessions[0] // the new tab second
			a.active = 1
			tab := a.sessions[1]
			tt.Frame()
			connect(t, a, tt)
			tt.Frame()
			if len(a.sessions) != 2 || a.sessions[0] != other || a.sessions[1] != tab {
				t.Fatalf("rows changed: %q", titles(hostsOf(a.sessions)))
			}
			if a.active != 1 {
				t.Errorf("selected row %d, want 1", a.active)
			}
			want := "web-1"
			if tab.Host == nil || tab.Host.Name != want {
				t.Fatalf("the tab's host is %v, want %s", tab.Host, want)
			}
			if tab.status != statusConnecting && tab.status != statusFailed {
				t.Errorf("status %d", tab.status)
			}
			if tt.HasText(locales.QuickConnect()) {
				t.Error("the start page still shows")
			}
			if !tt.HasText("web-1") || tt.HasText(locales.NotConnected()) {
				t.Errorf("the row is not the session's; texts %q", tt.Texts())
			}
		}
	}
}

// TestOpenWithNewTabsOpen opens sessions from elsewhere while new tabs
// are open: with a session selected, the new session follows the
// Options' "select new tabs", and Duplicate session opens a tab of its
// own even while a new tab is selected.
func TestOpenWithNewTabsOpen(t *testing.T) {
	for _, noAuto := range []bool{false, true} {
		t.Run(fmt.Sprintf("noAutoSelect=%v", noAuto), func(t *testing.T) {
			a, _, tt, hosts := startApp(t)
			a.settings.NoAutoSelectTab = noAuto
			if n := a.liveSessions(); n != 0 {
				t.Errorf("a new tab counts as %d live sessions", n)
			}
			s := demoSession(a, "other", "198.51.100.7")
			s.Host.Auth, s.Host.KeyPath = config.AuthKey, "/nonexistent" // asks nothing, fails at once
			a.active = 1
			a.openSession(hosts["db-1"]) // as the connection manager does
			if len(a.sessions) != 3 || !a.sessions[0].isNewTab() || a.sessions[2].Host != hosts["db-1"] {
				t.Fatalf("rows %q", titles(hostsOf(a.sessions)))
			}
			want := 2
			if noAuto {
				want = 1
			}
			if a.active != want {
				t.Errorf("selected row %d, want %d", a.active, want)
			}

			a.active = 0 // the new tab
			tt.Frame()
			tt.RightClickAt(rowCenter(t, tt, s.Host.Target()))
			if err := tt.ChooseMenuItem(locales.DuplicateSession()); err != nil {
				t.Fatalf("%v; menu %q", err, tt.Menu())
			}
			if len(a.sessions) != 4 || !a.sessions[0].isNewTab() || a.sessions[3].Host != s.Host {
				t.Fatalf("duplicated into %q", titles(hostsOf(a.sessions)))
			}
			want = 3
			if noAuto {
				want = 0
			}
			if a.active != want {
				t.Errorf("selected row %d after duplicating, want %d", a.active, want)
			}
		})
	}
}

// hostsOf are the hosts of the rows, a made-up one for new tabs.
func hostsOf(rows []*Session) []*config.Host {
	var hosts []*config.Host
	for _, s := range rows {
		h := s.Host
		if h == nil {
			h = &config.Host{Name: "(new tab)"}
		}
		hosts = append(hosts, h)
	}
	return hosts
}

func TestNewTabRows(t *testing.T) {
	a, _, tt, _ := startApp(t)
	s := demoSession(a, "web-1", "203.0.113.10") // a session after the new tab

	// The "+" button, after the options button, adds another new tab
	// and selects it.
	set, ok := tt.Find(locales.Options())
	if !ok {
		t.Fatal("no options button")
	}
	tt.ClickAt(set.X+set.W+2+set.W/2, set.Y+set.H/2)
	if len(a.sessions) != 3 || !a.sessions[2].isNewTab() || a.active != 2 {
		t.Fatalf("+ made %d rows, active %d", len(a.sessions), a.active)
	}
	tt.Frame()
	if !tt.HasText(locales.SessionsTitle(1)) {
		t.Errorf("new tabs counted as sessions; texts %q", tt.Texts())
	}

	// Ctrl+Tab goes through the new tabs and the session; the side panel
	// shows the skeleton on a new tab.
	for _, want := range []*Session{a.sessions[0], s, a.sessions[2]} {
		tt.Key(ui.Ctrl, ui.KeyTab)
		tt.Frame()
		if a.current() != want {
			t.Fatalf("Ctrl+Tab selected row %d", a.active)
		}
		if ip := tt.HasText("IP -"); ip != want.isNewTab() {
			t.Errorf("row %d: skeleton shown %v", a.active, ip)
		}
		if page := tt.HasText(locales.QuickConnect()); page != want.isNewTab() {
			t.Errorf("row %d: start page shown %v", a.active, page)
		}
	}

	// A new tab's menu has Close alone.
	tt.RightClickAt(rowCenter(t, tt, locales.NotConnected())) // the + button is "New tab" too
	if items := tt.Menu(); len(items) != 1 {
		t.Errorf("menu %q", items)
	}
	if err := tt.ChooseMenuItem(locales.Close()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(a.sessions) != 2 || a.sessions[0].isNewTab() {
		t.Fatalf("the menu closed the wrong row: %q", titles(hostsOf(a.sessions)))
	}
	if a.current() != a.sessions[1] {
		t.Errorf("selected row %d after closing another", a.active)
	}
	saveShot(t, tt, "start-rows-en.png")

	// Ctrl+Shift+W closes new tabs as it closes sessions; with none left
	// the start page still shows, and connecting there opens a tab.
	tt.Key(ui.Ctrl|ui.Shift, ui.KeyW)
	a.active = 0
	tt.Key(ui.Ctrl|ui.Shift, ui.KeyW)
	tt.Frame()
	if len(a.sessions) != 0 {
		t.Fatalf("%d rows left", len(a.sessions))
	}
	if !tt.HasText(locales.QuickConnect()) || !tt.HasText(locales.NoSessions()) || !tt.HasText("IP -") {
		t.Errorf("no start page with the list empty; texts %q", tt.Texts())
	}
	tt.ClickAt(rowCenter(t, tt, "db-1"))
	if len(a.sessions) != 1 || a.sessions[0].Host.Name != "db-1" || a.active != 0 {
		t.Errorf("connecting with no row opened %d rows, active %d", len(a.sessions), a.active)
	}
}

func TestStartPageWithoutHosts(t *testing.T) {
	a, p := testApp(t)
	a.NewTab()
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	tt.Frame()
	if !tt.HasText(locales.NoHostsTitle()) || tt.HasText(locales.QuickConnect()) {
		t.Fatalf("no empty state; texts %q", tt.Texts())
	}
	if err := tt.Click(locales.NewConnection()); err != nil {
		t.Fatal(err)
	}
	if !a.windowOpen(WindowEditor) || a.editor == nil || a.editor.id != "" {
		t.Fatalf("the editor for a new host not open: %v", a.windows)
	}
	if spec := p.windows[WindowEditor].spec; spec.Title() != locales.NewConnection() {
		t.Errorf("title %q", spec.Title())
	}
}

// TestStartPageShots draws the start page in both languages, for
// SHOT_DIR.
func TestStartPageShots(t *testing.T) {
	for _, lang := range []string{"en", "zh-Hans"} {
		t.Run(lang, func(t *testing.T) {
			a, p, tt, _ := startApp(t)
			p.SetLocale(lang)
			s := demoSession(a, "web-1", "203.0.113.10")
			db := demoSession(a, "db-1", "10.0.0.12")
			db.status = statusConnecting
			a.hosts.Remove(s.Host.ID) // the sessions of saved hosts in truth
			a.hosts.Remove(db.Host.ID)
			a.sessions[0], a.sessions[1] = s, a.sessions[0] // the new tab second
			a.active = 1
			tt.Frame()
			tt.Frame()
			saveShot(t, tt, "start-recent-"+lang+".png")
			tt.Type("10.0")
			tt.Frame()
			if hosts, _ := quickHosts(a.hosts, "10.0"); !tt.HasText("10.0.0.30") || len(hosts) != 2 {
				t.Errorf("search not filtered; texts %q", tt.Texts())
			}
			saveShot(t, tt, "start-search-"+lang+".png")

			// With the recent list cleared, as a new user's.
			tt.Key(ui.Cmd, ui.KeyA)
			tt.Key(0, ui.KeyBackspace)
			if err := tt.Click(locales.ClearRecents()); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			saveShot(t, tt, "start-cleared-"+lang+".png")

			e, ep := testApp(t)
			ep.SetLocale(lang)
			e.NewTab()
			et := ui.NewTester(e.View, 1280, 820)
			et.SetLocale(lang)
			et.Frame()
			saveShot(t, et, "start-empty-"+lang+".png")
		})
	}
}
