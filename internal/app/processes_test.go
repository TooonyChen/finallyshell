package app

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"finallyshell/internal/monitor"

	"github.com/egoist/mygo/ui"
	"golang.org/x/crypto/ssh"
)

// fakeProcesses stands in for a server's processes.
type fakeProcesses struct {
	mu      sync.Mutex
	procs   []monitor.Process
	listErr error
	lists   int // reads of the list
	info    map[int]monitor.ProcessDetails
	signals []string     // "TERM 7267@3000": the signal, the PID and the start time, as sent
	ended   map[int]bool // processes that ended: signals to them are not sent
	sigErr  error
	// alive is the processes still running after a signal.
	alive map[int]bool
}

func (f *fakeProcesses) list(ctx context.Context, client *ssh.Client, l *monitor.ProcessLister) ([]monitor.Process, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lists++
	if f.listErr != nil {
		return nil, f.listErr
	}
	return slices.Clone(f.procs), nil
}

func (f *fakeProcesses) details(ctx context.Context, client *ssh.Client, pid int) (monitor.ProcessDetails, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.info[pid]
	if !ok {
		return d, monitor.ErrNoProcess
	}
	return d, nil
}

func (f *fakeProcesses) signal(ctx context.Context, client *ssh.Client, pid int, start uint64, sig string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ended[pid] {
		return monitor.ErrNoProcess
	}
	f.signals = append(f.signals, sig+" "+strconv.Itoa(pid)+"@"+strconv.FormatUint(start, 10))
	return f.sigErr
}

func (f *fakeProcesses) running(ctx context.Context, client *ssh.Client, pid int, start uint64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.alive[pid], nil
}

func (f *fakeProcesses) reads() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lists
}

func (f *fakeProcesses) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.signals)
}

// set changes f under its lock, as the reads run on goroutines.
func (f *fakeProcesses) set(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn()
}

func demoProcesses() *fakeProcesses {
	return &fakeProcesses{
		procs: []monitor.Process{
			{PID: 1, User: "root", RSS: 12_300_000, CPU: 0.1, Name: "systemd", Args: "/sbin/init splash", Path: "/usr/lib/systemd/systemd", Start: 2},
			{PID: 812, User: "mysql", RSS: 118_000_000, CPU: 1.7, Name: "mysqld", Args: "/usr/sbin/mysqld --daemonize --pid-file=/run/mysqld/mysqld.pid", Path: "/usr/sbin/mysqld", Start: 900},
			{PID: 7267, User: "tomcat", RSS: 1_930_000_000, CPU: 33.4, Name: "java", Args: "/usr/bin/java -Xmx2g -jar /opt/app/app.jar --spring.profiles.active=prod", Path: "/usr/lib/jvm/java-17/bin/java", Start: 3000},
			{PID: 1290, User: "root", RSS: 9_800_000, CPU: 0, Name: "sshd", Args: "sshd: /usr/sbin/sshd -D [listener] 0 of 10-100 startups", Path: "/usr/sbin/sshd", Start: 400},
			{PID: 4021, User: "deploy", RSS: 5_200_000, CPU: 0.3, Name: "bash", Args: "-bash", Path: "/usr/bin/bash", Start: 50000},
			{PID: 4090, User: "deploy", RSS: 3_100_000, CPU: 0, Name: "tmux: server", Args: "tmux new -s main", Path: "/usr/bin/tmux", Start: 50100},
			{PID: 2, User: "root", Name: "kthreadd", Args: "[kthreadd]", Start: 2},
		},
		info: map[int]monitor.ProcessDetails{
			7267: {
				PID: 7267, Name: "java", User: "tomcat", Start: 3000,
				Args: []string{"/usr/bin/java", "-Xmx2g", "-jar", "/opt/app/app.jar", "--spring.profiles.active=prod"},
				Path: "/usr/lib/jvm/java-17/bin/java", Cwd: "/opt/app",
				Env: []monitor.EnvVar{
					{Name: "PATH", Value: "/usr/local/bin:/usr/bin:/bin"},
					{Name: "JAVA_HOME", Value: "/usr/lib/jvm/java-17"},
					{Name: "LANG", Value: "en_US.UTF-8"},
					{Name: "CATALINA_BASE", Value: "/opt/app"},
					{Name: "SPRING_PROFILES_ACTIVE", Value: "prod"},
					{Name: "USER", Value: "tomcat"},
				},
			},
			1: {PID: 1, Name: "systemd", User: "root", Start: 2, Args: []string{"/sbin/init", "splash"}, PathDenied: true, CwdDenied: true, EnvDenied: true},
		},
		alive: map[int]bool{},
		ended: map[int]bool{},
	}
}

