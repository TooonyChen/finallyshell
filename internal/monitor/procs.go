package monitor

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/crypto/ssh"
)

// Process is a row of the process manager: a process of the server, as
// ps and /proc tell of it.
type Process struct {
	PID  int
	PPID int
	User string
	RSS  uint64 // bytes
	// CPU is the percent of one CPU the process used since the last
	// read, as top shows it: a busy process with threads can pass 100.
	// The first read has ps's average over the process's life, or 0
	// where ps has none (BusyBox).
	CPU   float64
	Name  string // the kernel's name of it (comm)
	Args  string // the command line
	Path  string // the executable; "" when it cannot be read (other users' processes need root)
	State byte   // R, S, D, Z, T, … ; 0 when unknown
	// Start is when the process started, in clock ticks after boot: with
	// the PID, it tells a process from a later one given the same PID.
	Start uint64
}

// procMark starts each section of the process scripts' output.
const procMark = "---FINALLYSHELL:"

// procsScript lists the processes in one round trip. It is POSIX sh and
// runs with BusyBox's tools: BusyBox's ps has no -e, pcpu or widths, so
// the second ps is the one it runs. The kernel's name and the CPU time
// come from /proc/<pid>/stat, which every process may read, and the
// executables from one ls of the /proc/<pid>/exe links rather than a
// readlink for each: those of other users' processes fail without root,
// and their errors are dropped. The script's own processes (the shell,
// ps, cat and ls) are those whose parent is $$, left out of the list.
const procsScript = `export LC_ALL=C
m() { echo "` + procMark + `$1---"; }
m self; echo $$
m uname; uname -s
m cpu; grep '^cpu' /proc/stat 2>/dev/null
m ps; ps -eo pid=,ppid=,user:32=,rss=,pcpu=,args= 2>/dev/null || ps -o pid=,ppid=,user=,rss=,args= 2>/dev/null
m stat; cat /proc/[0-9]*/stat 2>/dev/null
m exe; ls -l /proc/[0-9]*/exe 2>/dev/null
m end
`

// ErrNotLinux is a server whose system is not Linux: the process
// manager reads /proc, which only Linux has.
var ErrNotLinux = errors.New("not a Linux server")

// ErrScriptOutput is a server whose answer to a script is not the
// script's: it has no POSIX shell.
var ErrScriptOutput = errors.New("the server did not run the script")

// ErrNoProcess is a process that is not there, or no longer.
var ErrNoProcess = errors.New("no such process")

// procOutputLimit is how much of a script's output is read: a few
// thousand processes with long command lines fit.
const procOutputLimit = 32 << 20

// RunScript runs a POSIX sh script on a new exec channel of client and
// returns what it wrote on stdout and stderr, stopping it when ctx ends.
// The script goes on stdin, so the login shell, which may be fish or
// csh, only has to start sh. err is the channel's error or the script's
// exit status (*ssh.ExitError); the output read is returned with it.
func RunScript(ctx context.Context, client *ssh.Client, script string) (stdout, stderr string, err error) {
	if client == nil {
		return "", "", errors.New("not connected")
	}
	s, err := client.NewSession()
	if err != nil {
		return "", "", err
	}
	defer s.Close()
	stop := context.AfterFunc(ctx, func() { s.Close() })
	defer stop()
	s.Stdin = strings.NewReader(script)
	var errBuf bytes.Buffer
	s.Stderr = &limitWriter{w: &errBuf, n: 64 << 10}
	out, err := s.StdoutPipe()
	if err != nil {
		return "", "", err
	}
	if err := s.Start("sh -s"); err != nil {
		return "", "", err
	}
	data, rerr := io.ReadAll(io.LimitReader(out, procOutputLimit))
	io.Copy(io.Discard, out) // past the limit, let the script end
	werr := s.Wait()
	if ctx.Err() != nil {
		return string(data), errBuf.String(), ctx.Err()
	}
	if werr == nil {
		werr = rerr
	}
	return string(data), errBuf.String(), werr
}

