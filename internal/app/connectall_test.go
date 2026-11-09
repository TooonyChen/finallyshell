package app

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"finallyshell/internal/config"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// unreachable makes the live hosts connect with a password known, to a
// port nothing listens on, to fail at once without a prompt.
func unreachable(a *App) {
	for _, h := range a.hosts.Live() {
		h.Addr, h.Port, h.User, h.Auth, h.NoMonitor = "127.0.0.1", 1, "u", config.AuthPassword, true
		a.passwords[h.ID] = "secret"
	}
}

// sessionHosts are the IDs of the hosts of the sessions, "" for a new
// tab.
func sessionHosts(a *App) []string {
	var out []string
	for _, s := range a.sessions {
		id := ""
		if s.Host != nil {
			id = s.Host.ID
		}
		out = append(out, id)
	}
	return out
}

// "Connect all" on a folder of the Connections page opens a session to
// every connection in it and its subfolders, in the order of the tree,
// the first in the new tab selected; never a deleted one, and an empty
// folder has it disabled.
func TestConnectAllFromPage(t *testing.T) {
	for _, lang := range []string{"en", "zh-Hans"} {
		t.Run(lang, func(t *testing.T) {
			a, p, tt := connPageApp(t, lang)
			t.Cleanup(a.CloseAll)
			empty := a.hosts.AddFolder("Empty", "")
			a.hosts.Add(&config.Host{ID: "gone", Name: "gone", Addr: "10.0.0.1", Folder: empty.ID, DeletedAt: time.Now()})
			unreachable(a)
			a.NewTab()
			tab := a.sessions[0]
			tt.Frame()

			tt.RightClickAt(connRow(t, tt, "Production"))
			tt.Frame()
			if menu := tt.Menu(); len(menu) < 2 || menu[0] != locales.ConnectAll() || menu[1] != "-" {
				t.Fatalf("menu %q", menu)
			}
			if err := tt.ChooseMenuItem(locales.ConnectAll()); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if got, want := sessionHosts(a), []string{"pg", "web-1", "web-2"}; !slices.Equal(got, want) || a.sessions[0] != tab {
				t.Fatalf("sessions %q, want %q", got, want)
			}
			if a.pwOpen || a.manager.question != nil || a.connTree.question != nil || a.left.page != leftConnections {
				t.Errorf("prompt %v, question %v %v, page %d", a.pwOpen, a.manager.question, a.connTree.question, a.left.page)
			}

			// The deleted host in Lab is left out.
			connMenu(t, tt, "Lab", locales.ConnectAll())
			if got, want := sessionHosts(a), []string{"pg", "web-1", "web-2", "pi"}; !slices.Equal(got, want) {
				t.Fatalf("sessions %q, want %q", got, want)
			}

			// A folder with only a deleted host has nothing to connect.
			tt.RightClickAt(connRow(t, tt, "Empty"))
			tt.Frame()
			if err := tt.ChooseMenuItem(locales.ConnectAll()); err == nil {
				t.Error("Connect all is enabled on an empty folder")
			}
			tt.CloseMenu()
			if len(a.sessions) != 4 {
				t.Errorf("%d sessions", len(a.sessions))
			}
			p.until(t, "the sessions to fail", func() bool {
				return !slices.ContainsFunc(a.sessions, func(s *Session) bool { return s.status == statusConnecting })
			})
			if a.pwOpen {
				t.Errorf("a prompt for %+v", a.pwNeed)
			}
		})
	}
}

// The folder's menu, with "Connect all" at its top, in the manager.
func TestConnectAllMenu(t *testing.T) {
	for _, lang := range []string{"en", "zh-Hans"} {
		t.Run(lang, func(t *testing.T) {
			a, p := testApp(t)
			p.SetLocale(lang)
			managerHosts(a)
			a.openManager()
			w := p.windows[WindowManager]
			tt := ui.NewTester(func(c *ui.Context) {
				c.SetMenuPresentation(ui.PopoverMenus)
				w.view(c)
			}, w.spec.Width, w.spec.Height)
			tt.SetScale(2)
			tt.SetLocale(lang)
			tt.Frame()
			tt.RightClickAt(rowCenter(t, tt, "Production"))
			tt.Frame()
			if !tt.HasText(locales.ConnectAll()) {
				t.Errorf("no %q; texts %q", locales.ConnectAll(), tt.Texts())
			}
			saveShot(t, tt, "connect-all-menu-"+lang+".png")
		})
	}
}