// processesApp is an app showing a connected session's Processes tab,
// on fake processes.
func processesApp(t *testing.T) (*App, *fakePlatform, *Session, *fakeProcesses, *ui.Tester) {
	t.Helper()
	a, p := testApp(t)
	s := demoSession(a, "web-1", "45.56.78.90")
	a.active = 0
	f := demoProcesses()
	a.processes.api = f
	a.bottomTab = tabProcesses
	a.terminalPct = 28 // room for the list and the details
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	tt.Frame()
	p.until(t, "the processes", func() bool { return s.procs.loaded })
	return a, p, s, f, tt
}

func TestProcessesList(t *testing.T) {
	a, p, s, f, tt := processesApp(t)
	for _, want := range []string{"Processes", "PID", "User", "Memory", "CPU %", "Command line", "Path", "java", "tomcat", "33.4", "7 processes", "/usr/sbin/mysqld"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q", want)
		}
	}
	// CPU, descending, first.
	if view := s.procs.shown(); view[0].PID != 7267 || view[1].PID != 812 {
		t.Errorf("first rows %d, %d", view[0].PID, view[1].PID)
	}
	s.procs.pid = 7267
	tt.Frame()
	saveShot(t, tt, "processes-en.png")
	a.setLanguage("zh-Hans")
	tt.Frame()
	for _, want := range []string{"进程", "用户", "内存", "命令行", "路径", "7 个进程"} {
		if !tt.HasText(want) {
			t.Errorf("Chinese: missing %q", want)
		}
	}
	saveShot(t, tt, "processes-zh.png")
	a.setLanguage("en")

	// The filter matches the name, the command line and the user.
	for query, want := range map[string][]int{"MYSQL": {812}, "deploy": {4021, 4090}, "spring": {7267}, "nothing": nil} {
		s.procs.filter = query
		var got []int
		for _, pr := range s.procs.shown() {
			got = append(got, pr.PID)
		}
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Errorf("filter %q: %v, want %v", query, got, want)
		}
	}
	s.procs.filter = "deploy"
	tt.Frame()
	if !tt.HasText("2 of 7 processes") {
		t.Errorf("no filtered count; texts %q", tt.Texts())
	}
	s.procs.filter = ""

	// A click on a header sorts by it.
	if err := tt.Click("Memory"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.procs.sort.Column != "mem" {
		t.Errorf("sort %+v", s.procs.sort)
	}
	if view := s.procs.shown(); view[0].PID != 2 {
		t.Errorf("by memory, ascending: first %d", view[0].PID)
	}

	// A refresh keeps the choice on its process, wherever it goes.
	f.set(func() {
		f.procs = append([]monitor.Process{{PID: 9000, User: "root", RSS: 1, Name: "new", Args: "new"}}, f.procs...)
	})
	s.procs.poll.pollNow()
	p.until(t, "the new process", func() bool { return len(s.procs.list) == 8 })
	tt.Frame()
	if s.procs.pid != 7267 || s.procs.row < 0 || s.procs.shown()[s.procs.row].PID != 7267 {
		t.Errorf("choice %d at row %d", s.procs.pid, s.procs.row)
	}

	// Polling stops while another tab shows, and starts again with it.
	a.bottomTab = tabFiles
	tt.Frame()
	if s.procs.poll.run != nil || len(a.polls) != 0 {
		t.Error("still polling behind the Files tab")
	}
	n := f.reads()
	time.Sleep(50 * time.Millisecond)
	if f.reads() != n {
		t.Error("read while not shown")
	}
	a.bottomTab = tabProcesses
	tt.Frame()
	p.until(t, "a read again", func() bool { return f.reads() > n })

	// Disconnected: the last list stays, greyed, with the banner.
	s.status, s.err = statusClosed, errConnectionLost
	tt.Frame()
	if s.procs.poll.run != nil || !tt.HasText("java") || !tt.HasText("Reconnect") {
		t.Errorf("disconnected: polling %v; texts %q", s.procs.poll.run != nil, tt.Texts())
	}
	s.status, s.err = statusConnected, nil
}