// limitWriter keeps the first n bytes written to it and drops the rest.
type limitWriter struct {
	w io.Writer
	n int
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if l.n > 0 {
		k := min(len(p), l.n)
		l.w.Write(p[:k])
		l.n -= k
	}
	return len(p), nil
}

// ProcessLister reads the server's processes, again and again: it keeps
// the CPU time of each between reads, for CPU percents over the time
// between them. Its zero value is ready; it is not safe for concurrent
// use.
type ProcessLister struct {
	total uint64              // the CPUs' jiffies at the last read
	ticks map[pidStart]uint64 // each process's utime+stime at the last read
}

type pidStart struct {
	pid   int
	start uint64
}

// List runs the script once over a new exec channel of client.
func (l *ProcessLister) List(ctx context.Context, client *ssh.Client) ([]Process, error) {
	out, _, err := RunScript(ctx, client, procsScript)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if !strings.Contains(out, procMark+"end---") {
		if err != nil {
			return nil, err
		}
		return nil, ErrScriptOutput
	}
	return l.parse(out)
}

// parse reads the output of procsScript, and the CPU percents against
// the last parse.
func (l *ProcessLister) parse(out string) ([]Process, error) {
	sec := procSections(out)
	if sys := strings.TrimSpace(sec["uname"]); sys != "" && sys != "Linux" {
		return nil, ErrNotLinux
	}
	self, _ := strconv.Atoi(strings.TrimSpace(sec["self"]))
	procs := parsePS(sec["ps"])
	stats := parseProcStats(sec["stat"])
	exes := parseExeLinks(sec["exe"])
	total, ncpu := parseCPUTotal(sec["cpu"])

	elapsed := 0.0 // jiffies of one CPU since the last read
	if l.ticks != nil && total > l.total && ncpu > 0 {
		elapsed = float64(total-l.total) / float64(ncpu)
	}
	ticks := make(map[pidStart]uint64, len(stats))
	kept := procs[:0]
	for _, p := range procs {
		if self != 0 && (p.PID == self || p.PPID == self) {
			continue
		}
		if st, ok := stats[p.PID]; ok {
			p.Name, p.State, p.Start = st.name, st.state, st.start
			key := pidStart{p.PID, st.start}
			ticks[key] = st.ticks
			// A process new since the last read used all its time since.
			if prev := l.ticks[key]; elapsed > 0 && st.ticks >= prev {
				p.CPU = 100 * float64(st.ticks-prev) / elapsed
			}
		}
		if p.Name == "" {
			p.Name = nameFromArgs(p.Args)
		}
		p.Path = exes[p.PID]
		kept = append(kept, p)
	}
	l.total, l.ticks = total, ticks
	return kept, nil
}

// procSections splits a script's output by its marks: the text between a
// mark and the next, without the newline before the mark.
func procSections(out string) map[string]string {
	sections := map[string]string{}
	name := ""
	start := -1
	for i := 0; i < len(out); {
		end := strings.IndexByte(out[i:], '\n')
		line := out[i:]
		next := len(out)
		if end >= 0 {
			line, next = out[i:i+end], i+end+1
		}
		if n, ok := strings.CutPrefix(line, procMark); ok && strings.HasSuffix(n, "---") {
			if start >= 0 {
				sections[name] += strings.TrimSuffix(out[start:i], "\n")
			}
			name, start = strings.TrimSuffix(n, "---"), next
		}
		i = next
	}
	if start >= 0 && start <= len(out) {
		sections[name] += strings.TrimSuffix(out[start:], "\n")
	}
	return sections
}

