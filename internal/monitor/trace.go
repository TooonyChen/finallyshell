package monitor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
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

// ValidTraceTarget says whether s can be traced: an IP address, or a
// host name of letters, digits, dots and hyphens.
func ValidTraceTarget(s string) bool {
	if _, err := netip.ParseAddr(s); err == nil {
		return true
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
// its JSON output and without its map (which it would upload), else
// traceroute or tracepath. The sbin folders are on the PATH, where
// traceroute often is. Each tool's stdin is /dev/null, as the script is
// the shell's.
func traceScript(target string) string {
	q := "'" + target + "'" // ValidTraceTarget: nothing to escape
	return `export LC_ALL=C PATH="$PATH:/usr/local/bin:/usr/sbin:/sbin"
m() { echo "` + netMark + `$1---"; }
if command -v nexttrace >/dev/null 2>&1; then m nexttrace; exec nexttrace --json -M ` + q + ` </dev/null 2>&1; fi
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
// of client, calling update with the route each time a hop arrives
// (with NextTrace, whose JSON comes whole, once at the end) and with the
// end. lang is the language of NextTrace's locations: "en" or "cn".
func Trace(ctx context.Context, client *ssh.Client, target, lang string, update func(TraceResult)) TraceResult {
	var r TraceResult
	finish := func(err error) TraceResult {
		r.Done, r.Err = true, err
		update(r)
		return r
	}
	if !ValidTraceTarget(target) {
		return finish(&TraceError{Msg: "invalid address: " + target})
	}
	if client == nil {
		return finish(errors.New("not connected"))
	}
	ctx, cancel := context.WithTimeout(ctx, traceTimeout)
	defer cancel()
	var nt bytes.Buffer // NextTrace's output
	none := false
	var last string // the last line a tool wrote
	stderr, err := runLines(ctx, client, traceScript(target), func(line string) {
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
		switch r.Tool {
		case ToolNextTrace:
			nt.WriteString(line)
			nt.WriteByte('\n')
		case ToolTraceroute, ToolTracepath:
			var changed bool
			if r.Tool == ToolTraceroute {
				r.Hops, changed = addTracerouteLine(r.Hops, line)
			} else {
				r.Hops, changed = addTracepathLine(r.Hops, line)
			}
			if changed {
				update(r)
			}
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
	if r.Tool == ToolNextTrace {
		hops, perr := ParseNextTrace(nt.Bytes(), lang)
		if perr == nil {
			r.Hops = hops
			return finish(nil)
		}
		if last == "" {
			last = perr.Error()
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
	sc.Buffer(make([]byte, 64<<10), 16<<20) // NextTrace's JSON is one line
	for sc.Scan() {
		line(sc.Text())
	}
	werr := s.Wait()
	return errBuf.String(), werr
}

// nextTraceHop is a probe of NextTrace's --json output: its Go struct
// as encoding/json writes it (RTT in nanoseconds).
type nextTraceHop struct {
	Success bool
	Address *struct {
		IP string
	}
	Hostname string
	TTL      int
	RTT      int64
	Geo      *struct {
		Asnumber  string `json:"asnumber"`
		Country   string `json:"country"`
		CountryEn string `json:"country_en"`
		Prov      string `json:"prov"`
		ProvEn    string `json:"prov_en"`
		City      string `json:"city"`
		CityEn    string `json:"city_en"`
		District  string `json:"district"`
		Owner     string `json:"owner"`
		Isp       string `json:"isp"`
	}
}

// ParseNextTrace reads the output of nexttrace --json: a JSON document
// whose Hops are, for each TTL, its probes. Lines before the document,
// as warnings, are skipped. Each hop is the first router that answered,
// with the round trips of the probes it answered.
func ParseNextTrace(out []byte, lang string) ([]TraceHop, error) {
	i := bytes.Index(out, []byte(`{"Hops"`))
	if i < 0 {
		i = bytes.IndexByte(out, '{')
	}
	if i < 0 {
		return nil, errors.New("no JSON in NextTrace's output")
	}
	var doc struct {
		Hops [][]nextTraceHop
	}
	if err := json.NewDecoder(bytes.NewReader(out[i:])).Decode(&doc); err != nil {
		return nil, err
	}
	var hops []TraceHop
	for n, probes := range doc.Hops {
		h := TraceHop{TTL: n + 1}
		for _, p := range probes {
			if p.TTL > 0 {
				h.TTL = p.TTL
			}
			if p.Address == nil || p.Address.IP == "" {
				continue
			}
			if h.IP == "" {
				h.IP = p.Address.IP
				h.Host = strings.TrimSuffix(p.Hostname, ".")
				if g := p.Geo; g != nil {
					h.ASN = g.Asnumber
					pick := func(cn, en string) string {
						if lang == "en" && en != "" {
							return en
						}
						return cn
					}
					owner := g.Owner
					if owner == "" {
						owner = g.Isp
					}
					for _, w := range []string{pick(g.Country, g.CountryEn), pick(g.Prov, g.ProvEn), pick(g.City, g.CityEn), g.District, owner} {
						if w = strings.TrimSpace(w); w != "" && !slices.Contains(h.Where, w) {
							h.Where = append(h.Where, w)
						}
					}
				}
			}
			if p.IP() == h.IP && p.Success && p.RTT > 0 {
				h.RTTs = append(h.RTTs, time.Duration(p.RTT))
			}
		}
		hops = append(hops, h)
	}
	return hops, nil
}

func (p nextTraceHop) IP() string {
	if p.Address == nil {
		return ""
	}
	return p.Address.IP
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
