package app

import (
	"image/png"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"finalshelly/internal/config"
	"finalshelly/internal/monitor"
	"finalshelly/internal/remotefs"
	"finalshelly/locales"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"github.com/pkg/sftp"
)

// fakePlatform runs posted functions when the test drains them, as the
// UI thread would, and stands in for the system's dialogs.
type fakePlatform struct {
	posts     chan func()
	tt        *ui.Tester
	locale    string
	downloads string
	opened    []string // what OpenFiles returns
	shown     []string
	fakeWindows
	fakeSystem
	fakeFilePicker
}

func newFakePlatform(t *testing.T) *fakePlatform {
	p := &fakePlatform{posts: make(chan func(), 1024), downloads: t.TempDir()}
	i18n.SetLocaleFunc(func() string { return p.locale })
	t.Cleanup(func() { i18n.SetLocaleFunc(nil) })
	return p
}

func (p *fakePlatform) Post(fn func())                           { p.posts <- fn }
func (p *fakePlatform) OpenFiles(string, bool) ([]string, error) { return p.opened, nil }
func (p *fakePlatform) ChooseFolder(string) (string, error)      { return p.downloads, nil }
func (p *fakePlatform) DownloadsDir() string                     { return p.downloads }
func (p *fakePlatform) ShowInFolder(path string)                 { p.shown = append(p.shown, path) }
func (p *fakePlatform) Languages() []string                      { return []string{"en", "zh-Hans"} }
func (p *fakePlatform) SetLocale(tag string) {
	p.locale = tag
	if p.tt != nil {
		p.tt.SetLocale(tag)
	}
	p.fakeWindows.setLocale(tag)
}

// until runs posted functions and draws frames until cond holds.
func (p *fakePlatform) until(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.After(20 * time.Second)
	for !cond() {
		select {
		case fn := <-p.posts:
			fn()
		case <-time.After(20 * time.Millisecond):
		case <-deadline:
			t.Fatalf("timed out waiting for %s", what)
		}
		if p.tt != nil {
			p.tt.Frame()
		}
	}
}

func testApp(t *testing.T) (*App, *fakePlatform) {
	t.Setenv("FINALSHELLY_DIR", t.TempDir())
	p := newFakePlatform(t)
	a := New(p)
	a.saveHosts = func(*config.Store) error { return nil }
	return a, p
}

// demoSession is a connected session with made-up stats and files.
func demoSession(a *App, name, addr string) *Session {
	h := &config.Host{ID: config.NewHostID(), Name: name, Addr: addr, User: "root", Auth: config.AuthPassword}
	a.hosts.Add(h)
	s := newSession(h)
	s.status, s.ip = statusConnected, addr
	hist := func(base float64) []float64 {
		var h []float64
		for i := range monitor.HistoryLen {
			h = append(h, base*(1+0.6*float64((i*7)%11)/10))
		}
		return h
	}
	s.stats = monitor.Stats{
		OK: true, Uptime: 410 * 24 * time.Hour, Load: [3]float64{0.28, 0.31, 0.27}, CPU: 7,
		MemUsed: 2_900_000_000, MemTotal: 4_080_000_000, SwapUsed: 34_000_000, SwapTotal: 2_360_000_000,
		Procs: []monitor.Proc{{RSS: 1_930_000_000, CPU: 3.3, Name: "java"}, {RSS: 118_000_000, CPU: 1.7, Name: "mysqld"}, {RSS: 467_000_000, CPU: 0.3, Name: "syncthing"}},
		Iface: "eth0", RxRate: 126_000, TxRate: 66_000, RxHist: hist(90_000), TxHist: hist(40_000),
		Latency: 254 * time.Millisecond, LatHist: hist(250),
		Disks: []monitor.Disk{{Mount: "/", Avail: 4_700_000_000, Size: 25_000_000_000}},
	}
	f := &s.files
	f.cwd, f.pathEdit = "/lib64", "/lib64"
	mod := time.Date(2018, 6, 19, 21, 27, 0, 0, time.Local)
	f.entries = []remotefs.Entry{
		{Name: "security", Mode: fs.ModeDir | 0o755, ModTime: mod},
		{Name: "ld-2.12.so", Size: 159_334, Mode: 0o755, ModTime: mod},
		{Name: "ld-linux-x86-64.so.2", Size: 159_334, Mode: 0o777, ModTime: mod, Link: true},
		{Name: "libacl.so.1", Size: 31_280, Mode: 0o755, ModTime: mod},
	}
	f.subdirs["/"] = []string{"/etc", "/home", "/lib64", "/usr"}
	f.subdirs["/lib64"] = []string{"/lib64/security"}
	f.tree.Open.Add("/lib64")
	a.sessions = append(a.sessions, s)
	return s
}