// parsePS reads ps's lines: pid, ppid, user, rss, pcpu and args from
// procps, or the same without pcpu from BusyBox. A header line, as an
// older BusyBox prints, and lines that are not a process are skipped.
func parsePS(s string) []Process {
	var procs []Process
	sc := bufio.NewScanner(strings.NewReader(s))
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		line := sc.Text()
		f, rest := fieldsN(line, 5)
		if len(f) < 4 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		if err1 != nil || err2 != nil || pid <= 0 {
			continue
		}
		p := Process{PID: pid, PPID: ppid, User: f[2], RSS: parseKB(f[3])}
		// procps has pcpu before the args; BusyBox has the args there.
		if len(f) == 5 {
			if cpu, err := strconv.ParseFloat(f[4], 64); err == nil && isNumber(f[4]) {
				p.CPU, p.Args = cpu, rest
			} else {
				p.Args = strings.TrimSpace(f[4] + " " + rest)
			}
		}
		procs = append(procs, p)
	}
	return procs
}

// fieldsN splits the first n fields off s, by spaces, and returns them
// with the rest of the line, its own spaces kept, but for those around.
func fieldsN(s string, n int) (fields []string, rest string) {
	for len(fields) < n {
		s = strings.TrimLeft(s, " \t")
		if s == "" {
			return fields, ""
		}
		i := strings.IndexAny(s, " \t")
		if i < 0 {
			return append(fields, s), ""
		}
		fields, s = append(fields, s[:i]), s[i:]
	}
	return fields, strings.TrimSpace(s)
}

func isNumber(s string) bool {
	return s != "" && strings.Trim(s, "0123456789.") == ""
}

// parseKB reads a size in KiB, as ps's rss: a number, or BusyBox's
// shorter form for large ones, as 310m (MiB) or 1.2g.
func parseKB(s string) uint64 {
	mult := 1024.0
	if s != "" {
		switch s[len(s)-1] {
		case 'k', 'K':
			s = s[:len(s)-1]
		case 'm', 'M':
			mult, s = mult*1024, s[:len(s)-1]
		case 'g', 'G':
			mult, s = mult*1024*1024, s[:len(s)-1]
		case 't', 'T':
			mult, s = mult*1024*1024*1024, s[:len(s)-1]
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return 0
	}
	return uint64(v * mult)
}

// nameFromArgs is a process's name from its command line, for one whose
// stat was not read: the last part of its first word, or a kernel
// thread's name without its brackets.
func nameFromArgs(args string) string {
	if strings.HasPrefix(args, "[") && strings.HasSuffix(args, "]") {
		return args[1 : len(args)-1]
	}
	first, _, _ := strings.Cut(args, " ")
	if i := strings.LastIndexByte(first, '/'); i >= 0 && i < len(first)-1 {
		first = first[i+1:]
	}
	return first
}

// procStat is what a /proc/<pid>/stat line tells.
type procStat struct {
	name  string
	state byte
	ppid  int
	ticks uint64 // utime + stime
	start uint64
}

// parseProcStat reads one /proc/<pid>/stat line. The name is between the
// first ( and the last ), as it may hold spaces and parentheses itself.
func parseProcStat(line string) (pid int, st procStat, ok bool) {
	open := strings.IndexByte(line, '(')
	closing := strings.LastIndexByte(line, ')')
	if open < 0 || closing < open {
		return 0, st, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(line[:open]))
	if err != nil {
		return 0, st, false
	}
	st.name = line[open+1 : closing]
	f := strings.Fields(line[closing+1:])
	// After the name: state(3) ppid(4) … utime(14) stime(15) …
	// starttime(22), counted from 1 with the pid and the name.
	if len(f) < 20 {
		return 0, st, false
	}
	if len(f[0]) > 0 {
		st.state = f[0][0]
	}
	st.ppid, _ = strconv.Atoi(f[1])
	utime, _ := strconv.ParseUint(f[11], 10, 64)
	stime, _ := strconv.ParseUint(f[12], 10, 64)
	st.ticks = utime + stime
	st.start, _ = strconv.ParseUint(f[19], 10, 64)
	return pid, st, true
}

func parseProcStats(s string) map[int]procStat {
	stats := map[int]procStat{}
	sc := bufio.NewScanner(strings.NewReader(s))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		if pid, st, ok := parseProcStat(sc.Text()); ok {
			stats[pid] = st
		}
	}
	return stats
}

