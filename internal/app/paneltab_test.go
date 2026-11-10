package app

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"finallyshell/internal/config"
	"finallyshell/internal/monitor"
	"finallyshell/internal/sshtest"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
	"golang.org/x/crypto/ssh"
)

// rowNames is the session list as its rows' text: "web-1", and a panel
// row as "web-1 | Network".
func rowNames(a *App) []string {
	var out []string
	for _, s := range a.sessions {
		out = append(out, listLabel(s))
	}
	return out
}

// panelApp is two connected sessions, web-1 with a terminal and db-1,
// web-1 selected on the Processes tab, on fake processes.
func panelApp(t *testing.T) (*App, *fakePlatform, *ui.Tester, *Session, *Session) {
	t.Helper()
	a, p := testApp(t)
	web := coloredSession(t, a, "web-1", 3)
	db := demoSession(a, "db-1", "10.0.0.12")
	a.active = 0
	a.processes.api = demoProcesses()
	a.bottomTab = tabProcesses
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	tt.Frame()
	return a, p, tt, web, db
}

func TestPanelRow(t *testing.T) {
	a, p, tt, web, db := panelApp(t)
	termColor := tt.Image().RGBAAt(termSpotX, termSpotY)
	if _, ok := tt.Find(locales.BackToTerminal()); ok {
		t.Fatal("Back to terminal on a session's row")
	}
	// inList checks that the session list shows the panel row's name: the
	// first text name is the list's, left of the bottom panel's tab.
	inList := func(name string) {
		t.Helper()
		if r, ok := tt.Find(name); !ok || r.X > a.sideWidth || r.X < panelIndent {
			t.Errorf("%s at %+v, not in the list", name, r)
		}
	}
	if r, _ := tt.Find(locales.TabProcesses()); r.X < a.sideWidth {
		t.Fatalf("Processes in the list at %+v before the panel row", r)
	}

	// The button opens the panel row right below its session, selected,
	// on the tab the session showed.
	if err := tt.Click(locales.OpenPanelTab()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got, want := rowNames(a), []string{"web-1", "web-1 | Processes", "db-1"}; !slices.Equal(got, want) {
		t.Fatalf("rows %q, want %q", got, want)
	}
	panel := a.sessions[1]
	if a.active != 1 || panel.panelOf != web || panel.panelTab != tabProcesses || panel.Host != nil {
		t.Fatalf("active %d, panel of %v on tab %d", a.active, panel.panelOf, panel.panelTab)
	}
	if a.connSession() != web || a.current() != panel {
		t.Fatal("the panel row does not read its session")
	}
	p.until(t, "the processes", func() bool { return web.procs.loaded })
	tt.Frame()

	// The bottom panel alone, at the top of the main area: no terminal.
	if r, ok := tt.Find(locales.BackToTerminal()); !ok || r.Y > 40 {
		t.Errorf("the panel's tab row at %+v, %v", r, ok)
	}
	if tt.Image().RGBAAt(termSpotX, termSpotY) == termColor {
		t.Error("the terminal shows in the panel row")
	}
	for _, want := range []string{"java", "7 processes", "IP web-1.example"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q; texts %q", want, tt.Texts())
		}
	}
	if _, ok := tt.Find(locales.OpenPanelTab()); ok {
		t.Error("Open in its own tab on the panel row")
	}
	inList("Processes")
	// One session each, two sessions open.
	if !tt.HasText("Sessions (2)") || a.sessionCount() != 2 || a.liveSessions() != 2 {
		t.Errorf("count %d, live %d; texts %q", a.sessionCount(), a.liveSessions(), tt.Texts())
	}
	saveShot(t, tt, "panel-row-processes.png")

	// Its icon and name follow its own tab; the session's row keeps its.
	if err := tt.Click(locales.TabNetwork()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if panel.panelTab != tabNetwork || a.bottomTab != tabProcesses || listLabel(panel) != "web-1 | Network" {
		t.Errorf("panel on %d, session on %d, label %q", panel.panelTab, a.bottomTab, listLabel(panel))
	}
	inList("Network")
	// The side panel's process list shows the panel row's Processes tab.
	if err := tt.Click("mysqld"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if panel.panelTab != tabProcesses || a.current() != panel {
		t.Errorf("after the side panel's click: tab %d, active %d", panel.panelTab, a.active)
	}

	// Terminal actions do nothing on it.
	fs := a.shortcut(scFullScreen)
	tt.Key(fs.Mods, fs.Key)
	tt.Frame()
	if a.fullScreen.on || a.terminalLive() {
		t.Error("full screen on the panel row")
	}
	a.openFind()
	a.toggleBarFocus()
	if web.findOpen() || web.bar.focus || panel.findOpen() {
		t.Error("find or command bar on the panel row")
	}
	if a.selectedNewTab() != nil {
		t.Error("the panel row taken for a new tab")
	}

	// Its context menu: back to the terminal, or close.
	if r, ok := tt.Find(locales.TabProcesses()); ok {
		tt.RightClickAt(r.X+r.W/2, r.Y+r.H/2)
	}
	if got, want := tt.Menu(), []string{locales.BackToTerminal(), "-", locales.Close()}; !slices.Equal(got, want) {
		t.Errorf("menu %q, want %q", got, want)
	}
	if err := tt.ChooseMenuItem(locales.BackToTerminal()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.current() != web || tt.Image().RGBAAt(termSpotX, termSpotY) != termColor {
		t.Errorf("Back to terminal selected %d", a.active)
	}

	// The button again selects the same panel row, on its own tab.
	a.bottomTab = tabFiles
	tt.Frame()
	if err := tt.Click(locales.OpenPanelTab()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(a.sessions) != 3 || a.current() != panel || panel.panelTab != tabProcesses {
		t.Fatalf("rows %q, active %d, tab %d", rowNames(a), a.active, panel.panelTab)
	}
	// The corner button goes back too.
	if err := tt.Click(locales.BackToTerminal()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.current() != web || len(a.sessions) != 3 {
		t.Errorf("Back to terminal: active %d, rows %q", a.active, rowNames(a))
	}

	// Closing the session closes its panel row, and only that session.
	a.active = 1
	a.closeSession(web)
	tt.Frame()
	if got := rowNames(a); !slices.Equal(got, []string{"db-1"}) || a.current() != db {
		t.Errorf("after closing web-1: rows %q, active %d", got, a.active)
	}
	if db.status != statusConnected {
		t.Error("db-1 disconnected")
	}

	// Closing a panel row ends nothing.
	a.openPanelTab(db, tabFiles)
	tt.Frame()
	if err := tt.Click(locales.Close()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := rowNames(a); !slices.Equal(got, []string{"db-1"}) || a.current() != db || db.status != statusConnected {
		t.Errorf("after closing the panel row: rows %q, active %d, status %v", got, a.active, db.status)
	}
}

// A session takes its panel row along as it is dragged; a panel row
// does not move alone, and a drop between them goes below the panel
// row.
func TestPanelRowMoves(t *testing.T) {
	a, _ := testApp(t)
	x := demoSession(a, "x", "1")
	y := demoSession(a, "y", "2")
	z := demoSession(a, "z", "3")
	a.active = 0
	a.openPanelTab(x, tabNetwork)
	px := a.current()
	check := func(what string, want ...string) {
		t.Helper()
		if got := rowNames(a); !slices.Equal(got, want) {
			t.Errorf("%s: rows %q, want %q", what, got, want)
		}
		if a.current() != px {
			t.Errorf("%s: selected %d", what, a.active)
		}
	}
	check("opened", "x", "x | Network", "y", "z")
	a.moveSessions([]int{0}, 4)
	check("x to the end", "y", "z", "x", "x | Network")
	a.moveSessions([]int{3}, 0)
	check("the panel row alone", "y", "z", "x", "x | Network")
	a.moveSessions([]int{0}, 3) // between x and its panel row
	check("y between x and its panel row", "z", "x", "x | Network", "y")
	a.moveSessions([]int{1}, 0)
	check("x to the top", "x", "x | Network", "z", "y")
	a.moveSessions([]int{0}, 1) // onto its own panel row
	check("x before its panel row", "x", "x | Network", "z", "y")
	a.moveSessions([]int{2, 3}, 0)
	check("two to the top", "z", "y", "x", "x | Network")
	_, _ = y, z
}

// Dragging a session's row in the list moves its panel row with it.
func TestPanelRowDrag(t *testing.T) {
	a, p := testApp(t)
	x := demoSession(a, "alpha", "1")
	demoSession(a, "bravo", "2")
	a.active = 0
	a.openPanelTab(x, tabFiles)
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	tt.Frame()
	fx, fy := rowCenter(t, tt, "alpha")
	_, by := rowCenter(t, tt, "bravo")
	dragTo(tt, fx, fy, fx, by+14)
	if got, want := rowNames(a), []string{"bravo", "alpha", "alpha | Files"}; !slices.Equal(got, want) {
		t.Errorf("rows %q, want %q", got, want)
	}
	// The panel row alone does not move.
	_, py := rowCenter(t, tt, "Files")
	_, ty := rowCenter(t, tt, "bravo")
	dragTo(tt, fx, py, fx, ty-14)
	if got, want := rowNames(a), []string{"bravo", "alpha", "alpha | Files"}; !slices.Equal(got, want) {
		t.Errorf("after dragging the panel row: rows %q, want %q", got, want)
	}
}

// A panel row is no session: quitting asks about the sessions only, and
// ends each once.
func TestPanelRowQuit(t *testing.T) {
	a, p := testApp(t)
	x := demoSession(a, "x", "1")
	a.openPanelTab(x, tabFiles)
	if a.liveSessions() != 1 || a.sessionCount() != 1 {
		t.Fatalf("live %d, count %d", a.liveSessions(), a.sessionCount())
	}
	p.answer = false
	if a.QuitRequested() {
		t.Fatal("quit without asking")
	}
	p.until(t, "the question", func() bool { return !a.askingQuit })
	q := p.questions()
	if len(q) != 1 || !strings.Contains(q[0].Detail, locales.ConfirmQuitBody(1)) {
		t.Errorf("asked %+v", q)
	}
	a.sessions[0].status = statusClosed
	if !a.QuitRequested() {
		t.Error("asked with no session open")
	}
}

// recordingProcesses is fakeProcesses that keeps the connections it was
// asked over.
type recordingProcesses struct {
	*fakeProcesses
	mu      sync.Mutex
	clients []*ssh.Client
}

func (r *recordingProcesses) list(ctx context.Context, client *ssh.Client, l *monitor.ProcessLister) ([]monitor.Process, error) {
	r.mu.Lock()
	r.clients = append(r.clients, client)
	r.mu.Unlock()
	return r.fakeProcesses.list(ctx, client, l)
}

func (r *recordingProcesses) asked() []*ssh.Client {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.clients)
}

// The panel row's files and processes are its session's, over its
// connection: browsing in one shows in the other.
func TestPanelRowSharesConnection(t *testing.T) {
	home := t.TempDir()
	for _, d := range []string{"etc", "srv/www"} {
		os.MkdirAll(filepath.Join(home, d), 0o755)
	}
	os.WriteFile(filepath.Join(home, "srv/www/index.html"), []byte("hi"), 0o644)
	srv := sshtest.StartSFTP(t, home)
	a, p := testApp(t)
	procs := &recordingProcesses{fakeProcesses: demoProcesses()}
	a.processes.api = procs
	h := &config.Host{ID: "web", Name: "web-1", Addr: srv.Host, Port: srv.Port, User: "tester", Auth: config.AuthPassword}
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

	// Files: the panel row browses the session's folder, and the
	// session's row then shows where it went.
	if err := tt.Click(locales.OpenPanelTab()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	panel := a.current()
	if !panel.isPanelTab() || panel.panelTab != tabFiles || !tt.HasText("srv") {
		t.Fatalf("panel row on %d; texts %q", panel.panelTab, tt.Texts())
	}
	a.openDir(s, path.Join(home, "srv/www"))
	p.until(t, "srv/www", func() bool { return s.files.cwd == path.Join(home, "srv/www") && !s.files.loading })
	tt.Frame()
	if !tt.HasText("index.html") {
		t.Errorf("the panel row does not list srv/www; texts %q", tt.Texts())
	}
	if err := tt.Click(locales.ParentFolder()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "srv", func() bool { return s.files.cwd == path.Join(home, "srv") && !s.files.loading })
	if err := tt.Click(locales.BackToTerminal()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.current() != s || !tt.HasText("www") || tt.HasText("index.html") {
		t.Errorf("the session's row does not show srv; texts %q", tt.Texts())
	}

	// Processes: read over the session's connection, while the panel row
	// shows them.
	a.openPanelTab(s, tabFiles)
	tt.Frame()
	if err := tt.Click(locales.TabProcesses()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	p.until(t, "the processes", func() bool { return s.procs.loaded })
	tt.Frame()
	asked := procs.asked()
	if len(asked) == 0 || slices.ContainsFunc(asked, func(c *ssh.Client) bool { return c != s.client }) {
		t.Errorf("read over %v, want the session's %p", asked, s.client)
	}
	if !tt.HasText("java") || s.procs.poll.run == nil {
		t.Errorf("polling %v; texts %q", s.procs.poll.run != nil, tt.Texts())
	}
	if panel.procs.poll.run != nil || panel.sftp != nil || panel.client != nil {
		t.Error("the panel row has a connection of its own")
	}

	// Reconnecting the session keeps the panel row, which then reads the
	// new connection.
	old := s.client
	a.reconnect(s)
	p.until(t, "the new connection", func() bool {
		if s.hostKeyQ != nil {
			s.hostKeyQ.Answer <- true
			s.hostKeyQ = nil
		}
		return s.status == statusConnected && s.client != nil && s.client != old
	})
	if a.current() != panel || a.panelRowOf(s) != panel {
		t.Fatal("the panel row went with the reconnect")
	}
	p.until(t, "a read on the new connection", func() bool {
		asked := procs.asked()
		return asked[len(asked)-1] == s.client
	})
}

// A session with its panel row open on the Network and the Processes
// tabs, in both languages: the frames SHOT_DIR keeps.
func TestPanelRowScreenshots(t *testing.T) {
	a, p, s, _, _, tt := networkApp(t)
	demoSession(a, "db-1", "10.0.0.12")
	a.processes.api = demoProcesses()
	if err := tt.Click(locales.GeoDownload()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the data", func() bool { return a.locationsShown() })
	a.geoNotNow(true)
	a.openNetTab(s, netGroup(t, s, "0.0.0.0:80"))
	if err := tt.Click(locales.OpenPanelTab()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got, want := rowNames(a), []string{"web-1", "web-1 | Network", "db-1"}; !slices.Equal(got, want) {
		t.Fatalf("rows %q, want %q", got, want)
	}
	for _, lang := range []string{"en", "zh-Hans"} {
		a.setLanguage(lang)
		a.current().panelTab = tabNetwork
		tt.Frame()
		if lang == "zh-Hans" && (!tt.HasText("网络") || listLabel(a.current()) != "web-1 | 网络") {
			t.Errorf("Chinese: %q; texts %q", listLabel(a.current()), tt.Texts())
		}
		saveShot(t, tt, "panel-row-network-"+lang+".png")
		a.current().panelTab = tabProcesses
		tt.Frame()
		p.until(t, "the processes", func() bool { return s.procs.loaded })
		s.procs.pid = 7267
		tt.Frame()
		saveShot(t, tt, "panel-row-processes-"+lang+".png")
	}
	// The session's row, with the button that opens the panel row.
	a.backToTerminal(a.current())
	tt.Frame()
	if _, ok := tt.Find(locales.OpenPanelTab()); !ok {
		t.Error("no button on the session's row")
	}
	saveShot(t, tt, "panel-row-session-zh-Hans.png")
	a.setLanguage("en")
	tt.Frame()
	saveShot(t, tt, "panel-row-session-en.png")
}

// A panel row is not renamed: it is named after its session, whose new
// name its text form then reads.
func TestPanelRowRename(t *testing.T) {
	a, tt, _ := renameApp(t)
	alpha := a.sessions[0]
	a.openPanelTab(alpha, tabNetwork)
	tt.Frame()
	x, y := rowCenter(t, tt, locales.TabNetwork()) // the list's row comes first
	if x > a.sideWidth {
		t.Fatalf("Network at %v, not in the list", x)
	}
	tt.RightClickAt(x, y)
	if menu := tt.Menu(); slices.Contains(menu, locales.Rename()) || !slices.Contains(menu, locales.BackToTerminal()) {
		t.Errorf("panel row's menu %q", menu)
	}
	tt.CloseMenu()
	tt.ClickAt(x, y) // the list takes the focus
	tt.Key(0, ui.KeyF2)
	tt.Frame()
	if a.current() != a.sessions[1] || a.rename.s != nil || canRename(a.current()) {
		t.Errorf("F2 on the panel row: active %d, renaming %v", a.active, a.rename.s != nil)
	}

	renameFromMenu(t, tt, alpha)
	tt.Type("build\n")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if alpha.label() != "build" || listLabel(a.sessions[1]) != "build | Network" {
		t.Errorf("label %q, panel row %q", alpha.label(), listLabel(a.sessions[1]))
	}
}