func saveShot(t *testing.T, tt *ui.Tester, name string) {
	dir := os.Getenv("SHOT_DIR")
	if dir == "" {
		return
	}
	out, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	if err := png.Encode(out, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

// idleConn never sends anything, for a terminal the test feeds.
type idleConn struct{ done chan struct{} }

func (c idleConn) Read([]byte) (int, error)    { <-c.done; return 0, io.EOF }
func (c idleConn) Write(p []byte) (int, error) { return len(p), nil }
func (c idleConn) Close() error                { return nil }

func TestLayoutInBothLanguages(t *testing.T) {
	a, p := testApp(t)
	s := demoSession(a, "web-1", "45.56.78.90")
	demoSession(a, "db-1", "10.0.0.12").status = statusConnecting
	failed := demoSession(a, "backup", "10.0.0.30")
	failed.status, failed.err = statusFailed, io.ErrUnexpectedEOF
	a.active = 0

	done := make(chan struct{})
	defer close(done)
	if term, err := terminal.New(terminal.Options{Conn: idleConn{done}, Font: a.termFont, Theme: a.termTheme, DarkTheme: a.termTheme}); err == nil {
		defer term.Close()
		s.term = term
		term.Feed([]byte("[root@web-1 ~]# ls\r\n\x1b[1;34mlogs\x1b[0m  \x1b[1;32mdeploy.sh\x1b[0m  中文文件名.txt\r\n[root@web-1 ~]# "))
	}

	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	tt.Frame()
	for _, want := range []string{"Sessions (3)", "web-1", "db-1", "IP 45.56.78.90", "Up 410 days", "java", "libacl.so.1", "Name", "Files", "Transfers"} {
		if !tt.HasText(want) {
			t.Errorf("English: missing %q", want)
		}
	}
	saveShot(t, tt, "en.png")

	a.setLanguage("zh-Hans")
	tt.Frame()
	if a.settings.Language != "zh-Hans" || config.LoadSettings().Language != "zh-Hans" {
		t.Errorf("language not kept: %+v", config.LoadSettings())
	}
	for _, want := range []string{"会话 (3)", "运行 410 天", "命令", "文件名", "文件", "传输", "修改时间"} {
		if !tt.HasText(want) {
			t.Errorf("Chinese: missing %q; texts %q", want, tt.Texts())
		}
	}
	saveShot(t, tt, "zh.png")

	a.active = 2
	tt.Frame()
	if !tt.HasText("重新连接") || !tt.HasText(describeError(io.ErrUnexpectedEOF)) {
		t.Errorf("no reconnect banner; texts %q", tt.Texts())
	}
}

func TestConnectionManager(t *testing.T) {
	a, p := testApp(t)
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	tt.Key(ui.Ctrl|ui.Shift, ui.KeyT) // with no hosts, on a new one
	if !a.managerOpen || !a.windowOpen(WindowEditor) {
		t.Fatalf("manager %v editor %v", a.managerOpen, a.windowOpen(WindowEditor))
	}
	editor := p.show(t, WindowEditor)
	editor.Type("example.com")
	if err := editor.Click(locales.Ok()); err != nil {
		t.Fatal(err)
	}
	if hosts := a.hosts.Live(); len(hosts) != 1 || hosts[0].Addr != "example.com" || hosts[0].PortOr22() != 22 || !hosts[0].MonitorEnabled() {
		t.Fatalf("hosts %+v", a.hosts.Hosts)
	}
	tt.Frame()
	if !tt.HasText(locales.ManagerTitle()) || !tt.HasText("root@example.com") {
		t.Errorf("list not shown; texts %q", tt.Texts())
	}
	saveShot(t, tt, "manager.png")
}

func TestMoveSessions(t *testing.T) {
	a, _ := testApp(t)
	x := demoSession(a, "x", "1")
	y := demoSession(a, "y", "2")
	z := demoSession(a, "z", "3")
	a.active = 0
	a.moveSessions([]int{0}, 3)
	if a.sessions[0] != y || a.sessions[1] != z || a.sessions[2] != x || a.current() != x {
		t.Fatalf("order %v %v %v, active %d", a.sessions[0].label(), a.sessions[1].label(), a.sessions[2].label(), a.active)
	}
}

// localSFTP serves the local file system over SFTP in the process.
func localSFTP(t *testing.T) *sftp.Client {
	t.Helper()
	cr, sw := io.Pipe()
	sr, cw := io.Pipe()
	server, err := sftp.NewServer(struct {
		io.Reader
		io.WriteCloser
	}{sr, sw})
	if err != nil {
		t.Fatal(err)
	}
	go server.Serve()
	c, err := sftp.NewClientPipe(cr, cw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cw.Close(); sw.Close(); c.Close() })
	return c
}

func TestDropUploadAndDownload(t *testing.T) {
	a, p := testApp(t)
	s := demoSession(a, "files", "127.0.0.1")
	a.active = 0
	remote, local := t.TempDir(), t.TempDir()
	s.sftp = localSFTP(t)
	s.files = fileState{}
	s.files.init()

	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	a.openDir(s, remote)
	p.until(t, "the listing", func() bool { return s.files.cwd == remote && !s.files.loading })

	// Drop a file and a folder from the desktop onto the file table.
	report := filepath.Join(local, "report.txt")
	os.WriteFile(report, []byte("quarterly numbers"), 0o644)
	os.MkdirAll(filepath.Join(local, "photos", "2026"), 0o755)
	os.WriteFile(filepath.Join(local, "photos", "2026", "a.jpg"), make([]byte, 300_000), 0o644)
	box, ok := tt.Find(locales.ColName())
	if !ok {
		t.Fatal("no table header")
	}
	if !tt.DropFiles(box.X+40, box.Y+120, report, filepath.Join(local, "photos")) {
		t.Fatal("the table took no files")
	}
	p.until(t, "the uploads", func() bool { return len(a.transfers.jobs) == 2 && a.transfers.active() == 0 })
	for _, j := range a.transfers.jobs {
		if j.state != transferDone {
			t.Fatalf("%s: state %d err %v", j.name(), j.state, j.err)
		}
	}
	if data, _ := os.ReadFile(filepath.Join(remote, "report.txt")); string(data) != "quarterly numbers" {
		t.Errorf("uploaded %q", data)
	}
	if fi, err := os.Stat(filepath.Join(remote, "photos", "2026", "a.jpg")); err != nil || fi.Size() != 300_000 {
		t.Errorf("folder upload: %v", err)
	}
	p.until(t, "the refreshed listing", func() bool { return len(s.files.entries) == 2 })

	// Dropping it again asks before replacing.
	tt.DropFiles(box.X+40, box.Y+120, report)
	p.until(t, "the question", func() bool { return a.replace != nil })
	tt.Frame()
	if !tt.HasText(locales.ReplaceTitle()) {
		t.Errorf("no question shown; texts %q", tt.Texts())
	}
	if err := tt.Click(locales.Replace()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the replacing upload", func() bool { return len(a.transfers.jobs) == 3 && a.transfers.active() == 0 })

	// Download both into the downloads folder.
	a.download(s, []string{"photos", "report.txt"}, "")
	p.until(t, "the downloads", func() bool { return len(a.transfers.jobs) == 5 && a.transfers.active() == 0 })
	if data, _ := os.ReadFile(filepath.Join(p.downloads, "report.txt")); string(data) != "quarterly numbers" {
		t.Errorf("downloaded %q", data)
	}
	if _, err := os.Stat(filepath.Join(p.downloads, "photos", "2026", "a.jpg")); err != nil {
		t.Errorf("folder download: %v", err)
	}

	a.bottomTab = 1
	tt.Frame()
	if !tt.HasText(locales.TransferDone()) || !tt.HasText("report.txt") {
		t.Errorf("transfers not listed; texts %q", tt.Texts())
	}
	saveShot(t, tt, "transfers.png")
	if err := tt.Click(locales.ClearFinished()); err != nil {
		t.Fatal(err)
	}
	if len(a.transfers.jobs) != 0 {
		t.Errorf("%d jobs left", len(a.transfers.jobs))
	}
}

func TestUploadFromDialog(t *testing.T) {
	a, p := testApp(t)
	s := demoSession(a, "files", "127.0.0.1")
	a.active = 0
	remote, local := t.TempDir(), t.TempDir()
	s.sftp = localSFTP(t)
	s.files.cwd = remote
	f := filepath.Join(local, "notes.md")
	os.WriteFile(f, []byte("# notes"), 0o644)
	p.opened = []string{f}
	a.pickAndUpload(s, false)
	p.until(t, "the upload", func() bool { return len(a.transfers.jobs) == 1 && a.transfers.active() == 0 })
	if data, _ := os.ReadFile(filepath.Join(remote, "notes.md")); string(data) != "# notes" {
		t.Errorf("uploaded %q", data)
	}
}

func TestErrorsInBothLanguages(t *testing.T) {
	_, p := testApp(t)
	err := &stepError{"shell", io.EOF}
	en := describeError(err)
	p.SetLocale("zh-Hans")
	zh := describeError(err)
	if !strings.Contains(en, "shell") || !strings.Contains(zh, "终端") {
		t.Errorf("en %q zh %q", en, zh)
	}
}