func TestProcessesDetails(t *testing.T) {
	a, p, s, _, tt := processesApp(t)
	view := s.procs.shown()
	a.openProcTab(s, view[0]) // java
	p.until(t, "the details", func() bool { return !s.procs.tabs[0].loading })
	tt.Frame()
	for _, want := range []string{"7267-java", "Working directory", "/opt/app", "/usr/lib/jvm/java-17/bin/java", "Environment", "JAVA_HOME", "/usr/lib/jvm/java-17", "SPRING_PROFILES_ACTIVE"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q; texts %q", want, tt.Texts())
		}
	}
	s.procs.pid = 7267
	tt.Frame()
	saveShot(t, tt, "process-details-en.png")

	// Opening it again chooses its tab; another opens a second.
	a.openProcTab(s, view[0])
	if len(s.procs.tabs) != 1 {
		t.Errorf("%d tabs", len(s.procs.tabs))
	}
	systemd := s.procs.list[0]
	a.openProcTab(s, systemd)
	p.until(t, "systemd's details", func() bool { return !s.procs.tabs[1].loading })
	tt.Frame()
	if !tt.HasText("1-systemd") || !tt.HasText("Needs root") {
		t.Errorf("another user's process; texts %q", tt.Texts())
	}

	// A process gone since.
	a.openProcTab(s, monitor.Process{PID: 4021, Name: "bash"})
	p.until(t, "bash's details", func() bool { return !s.procs.tabs[2].loading })
	tt.Frame()
	if !tt.HasText("Process 4021 is no longer running.") {
		t.Errorf("gone; texts %q", tt.Texts())
	}

	// Closing tabs.
	a.closeProcTab(s, 2)
	a.closeProcTab(s, 1)
	if len(s.procs.tabs) != 1 || s.procs.tab != 0 {
		t.Errorf("tabs %d, chosen %d", len(s.procs.tabs), s.procs.tab)
	}
	if got := procDetailsSummary(s.procs.tabs[0]); !strings.Contains(got, "Working directory: /opt/app") || !strings.Contains(got, "  JAVA_HOME=/usr/lib/jvm/java-17") {
		t.Errorf("summary %q", got)
	}

	a.setLanguage("zh-Hans")
	tt.Frame()
	for _, want := range []string{"工作目录", "环境变量", "JAVA_HOME"} {
		if !tt.HasText(want) {
			t.Errorf("Chinese: missing %q", want)
		}
	}
	saveShot(t, tt, "process-details-zh.png")

	// The context menu opens the details too.
	a.setLanguage("en")
	a.closeProcTab(s, 0)
	tt.Frame()
	if err := tt.RightClick("/usr/sbin/mysqld --daemonize --pid-file=/run/mysqld/mysqld.pid"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Open Details"); err != nil {
		t.Fatalf("%v; menu %q", err, tt.Menu())
	}
	tt.Frame()
	if len(s.procs.tabs) != 1 || s.procs.tabs[0].pid != 812 {
		t.Errorf("menu opened %d tabs", len(s.procs.tabs))
	}
}