// parseExeLinks reads ls -l of /proc/<pid>/exe links: the target after
// " -> " of each line that has one, by the PID in its path.
func parseExeLinks(s string) map[int]string {
	exes := map[int]string{}
	sc := bufio.NewScanner(strings.NewReader(s))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		i := strings.Index(line, " /proc/")
		if i < 0 {
			continue
		}
		rest := line[i+len(" /proc/"):]
		num, after, ok := strings.Cut(rest, "/exe -> ")
		if !ok {
			continue
		}
		pid, err := strconv.Atoi(num)
		if err != nil || after == "" {
			continue
		}
		exes[pid] = after
	}
	return exes
}

// parseCPUTotal reads the cpu lines of /proc/stat: the jiffies of all
// CPUs, and how many there are.
func parseCPUTotal(s string) (total uint64, ncpu int) {
	for line := range strings.SplitSeq(s, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if f[0] == "cpu" {
			_, total = parseCPU(line)
		} else if strings.HasPrefix(f[0], "cpu") {
			ncpu++
		}
	}
	if ncpu == 0 && total > 0 {
		ncpu = 1
	}
	return total, ncpu
}

// EnvVar is a variable of a process's environment.
type EnvVar struct {
	Name, Value string
}

// ProcessDetails is what a process's details tab shows. What cannot be
// read, as the working directory, the executable and the environment of
// another user's process without root, is "" or nil with its Denied
// field set.
type ProcessDetails struct {
	PID   int
	Name  string
	User  string
	State byte
	Start uint64
	Args  []string // the command line, word by word; nil for a kernel thread
	Path  string
	Cwd   string
	Env   []EnvVar

	PathDenied, CwdDenied, EnvDenied bool
}

// detailsScript reads one process's details. Each part ends with a mark
// telling whether it could be read: cmdline and environ are NUL
// separated, which the channel carries as they are.
func detailsScript(pid int) string {
	p := strconv.Itoa(pid)
	return `export LC_ALL=C
m() { echo "` + procMark + `$1---"; }
x() { n=$1; shift; m "$n"; if "$@" 2>/dev/null; then echo; m "$n:ok"; else echo; m "$n:fail"; fi; }
m uname; uname -s
[ -d /proc/` + p + ` ] || { m gone; m end; exit 0; }
x stat cat /proc/` + p + `/stat
x owner ls -ld /proc/` + p + `
x cmdline cat /proc/` + p + `/cmdline
x exe readlink /proc/` + p + `/exe
x cwd readlink /proc/` + p + `/cwd
x environ cat /proc/` + p + `/environ
m end
`
}

// ReadProcessDetails reads pid's details over a new exec channel of
// client. A process that is not there is ErrNoProcess.
func ReadProcessDetails(ctx context.Context, client *ssh.Client, pid int) (ProcessDetails, error) {
	out, _, err := RunScript(ctx, client, detailsScript(pid))
	if ctx.Err() != nil {
		return ProcessDetails{}, ctx.Err()
	}
	if !strings.Contains(out, procMark+"end---") {
		if err != nil {
			return ProcessDetails{}, err
		}
		return ProcessDetails{}, ErrScriptOutput
	}
	return parseDetails(pid, out)
}

