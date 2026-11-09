package monitor

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// TraceHop is a hop of a route: the router that answered at a TTL.
type TraceHop struct {
	TTL  int
	IP   string // "" when nothing answered
	Host string // its name, which NextTrace looks up
	RTTs []time.Duration
	// ASN and Where are what NextTrace says of the router, from its own
	// service: the AS number, and the country, region, city and owner,
	// in the language asked.
	ASN   string
	Where []string
}

// Trace tools: what ran on the server.
const (
	ToolNextTrace  = "nexttrace"
	ToolTraceroute = "traceroute"
	ToolTracepath  = "tracepath"
)

// TraceResult is a route as it is traced.
type TraceResult struct {
	Tool string // ToolNextTrace, ToolTraceroute or ToolTracepath; "" before it is known
	// NoNextTrace says that NextTrace is not on the server's PATH, so the
	// route comes from Tool, if any.
	NoNextTrace bool
	Hops        []TraceHop
	Done        bool
	Err         error // why it failed, when Done
}

// ErrNoTraceTool is a server with none of NextTrace, traceroute and
// tracepath.
var ErrNoTraceTool = errors.New("no trace tool on the server")

// TraceError is a trace tool that failed, with the last line it wrote
// on stderr, such as needing root.
type TraceError struct{ Msg string }

func (e *TraceError) Error() string { return e.Msg }

// NextTraceInstall is the command NextTrace's README gives to install
// it on Linux: the app shows it, and never runs it.
const NextTraceInstall = "curl -sL https://nxtrace.org/nt | bash"

// ValidTraceTarget says whether s can be traced: an IP address without
// a zone (which may hold any character), or a host name of letters,
// digits, dots and hyphens.
func ValidTraceTarget(s string) bool {
	if a, err := netip.ParseAddr(s); err == nil {
		return a.Zone() == ""
	}
	if s == "" || len(s) > 253 || s[0] == '-' || s[0] == '.' {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-') {
			return false
		}
	}
	return true
}

// traceScript runs the first tool there is on target: NextTrace, with
// its RAW output (a line per probe, as it goes), its locations in lang
// ("en" or "cn") and without its map (which it would upload), else
// traceroute or tracepath.
//
// NextTrace is kept to its traditional mode with --traceroute when its
// --help has the flag: its README says its default mode becomes MTR no
// earlier than 2027, and bare --raw then an endless MTR stream. The
// versions before --traceroute don't know it, and are traditional with
// bare --raw.
//
// The sbin folders are on the PATH, where traceroute often is. Each
// tool's stdin is /dev/null, as the script is the shell's.
func traceScript(target, lang string) string {
	// ValidTraceTarget leaves nothing to escape; quoted all the same.
	q := "'" + strings.ReplaceAll(target, "'", `'\''`) + "'"
	if lang != "cn" {
		lang = "en"
	}
	nt := `--raw -M -g ` + lang + ` ` + q + ` </dev/null 2>&1`
	return `export LC_ALL=C PATH="$PATH:/usr/local/bin:/usr/sbin:/sbin"
m() { echo "` + netMark + `$1---"; }
if command -v nexttrace >/dev/null 2>&1; then
	m nexttrace
	if nexttrace --help </dev/null 2>&1 | grep -q -e --traceroute; then exec nexttrace --traceroute ` + nt + `; fi
	exec nexttrace ` + nt + `
fi
m no-nexttrace
if command -v traceroute >/dev/null 2>&1; then m traceroute; exec traceroute -n -w 2 ` + q + ` </dev/null; fi
if command -v tracepath >/dev/null 2>&1; then m tracepath; exec tracepath -n ` + q + ` </dev/null; fi
m none
`
}

// traceTimeout is how long a trace may take: traceroute waits 2 seconds
// for each of 3 probes of 30 hops at worst.
const traceTimeout = 4 * time.Minute

// Trace traces the route from the server to target over an exec channel
// of client, calling update with the route each time a hop arrives and
// with the end. lang is the language of NextTrace's locations: "en" or
// "cn".
func Trace(ctx context.Context, client *ssh.Client, target, lang string, update func(TraceResult)) TraceResult {
	if !ValidTraceTarget(target) {
		r := TraceResult{Done: true, Err: &TraceError{Msg: "invalid address: " + target}}
		update(r)
		return r
	}
	return runTrace(ctx, client, traceScript(target, lang), update)
}

