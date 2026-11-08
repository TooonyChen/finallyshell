package monitor

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestParseProcsProcps(t *testing.T) {
	var l ProcessLister
	got, err := l.parse(readSample(t, "procs-procps.txt"))
	if err != nil {
		t.Fatal(err)
	}
	want := []Process{
		{PID: 1, PPID: 0, User: "root", RSS: 12040 * 1024, CPU: 0, Name: "systemd", Args: "/sbin/init splash", State: 'S', Start: 2},
		{PID: 2, PPID: 0, User: "root", RSS: 0, CPU: 0, Name: "kthreadd", Args: "[kthreadd]", State: 'S', Start: 2},
		{PID: 812, PPID: 1, User: "mysql", RSS: 118000 * 1024, CPU: 1.7, Name: "mysqld", Args: "/usr/sbin/mysqld --daemonize --pid-file=/run/mysqld/mysqld.pid", State: 'S', Start: 900},
		{PID: 7267, PPID: 1, User: "tomcat", RSS: 1930000 * 1024, CPU: 3.3, Name: "java", Args: "/usr/bin/java -Xmx2g -jar /opt/app/app.jar --spring.profiles.active=prod", State: 'S', Start: 3000},
		{PID: 4021, PPID: 4011, User: "deploy", RSS: 5120 * 1024, Name: "bash", Args: "-bash", Path: "/usr/bin/bash", State: 'S', Start: 50000},
		// A name with spaces and parentheses, and the spaces of the
		// command line kept.
		{PID: 4090, PPID: 4021, User: "deploy", RSS: 2048 * 1024, Name: "tmux: server (1)", Args: "tmux new -s  main", Path: "/usr/bin/tmux (deleted)", State: 'S', Start: 50100},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got\n%+v\nwant\n%+v", got, want)
	}
}