func parseDetails(pid int, out string) (ProcessDetails, error) {
	sec := procSections(out)
	if sys := strings.TrimSpace(sec["uname"]); sys != "" && sys != "Linux" {
		return ProcessDetails{}, ErrNotLinux
	}
	if _, gone := sec["gone"]; gone {
		return ProcessDetails{}, ErrNoProcess
	}
	read := func(name string) (string, bool) {
		_, ok := sec[name+":ok"]
		return sec[name], ok
	}
	d := ProcessDetails{PID: pid}
	stat, ok := read("stat")
	if !ok {
		return ProcessDetails{}, ErrNoProcess // gone between the test and the read
	}
	if _, st, ok := parseProcStat(strings.TrimSpace(stat)); ok {
		d.Name, d.State, d.Start = st.name, st.state, st.start
	}
	if owner, ok := read("owner"); ok {
		if f := strings.Fields(owner); len(f) >= 3 {
			d.User = f[2]
		}
	}
	if cmd, ok := read("cmdline"); ok {
		d.Args = splitNUL(cmd)
	}
	var exeOK, cwdOK, envOK bool
	d.Path, exeOK = read("exe")
	d.Cwd, cwdOK = read("cwd")
	env, envOK := read("environ")
	d.Path, d.Cwd = strings.TrimSpace(d.Path), strings.TrimSpace(d.Cwd)
	// A kernel thread has no executable and an empty environment, which
	// readlink fails on as it fails without the right: tell them apart
	// by its command line, empty too.
	kernel := len(d.Args) == 0
	d.PathDenied, d.CwdDenied = !exeOK && !kernel, !cwdOK && !kernel
	d.EnvDenied = !envOK
	for _, kv := range splitNUL(env) {
		name, value, _ := strings.Cut(kv, "=")
		d.Env = append(d.Env, EnvVar{Name: name, Value: value})
	}
	return d, nil
}

// splitNUL splits NUL-separated strings, as /proc's cmdline and environ,
// dropping the empty one after the last NUL.
func splitNUL(s string) []string {
	s = strings.TrimSuffix(s, "\x00")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\x00")
}

// Signals are the signals the process manager sends, by the name kill
// takes.
var Signals = []string{"TERM", "KILL", "HUP", "INT", "STOP", "CONT"}

// SignalError is a kill that failed: what the server said, as
// "Operation not permitted".
type SignalError struct {
	Msg string
}

func (e *SignalError) Error() string { return e.Msg }

// SendSignal sends sig, one of Signals, to pid with kill on the server.
// A kill that fails is a *SignalError with the server's message.
func SendSignal(ctx context.Context, client *ssh.Client, pid int, sig string) error {
	if !slices.Contains(Signals, sig) || pid <= 0 {
		return fmt.Errorf("bad signal %q to %d", sig, pid)
	}
	script := "export LC_ALL=C\nkill -" + sig + " " + strconv.Itoa(pid) + " 2>&1\n"
	out, stderr, err := RunScript(ctx, client, script)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var exit *ssh.ExitError
	if errors.As(err, &exit) {
		msg := strings.TrimSpace(out + stderr)
		if msg == "" {
			msg = fmt.Sprintf("kill exited with status %d", exit.ExitStatus())
		}
		return &SignalError{Msg: cleanKillMessage(msg)}
	}
	return err
}

// cleanKillMessage keeps the part of kill's message that says why, as
// shells put their name, the line and the PID before it: "sh: 1: kill:
// No such process" (dash), "bash: line 2: kill: (42) - Operation not
// permitted", "sh: can't kill pid 42: Operation not permitted"
// (BusyBox).
func cleanKillMessage(msg string) string {
	msg = strings.TrimSpace(strings.Split(msg, "\n")[0])
	if i := strings.LastIndex(msg, ": "); i >= 0 && i+2 < len(msg) {
		msg = msg[i+2:]
	}
	if rest, ok := strings.CutPrefix(msg, "("); ok {
		if _, why, ok := strings.Cut(rest, ") - "); ok {
			msg = why
		}
	}
	return msg
}

// ProcessRunning tells whether the process pid that started at start (0
// for any) is still there, and not a zombie waiting for its parent.
func ProcessRunning(ctx context.Context, client *ssh.Client, pid int, start uint64) (bool, error) {
	out, _, err := RunScript(ctx, client, "cat /proc/"+strconv.Itoa(pid)+"/stat 2>/dev/null; echo "+procMark+"end---\n")
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if !strings.Contains(out, procMark+"end---") {
		if err != nil {
			return false, err
		}
		return false, ErrScriptOutput
	}
	return statRunning(out, pid, start), nil
}

func statRunning(out string, pid int, start uint64) bool {
	got, st, ok := parseProcStat(strings.TrimSpace(strings.Split(out, procMark)[0]))
	return ok && got == pid && st.state != 'Z' && (start == 0 || st.start == start)
}