// runTrace runs a trace script, reading the lines of the tool it runs.
func runTrace(ctx context.Context, client *ssh.Client, script string, update func(TraceResult)) TraceResult {
	var r TraceResult
	finish := func(err error) TraceResult {
		r.Done, r.Err = true, err
		update(r)
		return r
	}
	if client == nil {
		return finish(errors.New("not connected"))
	}
	ctx, cancel := context.WithTimeout(ctx, traceTimeout)
	defer cancel()
	none := false
	var last string // the last line a tool wrote
	stderr, err := runLines(ctx, client, script, func(line string) {
		if name, ok := strings.CutPrefix(line, netMark); ok && strings.HasSuffix(name, "---") {
			switch strings.TrimSuffix(name, "---") {
			case "nexttrace":
				r.Tool = ToolNextTrace
			case "no-nexttrace":
				r.NoNextTrace = true
			case "traceroute":
				r.Tool = ToolTraceroute
			case "tracepath":
				r.Tool = ToolTracepath
			case "none":
				none = true
			}
			update(r)
			return
		}
		if strings.TrimSpace(line) != "" {
			last = strings.TrimSpace(line)
		}
		var changed bool
		switch r.Tool {
		case ToolNextTrace:
			r.Hops, changed = addNextTraceLine(r.Hops, line)
		case ToolTraceroute:
			r.Hops, changed = addTracerouteLine(r.Hops, line)
		case ToolTracepath:
			r.Hops, changed = addTracepathLine(r.Hops, line)
		}
		if changed {
			update(r)
		}
	})
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return finish(context.DeadlineExceeded)
		}
		return finish(ctx.Err())
	}
	if none {
		return finish(ErrNoTraceTool)
	}
	if r.Tool == ToolNextTrace && len(r.Hops) == 0 {
		// NextTrace writes its errors on stdout too (the script sends
		// its stderr there), and at least a hop when it works.
		if last == "" {
			if err == nil {
				err = errors.New("NextTrace wrote no hops")
			}
			last = err.Error()
		}
		return finish(&TraceError{Msg: last})
	}
	if err != nil && len(r.Hops) == 0 {
		msg := lastLine(stderr)
		if msg == "" {
			if r.Tool == "" {
				return finish(err)
			}
			msg = err.Error()
		}
		return finish(&TraceError{Msg: msg})
	}
	return finish(nil)
}

func lastLine(s string) string {
	ls := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(ls[len(ls)-1])
}

// runLines runs a script as RunScript does, giving each line of its
// stdout to line as it arrives.
func runLines(ctx context.Context, client *ssh.Client, script string, line func(string)) (stderr string, err error) {
	s, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer s.Close()
	stop := context.AfterFunc(ctx, func() { s.Close() })
	defer stop()
	s.Stdin = strings.NewReader(script)
	var errBuf bytes.Buffer
	s.Stderr = &limitWriter{w: &errBuf, n: 64 << 10}
	out, err := s.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := s.Start("sh -s"); err != nil {
		return "", err
	}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line(sc.Text())
	}
	werr := s.Wait()
	return errBuf.String(), werr
}

// ParseNextTrace reads the output of nexttrace --traceroute --raw, each
// line as addNextTraceLine does.
func ParseNextTrace(out string) []TraceHop {
	var hops []TraceHop
	for _, line := range strings.Split(out, "\n") {
		hops, _ = addNextTraceLine(hops, line)
	}
	return hops
}