// BusyBox's ps has no pcpu and prints large sizes shorter; a header
// line, as older versions print one, is skipped; processes whose stat
// was not read keep a name from their command line.
func TestParseProcsBusyBox(t *testing.T) {
	var l ProcessLister
	got, err := l.parse(readSample(t, "procs-busybox.txt"))
	if err != nil {
		t.Fatal(err)
	}
	want := []Process{
		{PID: 1, User: "root", RSS: 880 * 1024, Name: "init", Args: "/sbin/init", Path: "/bin/busybox", State: 'S', Start: 5},
		{PID: 2, User: "root", Name: "kthreadd", Args: "[kthreadd]"},
		{PID: 1501, PPID: 1, User: "root", RSS: 310 << 20, Name: "dockerd", Args: "/usr/bin/dockerd --host=unix:///var/run/docker.sock", Path: "/usr/bin/dockerd", State: 'S', Start: 400},
		{PID: 1777, PPID: 1, User: "nobody", RSS: 1288490188, Name: "node", Args: "node /srv/app/server.js"},
		{PID: 2001, PPID: 1, User: "root", RSS: 2304 * 1024, Name: "sshd", Args: "/usr/sbin/sshd -D"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got\n%+v\nwant\n%+v", got, want)
	}
}

// The CPU percent of the second read is the process's CPU time since
// the first over one CPU's: top's way, rather than ps's average over the
// process's life.
func TestProcsCPUDeltas(t *testing.T) {
	first := readSample(t, "procs-procps.txt")
	// 2 s later on 2 CPUs at 100 Hz: 400 jiffies in all, 200 of one CPU.
	second := strings.NewReplacer(
		"cpu  10000 0 2000 80000", "cpu  10300 0 2000 80100",
		"cpu0", "cpu0", // the CPUs are counted, their numbers unread
		// java used 150 ticks (75% of a CPU) and mysqld none.
		"7267 (java) S 1 7267 7267 0 -1 4194560 9000 0 0 0 20000 5000", "7267 (java) S 1 7267 7267 0 -1 4194560 9000 0 0 0 20100 5050",
		// The bash ended and another process has its PID: its time since
		// it started, 30 ticks, counts in full.
		"4021 (bash) S 4011 4021 4021 34816 4090 4194560 900 0 0 0 10 5 0 0 20 0 1 0 50000", "4021 (bash) S 4011 4021 4021 34816 4090 4194560 900 0 0 0 20 10 0 0 20 0 1 0 50150",
	).Replace(first)
	var l ProcessLister
	if _, err := l.parse(first); err != nil {
		t.Fatal(err)
	}
	got, err := l.parse(second)
	if err != nil {
		t.Fatal(err)
	}
	cpu := map[int]float64{}
	for _, p := range got {
		cpu[p.PID] = p.CPU
	}
	for pid, want := range map[int]float64{7267: 75, 812: 0, 1: 0, 4021: 15} {
		if math.Abs(cpu[pid]-want) > 1e-9 {
			t.Errorf("pid %d: CPU %v, want %v", pid, cpu[pid], want)
		}
	}
}

func TestParseProcsNotLinux(t *testing.T) {
	var l ProcessLister
	out := "---FINALLYSHELL:self---\n12\n---FINALLYSHELL:uname---\nFreeBSD\n---FINALLYSHELL:ps---\n---FINALLYSHELL:end---\n"
	if _, err := l.parse(out); !errors.Is(err, ErrNotLinux) {
		t.Errorf("FreeBSD: %v", err)
	}
}

// Lines missing fields, or not a process, are skipped.
func TestParsePSMissingFields(t *testing.T) {
	got := parsePS("\n  12\n x 1 root 10 0.0 a\n 13 1 root\n 14 1 root 20\n 15 1 root 30 0.5\n 16 1 root 4k [k]\n")
	want := []Process{
		{PID: 14, PPID: 1, User: "root", RSS: 20 * 1024},
		{PID: 15, PPID: 1, User: "root", RSS: 30 * 1024, CPU: 0.5},
		{PID: 16, PPID: 1, User: "root", RSS: 4 * 1024, Args: "[k]"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
	if _, _, ok := parseProcStat("17 (short) S 1 2 3"); ok {
		t.Error("a short stat line was read")
	}
}

func TestParseDetails(t *testing.T) {
	own := "---FINALLYSHELL:uname---\nLinux\n" +
		"---FINALLYSHELL:stat---\n7267 (java) S 1 7267 7267 0 -1 4194560 9000 0 0 0 20000 5000 0 0 20 0 60 0 3000 1 1\n\n---FINALLYSHELL:stat:ok---\n" +
		"---FINALLYSHELL:owner---\ndr-xr-xr-x 9 tomcat tomcat 0 Nov  8 22:48 /proc/7267\n\n---FINALLYSHELL:owner:ok---\n" +
		"---FINALLYSHELL:cmdline---\n/usr/bin/java\x00-jar\x00/opt/app my.jar\x00\n---FINALLYSHELL:cmdline:ok---\n" +
		"---FINALLYSHELL:exe---\n/usr/lib/jvm/bin/java\n\n---FINALLYSHELL:exe:ok---\n" +
		"---FINALLYSHELL:cwd---\n/opt/app\n\n---FINALLYSHELL:cwd:ok---\n" +
		"---FINALLYSHELL:environ---\nPATH=/usr/bin\x00JAVA_OPTS=-Dx=1 -Dy=a=b\x00MULTI=line1\nline2\x00EMPTY=\x00\n---FINALLYSHELL:environ:ok---\n" +
		"---FINALLYSHELL:end---\n"
	d, err := parseDetails(7267, own)
	if err != nil {
		t.Fatal(err)
	}
	want := ProcessDetails{
		PID: 7267, Name: "java", User: "tomcat", State: 'S', Start: 3000,
		Args: []string{"/usr/bin/java", "-jar", "/opt/app my.jar"},
		Path: "/usr/lib/jvm/bin/java", Cwd: "/opt/app",
		Env: []EnvVar{{"PATH", "/usr/bin"}, {"JAVA_OPTS", "-Dx=1 -Dy=a=b"}, {"MULTI", "line1\nline2"}, {"EMPTY", ""}},
	}
	if !reflect.DeepEqual(d, want) {
		t.Errorf("got  %+v\nwant %+v", d, want)
	}

	// Another user's process, read without root.
	other := "---FINALLYSHELL:uname---\nLinux\n" +
		"---FINALLYSHELL:stat---\n1 (systemd) S 0 1 1 0 -1 4194560 52 1 0 0 120 80 0 0 20 0 1 0 2 172 3010\n\n---FINALLYSHELL:stat:ok---\n" +
		"---FINALLYSHELL:owner---\ndr-xr-xr-x 9 root root 0 Nov  8 22:48 /proc/1\n\n---FINALLYSHELL:owner:ok---\n" +
		"---FINALLYSHELL:cmdline---\n/sbin/init\x00\n---FINALLYSHELL:cmdline:ok---\n" +
		"---FINALLYSHELL:exe---\n\n---FINALLYSHELL:exe:fail---\n" +
		"---FINALLYSHELL:cwd---\n\n---FINALLYSHELL:cwd:fail---\n" +
		"---FINALLYSHELL:environ---\n\n---FINALLYSHELL:environ:fail---\n" +
		"---FINALLYSHELL:end---\n"
	d, err = parseDetails(1, other)
	if err != nil {
		t.Fatal(err)
	}
	if d.User != "root" || d.Path != "" || !d.PathDenied || !d.CwdDenied || !d.EnvDenied || d.Env != nil || len(d.Args) != 1 {
		t.Errorf("other user's: %+v", d)
	}

	// A kernel thread has no command line, executable or directory, and
	// they are not denied.
	kernel := strings.NewReplacer("/sbin/init\x00", "", "(systemd)", "(kthreadd)").Replace(other)
	if d, _ := parseDetails(2, kernel); d.Args != nil || d.PathDenied || d.CwdDenied {
		t.Errorf("kernel thread: %+v", d)
	}

	gone := "---FINALLYSHELL:uname---\nLinux\n---FINALLYSHELL:gone---\n---FINALLYSHELL:end---\n"
	if _, err := parseDetails(5, gone); !errors.Is(err, ErrNoProcess) {
		t.Errorf("gone: %v", err)
	}
}

func TestCleanKillMessage(t *testing.T) {
	for in, want := range map[string]string{
		"sh: 1: kill: No such process":                         "No such process",
		"bash: line 2: kill: (42) - Operation not permitted":   "Operation not permitted",
		"sh: can't kill pid 42: Operation not permitted":       "Operation not permitted",
		"kill: sending signal to 42 failed: Permission denied": "Permission denied",
		"weird":                                 "weird",
		"sh: kill: (1) - No such process\nmore": "No such process",
	} {
		if got := cleanKillMessage(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestStatRunning(t *testing.T) {
	line := "42 (sleep) S 1 42 42 0 -1 4194560 52 1 0 0 0 0 0 0 20 0 1 0 777 172 3010\n" + procMark + "end---\n"
	if !statRunning(line, 42, 777) || !statRunning(line, 42, 0) {
		t.Error("running sleep not running")
	}
	if statRunning(line, 42, 778) {
		t.Error("another process with the PID is the one killed")
	}
	if statRunning(strings.Replace(line, ") S", ") Z", 1), 42, 777) {
		t.Error("a zombie runs")
	}
	if statRunning(procMark+"end---\n", 42, 777) {
		t.Error("nothing runs")
	}
}

func TestProcSections(t *testing.T) {
	got := procSections("junk\n---FINALLYSHELL:a---\none\ntwo\n---FINALLYSHELL:b---\n---FINALLYSHELL:c---\nno newline")
	want := map[string]string{"": "", "a": "one\ntwo", "b": "", "c": "no newline"}
	delete(got, "")
	delete(want, "")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q", got)
	}
}