func TestProcessesSignals(t *testing.T) {
	killWait = 10 * time.Millisecond
	defer func() { killWait = 3 * time.Second }()
	a, p, s, f, tt := processesApp(t)
	java := s.procs.list[2]

	// Signals ask first.
	if err := tt.RightClick(java.Args); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Send Signal", "SIGHUP"); err != nil {
		t.Fatalf("%v; menu %q", err, tt.Menu())
	}
	tt.Frame()
	if d := a.processes.dlg; d == nil || d.sig != "HUP" || d.pid != 7267 || !tt.HasText("Send SIGHUP to java (7267)?") {
		t.Fatalf("dialog %+v; texts %q", a.processes.dlg, tt.Texts())
	}
	if len(f.sent()) != 0 {
		t.Fatal("sent before asking")
	}
	// The server's refusal shows in the dialog.
	f.set(func() { f.sigErr = &monitor.SignalError{Msg: "Operation not permitted"} })
	tt.Click("Send")
	p.until(t, "the refusal", func() bool { return a.processes.dlg != nil && a.processes.dlg.err != nil })
	tt.Frame()
	if !tt.HasText("The server refused: Operation not permitted") {
		t.Errorf("no refusal; texts %q", tt.Texts())
	}
	saveShot(t, tt, "signal-refused-en.png")
	f.set(func() { f.sigErr = nil })
	tt.Click("Send")
	p.until(t, "the dialog closed", func() bool { return a.processes.dlg == nil })
	if got := f.sent(); !slices.Equal(got, []string{"HUP 7267@3000", "HUP 7267@3000"}) {
		t.Errorf("sent %q", got)
	}

	// Kill: TERM, then KILL when it is still running.
	f.set(func() { f.alive[7267] = true })
	a.askSignal(s, java, "TERM", true)
	tt.Frame()
	if !tt.HasText("Kill java (7267)?") {
		t.Fatalf("no kill dialog; texts %q", tt.Texts())
	}
	tt.Click("Kill")
	p.until(t, "the offer of SIGKILL", func() bool { return a.processes.dlg != nil && a.processes.dlg.still })
	tt.Frame()
	if !tt.HasText("java (7267) is still running") {
		t.Errorf("no offer; texts %q", tt.Texts())
	}
	saveShot(t, tt, "kill-still-running-en.png")
	tt.Click("Force Kill")
	p.until(t, "the dialog closed", func() bool { return a.processes.dlg == nil })
	if got := f.sent(); !slices.Equal(got[2:], []string{"TERM 7267@3000", "KILL 7267@3000"}) {
		t.Errorf("sent %q", got)
	}

	// A process that quits on TERM closes the dialog.
	f.set(func() { f.alive[7267] = false })
	a.askSignal(s, java, "TERM", true)
	tt.Frame()
	tt.Click("Kill")
	p.until(t, "the dialog closed", func() bool { return a.processes.dlg == nil })
	if got := f.sent(); len(got) != 5 || got[4] != "TERM 7267@3000" {
		t.Errorf("sent %q", got)
	}

	// Cancel sends nothing.
	a.askSignal(s, java, "STOP", false)
	tt.Frame()
	tt.Click("Cancel")
	tt.Frame()
	if a.processes.dlg != nil || len(f.sent()) != 5 {
		t.Errorf("cancel: dialog %v, sent %q", a.processes.dlg != nil, f.sent())
	}

	// A process that ended while the dialog was open, its PID maybe
	// another's now, gets no signal: from the menu's signals, Kill, and
	// Force Kill alike.
	f.set(func() { f.ended[7267] = true })
	for _, step := range []struct {
		sig        string
		kill, more bool
	}{{"HUP", false, false}, {"TERM", true, false}, {"TERM", true, true}} {
		a.askSignal(s, java, step.sig, step.kill)
		if step.more { // the offer of Force Kill, open when it ended
			a.processes.dlg.still = true
		}
		a.sendSignal(a.processes.dlg)
		p.until(t, "the answer", func() bool { return a.processes.dlg != nil && !a.processes.dlg.busy })
		tt.Frame()
		if d := a.processes.dlg; !d.gone || !tt.HasText("The process has already ended. No signal was sent.") {
			t.Errorf("%+v: gone %v; texts %q", step, d.gone, tt.Texts())
		}
		saveShot(t, tt, "signal-gone-en.png")
		tt.Click("Close")
		tt.Frame()
		if a.processes.dlg != nil {
			t.Errorf("%+v: Close left the dialog open", step)
		}
	}
	if got := f.sent(); len(got) != 5 {
		t.Errorf("signals sent to an ended process: %q", got[5:])
	}
}

func TestProcessesNotLinux(t *testing.T) {
	a, p := testApp(t)
	s := demoSession(a, "bsd", "10.0.0.9")
	a.active = 0
	a.processes.api = &fakeProcesses{listErr: monitor.ErrNotLinux}
	a.bottomTab = tabProcesses
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	tt.Frame()
	p.until(t, "the error", func() bool { return s.procs.err != nil })
	tt.Frame()
	if !tt.HasText("Only Linux servers are supported") {
		t.Errorf("texts %q", tt.Texts())
	}

	// Another error says what it was.
	s.procs.err = errors.New("boom")
	tt.Frame()
	if !tt.HasText("Could not list the processes: boom") {
		t.Errorf("texts %q", tt.Texts())
	}
}

// A click on the side panel's process list opens the Processes tab.
func TestSidebarOpensProcesses(t *testing.T) {
	a, p := testApp(t)
	demoSession(a, "web-1", "45.56.78.90")
	a.active = 0
	a.processes.api = demoProcesses()
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	tt.Frame()
	if a.bottomTab != tabFiles {
		t.Fatalf("tab %d", a.bottomTab)
	}
	if err := tt.Click("mysqld"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.bottomTab != tabProcesses {
		t.Errorf("tab %d after the click", a.bottomTab)
	}
}