// addNextTraceLine reads a line of nexttrace --traceroute --raw (its
// EasyPrinter), a probe: its TTL, the router that answered, its name,
// the round trip in milliseconds, its AS, where it is (country, region,
// city, district and owner, from NextTrace's own service in the language
// asked; in English NextTrace puts the city first), its latitude and
// longitude.
//
//	3|103.21.244.1|ae1.mel01.example.net|11.20|13335|澳大利亚|维多利亚州|墨尔本||Cloudflare|-37.8136|144.9631
//
// A probe that nothing answered is "3|*||||||", or the same in 12
// columns, as NextTrace's MTR RAW writes it. A TTL has a line per probe;
// its hop is the first router that answered, with the round trips of
// that router's probes, as for traceroute. Other lines, such as the
// header, are skipped.
func addNextTraceLine(hops []TraceHop, line string) ([]TraceHop, bool) {
	f := strings.Split(strings.TrimSpace(line), "|")
	if len(f) < 8 {
		return hops, false
	}
	ttl, err := strconv.Atoi(f[0])
	if err != nil || ttl <= 0 {
		return hops, false
	}
	ip := f[1]
	if ip == "*" {
		hops, _ = hop(hops, ttl)
		return hops, true
	}
	if len(f) < 12 || !isIP(ip) {
		return hops, false
	}
	hops, h := hop(hops, ttl)
	if h.IP == "" {
		h.IP = ip
		h.Host = strings.TrimSuffix(strings.TrimSpace(f[2]), ".")
		h.ASN = strings.TrimSpace(f[4])
		for _, w := range f[5:10] {
			if w = strings.TrimSpace(w); w != "" && !slices.Contains(h.Where, w) {
				h.Where = append(h.Where, w)
			}
		}
	}
	if ip == h.IP {
		if ms, err := strconv.ParseFloat(f[3], 64); err == nil && ms >= 0 {
			h.RTTs = append(h.RTTs, time.Duration(ms*float64(time.Millisecond)))
		}
	}
	return hops, true
}

// hop is the hop of ttl in hops, added in order when it is new.
func hop(hops []TraceHop, ttl int) ([]TraceHop, *TraceHop) {
	i, found := slices.BinarySearchFunc(hops, ttl, func(h TraceHop, ttl int) int { return h.TTL - ttl })
	if !found {
		hops = slices.Insert(hops, i, TraceHop{TTL: ttl})
	}
	return hops, &hops[i]
}

func isIP(s string) bool { return net.ParseIP(s) != nil }

// addTracerouteLine reads a line of traceroute -n (also BusyBox's): the
// TTL, then the routers that answered and the times of their probes,
// "*" for those that did not.
//
//	3  10.0.0.1  1.214 ms 10.0.0.2  1.302 ms  1.118 ms
func addTracerouteLine(hops []TraceHop, line string) ([]TraceHop, bool) {
	f := strings.Fields(line)
	if len(f) < 2 {
		return hops, false
	}
	ttl, err := strconv.Atoi(f[0])
	if err != nil || ttl <= 0 {
		return hops, false
	}
	hops, h := hop(hops, ttl)
	cur := ""
	for i := 1; i < len(f); i++ {
		switch {
		case isIP(f[i]):
			cur = f[i]
			if h.IP == "" {
				h.IP = cur
			}
		case i+1 < len(f) && f[i+1] == "ms" && cur == h.IP:
			if ms, err := strconv.ParseFloat(f[i], 64); err == nil {
				h.RTTs = append(h.RTTs, time.Duration(ms*float64(time.Millisecond)))
			}
		}
	}
	return hops, true
}

// addTracepathLine reads a line of tracepath -n: a TTL with ":" (or "?:"
// for the first, local, line), the router or "no reply", and the time.
//
//	3:  10.1.1.1                                              5.123ms asymm  4
func addTracepathLine(hops []TraceHop, line string) ([]TraceHop, bool) {
	f := strings.Fields(line)
	if len(f) < 2 {
		return hops, false
	}
	num, ok := strings.CutSuffix(f[0], ":")
	if !ok {
		return hops, false
	}
	num = strings.TrimSuffix(num, "?")
	ttl, err := strconv.Atoi(num)
	if err != nil || ttl <= 0 || f[1] == "[LOCALHOST]" {
		return hops, false
	}
	hops, h := hop(hops, ttl)
	if !isIP(f[1]) {
		return hops, true // no reply
	}
	if h.IP == "" {
		h.IP = f[1]
	}
	if len(f) > 2 && f[1] == h.IP {
		if ms, ok := strings.CutSuffix(f[2], "ms"); ok {
			if v, err := strconv.ParseFloat(ms, 64); err == nil {
				h.RTTs = append(h.RTTs, time.Duration(v*float64(time.Millisecond)))
			}
		}
	}
	return hops, true
}