// More than ten connections are asked about first, in the manager; the
// manager then closes and they all open in the main window.
func TestConnectAllAsks(t *testing.T) {
	for _, lang := range []string{"en", "zh-Hans"} {
		t.Run(lang, func(t *testing.T) {
			a, p := testApp(t)
			p.SetLocale(lang)
			t.Cleanup(a.CloseAll)
			managerHosts(a)
			big := a.hosts.AddFolder("Fleet", "")
			sub := a.hosts.AddFolder("Edge", big.ID)
			var want []string
			for i := range 11 {
				folder := big.ID
				if i < 4 {
					folder = sub.ID
				}
				id := fmt.Sprintf("n%02d", i)
				a.hosts.Add(&config.Host{ID: id, Name: id, Folder: folder})
				want = append(want, id)
			}
			unreachable(a)
			mainWin := ui.NewTester(a.View, 1280, 820)
			p.tt = mainWin
			a.openManager()
			tt := p.show(t, WindowManager)
			tt.Frame()

			rowMenu(t, tt, "Fleet", locales.ConnectAll())
			q := a.manager.question
			if q == nil || !tt.HasText(locales.ConnectAllTitle(11)) || !tt.HasText(locales.ConnectAllBody("Fleet")) || len(a.sessions) != 0 {
				t.Fatalf("question %v, %d sessions; texts %q", q, len(a.sessions), tt.Texts())
			}
			saveShot(t, tt, "connect-all-ask-"+lang+".png")
			if err := tt.Click(locales.Cancel()); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if a.manager.question != nil || len(a.sessions) != 0 || !a.windowOpen(WindowManager) {
				t.Fatalf("cancelled: question %v, %d sessions", a.manager.question, len(a.sessions))
			}

			rowMenu(t, tt, "Fleet", locales.ConnectAll())
			if err := tt.Click(locales.Connect()); err != nil {
				t.Fatal(err)
			}
			if got := sessionHosts(a); !slices.Equal(got, want) {
				t.Fatalf("sessions %q, want %q", got, want)
			}
			if a.windowOpen(WindowManager) || !slices.Contains(p.focused, WindowMain) {
				t.Errorf("manager open %v, focused %v", a.windowOpen(WindowManager), p.focused)
			}
		})
	}
}

// A host without a password known asks for it, one prompt at a time;
// Cancel skips that host and the next ones still open, the first of them
// in the new tab.
func TestConnectAllPasswordCancel(t *testing.T) {
	a, p, tt := connPageApp(t, "en")
	t.Cleanup(a.CloseAll)
	f := a.hosts.AddFolder("Keys", "")
	for _, id := range []string{"p1", "p2", "p3", "p4"} {
		a.hosts.Add(&config.Host{ID: id, Name: id, Folder: f.ID})
	}
	unreachable(a)
	delete(a.passwords, "p1")
	delete(a.passwords, "p3")
	a.NewTab()
	tab := a.sessions[0]
	tt.Frame()

	connMenu(t, tt, "Keys", locales.ConnectAll())
	p.until(t, "the prompt for p1", func() bool { return a.pwOpen && a.pwNeed.host.ID == "p1" })
	if len(a.sessions) != 1 || !tab.isNewTab() {
		t.Fatalf("sessions %q before the answer", sessionHosts(a))
	}
	tt.Frame()
	saveShot(t, tt, "connect-all-prompt-en.png")
	if err := tt.Click(locales.Cancel()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the prompt for p3", func() bool { return a.pwOpen && a.pwNeed.host.ID == "p3" })
	if got := sessionHosts(a); !slices.Equal(got, []string{"p2"}) || a.sessions[0] != tab {
		t.Fatalf("after Cancel: sessions %q", got)
	}
	tt.Frame()
	tt.Type("secret")
	if err := tt.Click(locales.Connect()); err != nil {
		t.Fatal(err)
	}
	if got := sessionHosts(a); !slices.Equal(got, []string{"p2", "p3", "p4"}) {
		t.Fatalf("after the password: sessions %q", got)
	}
	if a.pwOpen || a.pwCancel != nil {
		t.Errorf("prompt %v, cancel %v", a.pwOpen, a.pwCancel != nil)
	}

	// A prompt of a single connection is cancelled as before: nothing
	// runs on.
	a.openSession(a.hosts.Host("p1"))
	p.until(t, "the prompt", func() bool { return a.pwOpen })
	tt.Frame()
	if err := tt.Click(locales.Cancel()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.pwOpen || len(a.sessions) != 3 {
		t.Errorf("prompt %v, sessions %q", a.pwOpen, sessionHosts(a))
	}
}
