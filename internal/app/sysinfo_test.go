package app

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"finallyshell/internal/monitor"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
	"golang.org/x/crypto/ssh"
)

// fakeSysInfo answers the app's reads when the test says, one at a time.
type fakeSysInfo struct{ reads chan sysInfoRead }

type sysInfoRead struct {
	ctx   context.Context
	reply chan sysInfoReply
}

type sysInfoReply struct {
	info monitor.SysInfo
	err  error
}

func newFakeSysInfo(a *App) *fakeSysInfo {
	f := &fakeSysInfo{reads: make(chan sysInfoRead, 8)}
	a.readSysInfo = func(ctx context.Context, _ *ssh.Client) (monitor.SysInfo, error) {
		r := sysInfoRead{ctx, make(chan sysInfoReply, 1)}
		f.reads <- r
		select {
		case rep := <-r.reply:
			return rep.info, rep.err
		case <-ctx.Done():
			return monitor.SysInfo{}, ctx.Err()
		}
	}
	return f
}

// next is the read the app started last.
func (f *fakeSysInfo) next(t *testing.T) sysInfoRead {
	t.Helper()
	select {
	case r := <-f.reads:
		return r
	default:
		t.Fatal("no read started")
	}
	return sysInfoRead{}
}

func debianSample(t *testing.T) monitor.SysInfo {
	data, err := os.ReadFile("../monitor/testdata/sysinfo-debian.txt")
	if err != nil {
		t.Fatal(err)
	}
	return monitor.ParseSysInfo(string(data))
}

// clickButton clicks the button named name, where text of the same name
// shows elsewhere too.
func clickButton(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	b, ok := tt.AccessByRole(ui.RoleButton, name)
	if !ok {
		t.Fatalf("no %q button; texts %q", name, tt.Texts())
	}
	tt.ClickAt(b.Bounds.X+b.Bounds.W/2, b.Bounds.Y+b.Bounds.H/2)
}

func TestSidePanelSkeleton(t *testing.T) {
	a, p := testApp(t)
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	tt.Frame()
	for _, want := range []string{"IP -", "Up -", "Load -", "0%", locales.SystemInfo(), locales.DiskPath()} {
		if !tt.HasText(want) {
			t.Errorf("English skeleton: missing %q; texts %q", want, tt.Texts())
		}
	}
	if b, _ := tt.AccessByRole(ui.RoleButton, locales.SystemInfo()); !b.Disabled {
		t.Error("System info enabled without a session")
	}
	saveShot(t, tt, "skeleton-en.png")
	a.setLanguage("zh-Hans")
	tt.Frame()
	for _, want := range []string{"运行 -", "负载 -", "系统信息", "可用/大小"} {
		if !tt.HasText(want) {
			t.Errorf("Chinese skeleton: missing %q", want)
		}
	}
	saveShot(t, tt, "skeleton-zh.png")

	// A session still connecting shows its state over the skeleton.
	demoSession(a, "db-1", "10.0.0.12").status = statusConnecting
	a.active = 0
	tt.Frame()
	if !tt.HasText("运行 -") || !tt.HasText(locales.StatusConnecting()) {
		t.Errorf("connecting: texts %q", tt.Texts())
	}
}

func TestSysInfoPopover(t *testing.T) {
	a, p := testApp(t)
	demoSession(a, "web-1", "45.56.78.90")
	a.active = 0
	f := newFakeSysInfo(a)
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	tt.Frame()

	clickButton(t, tt, locales.SystemInfo())
	if !a.sysInfo.open || !a.sysInfo.loading || !tt.HasText(locales.ReadingServerInfo()) {
		t.Fatalf("not loading: %+v; texts %q", a.sysInfo, tt.Texts())
	}
	saveShot(t, tt, "popover-loading-en.png")
	f.next(t).reply <- sysInfoReply{info: debianSample(t)}
	p.until(t, "the system info", func() bool { return !a.sysInfo.loading })
	for _, want := range []string{"Hostname", "Debian GNU/Linux 12 (bookworm)", "6.1.0-18-amd64", "x86_64",
		"Intel(R) Xeon(R) CPU E5-2680 v4 @ 2.40GHz", "2 cores, 4 threads", "3.8 GB", "410 days, 0:17",
		"ens3  45.56.78.90/24", "docker0  172.17.0.1/16", "vda  25.0 GB"} {
		if !tt.HasText(want) {
			t.Errorf("English: missing %q; texts %q", want, tt.Texts())
		}
	}
	saveShot(t, tt, "popover-en.png")

	clickButton(t, tt, locales.Copy())
	got := tt.Clipboard()
	for _, want := range []string{"Hostname: web-1\n", "OS: Debian GNU/Linux 12 (bookworm)\n", "CPU:\n  Intel(R) Xeon(R) CPU E5-2680 v4 @ 2.40GHz\n  2 cores, 4 threads\n", "IP addresses:\n  ens3  45.56.78.90/24\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary lacks %q:\n%s", want, got)
		}
	}

	// Closed, then opened in Chinese: it reads again.
	tt.Key(0, ui.KeyEscape)
	if a.sysInfo.open {
		t.Fatal("Escape left the popover open")
	}
	a.setLanguage("zh-Hans")
	tt.Frame()
	clickButton(t, tt, "系统信息")
	f.next(t).reply <- sysInfoReply{info: debianSample(t)}
	p.until(t, "the system info", func() bool { return !a.sysInfo.loading })
	for _, want := range []string{"主机名", "内核", "处理器", "2 核 4 线程", "410 天 0:17", "IP 地址", "硬盘"} {
		if !tt.HasText(want) {
			t.Errorf("Chinese: missing %q; texts %q", want, tt.Texts())
		}
	}
	saveShot(t, tt, "popover-zh.png")
	tt.Key(0, ui.KeyEscape)

	// An error, then a retry that works.
	a.setLanguage("en")
	tt.Frame()
	clickButton(t, tt, locales.SystemInfo())
	f.next(t).reply <- sysInfoReply{err: monitor.ErrSysInfoOutput}
	p.until(t, "the error", func() bool { return !a.sysInfo.loading })
	if want := locales.SysInfoFailed(locales.SysInfoNoShell()); !tt.HasText(want) {
		t.Errorf("no error shown; texts %q", tt.Texts())
	}
	saveShot(t, tt, "popover-error-en.png")
	clickButton(t, tt, locales.Retry())
	f.next(t).reply <- sysInfoReply{info: debianSample(t)}
	p.until(t, "the retry", func() bool { return !a.sysInfo.loading })
	if !tt.HasText("x86_64") {
		t.Errorf("retry not shown; texts %q", tt.Texts())
	}
}

