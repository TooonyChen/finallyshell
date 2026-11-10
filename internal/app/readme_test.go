package app

import (
	"io"
	"testing"

	"finallyshell/internal/config"
	"finallyshell/locales"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// readmeTerminal is what the terminal of the README's main window shows.
const readmeTerminal = "\x1b[1;32mroot@web-1\x1b[0m:\x1b[1;34m/srv/app\x1b[0m# git log --oneline -4\r\n" +
	"\x1b[33me3f9a1c\x1b[0m (\x1b[1;36mHEAD -> \x1b[1;32mmain\x1b[0m, \x1b[1;31morigin/main\x1b[0m) Add a health check endpoint\r\n" +
	"\x1b[33m7b2d0f4\x1b[0m Cache static files for a day\r\n" +
	"\x1b[33m41c8e9a\x1b[0m (\x1b[1;33mtag: v2.4.1\x1b[0m) Fix the login redirect\r\n" +
	"\x1b[33m9d03b6e\x1b[0m Move the uploads to /srv/media\r\n" +
	"\x1b[1;32mroot@web-1\x1b[0m:\x1b[1;34m/srv/app\x1b[0m# docker compose ps\r\n" +
	"NAME          IMAGE         STATUS                  PORTS\r\n" +
	"app-web-1     app:2.4.1     Up 6 days               0.0.0.0:443->443/tcp\r\n" +
	"app-db-1      postgres:16   Up 6 days (\x1b[32mhealthy\x1b[0m)    5432/tcp\r\n" +
	"app-cache-1   redis:7       Up 6 days               6379/tcp\r\n" +
	"\x1b[1;32mroot@web-1\x1b[0m:\x1b[1;34m/srv/app\x1b[0m# systemctl status nginx --no-pager | head -3\r\n" +
	"\x1b[1;32m●\x1b[0m nginx.service - A high performance web server and a reverse proxy server\r\n" +
	"     Loaded: loaded (/lib/systemd/system/nginx.service; \x1b[1;32menabled\x1b[0m; preset: \x1b[1;32menabled\x1b[0m)\r\n" +
	"     Active: \x1b[1;32mactive (running)\x1b[0m since Mon 2026-11-02 09:12:44 UTC; 6 days ago\r\n" +
	"\x1b[1;32mroot@web-1\x1b[0m:\x1b[1;34m/srv/app\x1b[0m# ls\r\n" +
	"\x1b[1;34mdocs\x1b[0m  \x1b[1;34mnode_modules\x1b[0m  \x1b[1;34msrc\x1b[0m  docker-compose.yml  Dockerfile  go.mod  main.go  Makefile  README.md  \x1b[1;32mdeploy.sh\x1b[0m  部署说明.txt\r\n" +
	"\x1b[1;32mroot@web-1\x1b[0m:\x1b[1;34m/srv/app\x1b[0m# "

// readmeApp is the main window the README shows: four sessions, the first
// selected, with its terminal, its server's stats and /srv/app in the file
// panel, drawn at twice the pixels in lang.
func readmeApp(t *testing.T, lang string) (*App, *fakePlatform, *ui.Tester, *Session) {
	t.Helper()
	a, p := testApp(t)
	p.SetLocale(lang)
	s := iconsSession(a)
	s.Host.Name, s.Host.Addr, s.ip = "web-1", "45.56.78.90", "45.56.78.90"
	demoSession(a, "db-1", "10.0.0.12").Host.User = "postgres"
	demoSession(a, "staging", "staging.example.com").status = statusConnecting
	failed := demoSession(a, "backup", "10.0.0.30")
	failed.status, failed.err = statusFailed, io.ErrUnexpectedEOF
	a.active = 0

	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	opts := a.terminalOptions(s.Host)
	opts.Conn = idleConn{done}
	term, err := terminal.New(opts)
	if err != nil {
		t.Skip("no terminal:", err)
	}
	t.Cleanup(func() { term.Close() })
	s.setTerm(term)
	term.Feed([]byte(readmeTerminal))

	tt := ui.NewTester(a.View, 1280, 820)
	tt.SetScale(2)
	tt.SetLocale(lang)
	p.tt = tt
	tt.Frame()
	waitIconTheme(t, a, p, tt)
	return a, p, tt, s
}

// TestReadmeScreenshots draws the scenes of the main window the README
// shows, in English and Chinese. go run ./tools/screenshots collects them,
// with frames other tests save, into docs/screenshots.
func TestReadmeScreenshots(t *testing.T) {
	for _, lang := range []string{"en", "zh-Hans"} {
		t.Run(lang, func(t *testing.T) {
			t.Run("main", func(t *testing.T) {
				_, _, tt, _ := readmeApp(t, lang)
				for _, want := range []string{"web-1", "docker-compose.yml", "java", "staging"} {
					if !tt.HasText(want) {
						t.Errorf("missing %q; texts %q", want, tt.Texts())
					}
				}
				saveShot(t, tt, "readme-main-"+lang+".png")
			})

			// The same scene in the dark appearance, which the README
			// does not show (yet).
			t.Run("main-dark", func(t *testing.T) {
				_, _, tt, _ := readmeApp(t, lang)
				tt.SetDark(true)
				tt.Frame()
				if !theme.Dark || !tt.HasText("docker-compose.yml") {
					t.Fatalf("dark %v; texts %q", theme.Dark, tt.Texts())
				}
				saveShot(t, tt, "readme-main-dark-"+lang+".png")
			})

			t.Run("sysinfo", func(t *testing.T) {
				a, p, tt, _ := readmeApp(t, lang)
				f := newFakeSysInfo(a)
				clickButton(t, tt, locales.SystemInfo())
				f.next(t).reply <- sysInfoReply{info: debianSample(t)}
				p.until(t, "the system info", func() bool { return !a.sysInfo.loading })
				if !tt.HasText("Debian GNU/Linux 12 (bookworm)") {
					t.Errorf("no system info; texts %q", tt.Texts())
				}
				saveShot(t, tt, "readme-sysinfo-"+lang+".png")
			})

			t.Run("transfers", func(t *testing.T) {
				a, p, tt, s := readmeApp(t, lang)
				p.downloads = "/home/me/Downloads"
				demoTransfers(a, s)
				tt.Frame()
				if err := tt.Click(locales.DownloadsButton()); err != nil {
					t.Fatal(err)
				}
				if !tt.HasText("app-2.4.1.tar.gz") {
					t.Errorf("no transfers; texts %q", tt.Texts())
				}
				saveShot(t, tt, "readme-transfers-"+lang+".png")
			})

			t.Run("quick-commands", func(t *testing.T) {
				a, _, tt, _ := readmeApp(t, lang)
				demoQuick(a, lang != "en")
				a.bottomTab = tabCommands
				// The command editor beside the buttons, two lines
				// selected (cmdeditor_test.go).
				a.quickUI.box.shown = true
				tt.Frame()
				setCmdText(a, tt, cmdDemo[lang])
				selectLines(a, tt, 2, 3)
				if !tt.HasText(locales.QuickNewCommand()) || !tt.HasText("df -h") || !tt.HasText(locales.CmdEditorSendSelection()) {
					t.Errorf("no quick commands; texts %q", tt.Texts())
				}
				saveShot(t, tt, "readme-quick-commands-"+lang+".png")
			})

			// The left panel on its Connections page, the open sessions'
			// hosts in folders among others.
			t.Run("connections", func(t *testing.T) {
				a, _, tt, _ := readmeApp(t, lang)
				s := a.hosts
				s.Folders = []*config.Folder{
					{ID: "prod", Name: "Production"},
					{ID: "db", Name: "Databases", Parent: "prod"},
					{ID: "staging", Name: "Staging"},
					{ID: "lab", Name: "Lab"},
				}
				for _, h := range s.Hosts {
					switch h.Name {
					case "web-1":
						h.Folder = "prod"
					case "db-1":
						h.Folder = "db"
					case "staging":
						h.Folder = "staging"
					}
				}
				for _, h := range []*config.Host{
					{ID: "web-2", Name: "web-2", Addr: "45.56.78.91", User: "root", Folder: "prod"},
					{ID: "db-2", Name: "db-2", Addr: "10.0.0.13", User: "postgres", Folder: "db"},
					{ID: "pi", Name: "raspberry", Addr: "192.168.1.20", User: "pi", Folder: "lab"},
				} {
					s.Add(h)
				}
				for _, f := range []string{"prod", "db", "staging"} {
					a.connTree.tree.Open.Add(managerItem{folder: f})
				}
				a.setLeftPage(leftConnections)
				tt.Frame()
				if !tt.HasText("Databases") || !tt.HasText("db-2") {
					t.Errorf("no tree; texts %q", tt.Texts())
				}
				saveShot(t, tt, "readme-connections-"+lang+".png")
			})

			t.Run("quick-connect", func(t *testing.T) {
				a, p := testApp(t)
				p.SetLocale(lang)
				startHosts(a)
				s := demoSession(a, "web-1", "203.0.113.10")
				db := demoSession(a, "db-1", "10.0.0.12")
				db.status = statusConnecting
				a.hosts.Remove(s.Host.ID) // the sessions of saved hosts in truth
				a.hosts.Remove(db.Host.ID)
				a.NewTab()
				a.sessions[1], a.sessions[2] = a.sessions[2], a.sessions[1] // the new tab second
				a.active = 1
				tt := ui.NewTester(a.View, 1280, 820)
				tt.SetScale(2)
				tt.SetLocale(lang)
				p.tt = tt
				t.Cleanup(a.CloseAll)
				tt.Frame()
				tt.Frame()
				if !tt.HasText(locales.QuickConnect()) || !tt.HasText("staging.example.com") {
					t.Errorf("no quick connect; texts %q", tt.Texts())
				}
				saveShot(t, tt, "readme-quick-connect-"+lang+".png")
			})
		})
	}
}