// Results of a read that was closed, reopened, or whose session went
// away are dropped.
func TestSysInfoStaleReads(t *testing.T) {
	a, p := testApp(t)
	web := demoSession(a, "web-1", "45.56.78.90")
	demoSession(a, "db-1", "10.0.0.12")
	a.active = 0
	f := newFakeSysInfo(a)
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	tt.Frame()

	// Opened twice quickly: the first read is canceled, its answer dropped.
	clickButton(t, tt, locales.SystemInfo())
	first := f.next(t)
	clickButton(t, tt, locales.SystemInfo())
	if a.sysInfo.open {
		t.Fatal("a second click left it open")
	}
	clickButton(t, tt, locales.SystemInfo())
	second := f.next(t)
	if first.ctx.Err() == nil {
		t.Error("the first read was not canceled")
	}
	first.reply <- sysInfoReply{info: monitor.SysInfo{Hostname: "stale"}}
	second.reply <- sysInfoReply{info: monitor.SysInfo{Hostname: "fresh"}}
	p.until(t, "the second read", func() bool { return !a.sysInfo.loading })
	p.until(t, "the posts", func() bool { return len(p.posts) == 0 })
	if a.sysInfo.info.Hostname != "fresh" || tt.HasText("stale") {
		t.Errorf("shown %q", a.sysInfo.info.Hostname)
	}

	// The session closes mid-read: the popover goes, and the other
	// session gets nothing of it.
	tt.Key(0, ui.KeyEscape)
	clickButton(t, tt, locales.SystemInfo())
	pending := f.next(t)
	a.closeSession(web)
	tt.Frame()
	if a.sysInfo.open {
		t.Fatal("open after its session closed")
	}
	pending.reply <- sysInfoReply{err: errors.New("connection closed")}
	p.until(t, "the posts", func() bool { return len(p.posts) == 0 })
	tt.Frame()
	if a.sysInfo.err != nil || a.sysInfo.info.Hostname != "" || a.sysInfo.open {
		t.Errorf("stale state %+v", a.sysInfo)
	}

	// Switching sessions closes it too.
	clickButton(t, tt, locales.SystemInfo())
	f.next(t).reply <- sysInfoReply{info: monitor.SysInfo{Hostname: "db"}}
	demoSession(a, "web-2", "45.56.78.91")
	a.cycle(1)
	tt.Frame()
	if a.sysInfo.open {
		t.Error("open for another session")
	}
	p.until(t, "the posts", func() bool { return len(p.posts) == 0 })
	if a.sysInfo.info.Hostname == "db" {
		t.Error("the answer came after the switch and was kept")
	}
}

// A host with the monitor off opens no exec channel: the side panel says
// so over the skeleton, and the popover reads nothing.
func TestSysInfoMonitorOff(t *testing.T) {
	a, p := testApp(t)
	s := demoSession(a, "web-1", "45.56.78.90")
	s.Host.NoMonitor = true
	s.stats = monitor.Stats{}
	a.active = 0
	f := newFakeSysInfo(a)
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	tt.Frame()
	for _, want := range []string{"IP 45.56.78.90", locales.MonitorOff(), "Up -", "Load -"} {
		if !tt.HasText(want) {
			t.Errorf("side panel: missing %q; texts %q", want, tt.Texts())
		}
	}
	if tt.HasText(locales.ReadingServerInfo()) {
		t.Error("waiting for stats that never come")
	}
	saveShot(t, tt, "monitor-off-en.png")
	clickButton(t, tt, locales.SystemInfo())
	if !tt.HasText(locales.SysInfoOff()) {
		t.Errorf("no message; texts %q", tt.Texts())
	}
	if len(f.reads) != 0 {
		t.Error("read with the monitor off")
	}
	tt.Key(0, ui.KeyEscape)
	a.setLanguage("zh-Hans")
	tt.Frame()
	if !tt.HasText("此主机已关闭服务器监控") {
		t.Errorf("Chinese: texts %q", tt.Texts())
	}
	saveShot(t, tt, "monitor-off-zh.png")
}
