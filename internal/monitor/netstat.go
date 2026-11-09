package monitor

import (
	"bufio"
	"cmp"
	"context"
	"encoding/hex"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// Socket is a TCP or UDP socket of the server, as ss or /proc/net tells
// of it.
type Socket struct {
	Proto  string         // "tcp" or "udp"
	Local  netip.AddrPort // IPv4-mapped addresses are given as IPv4
	Remote netip.AddrPort // the zero AddrPort for a listening or unconnected socket
	State  string         // as ss names it: LISTEN, ESTAB, TIME-WAIT, UNCONN, …
	// PID and Name are the process the socket is of: 0 and "" when it
	// cannot be read, as another user's needs root. A socket several
	// processes share (a server's workers) is the lowest PID's.
	PID  int
	Name string

	// Sent and Received are the bytes the connection moved, which only
	// ss has, for TCP (HasBytes): the bytes the peer acknowledged, and
	// those received.
	Sent, Received uint64
	HasBytes       bool
	// SendRate and RecvRate are the bytes per second since the last
	// read: 0 on the first, and for a connection new since.
	SendRate, RecvRate float64
}

// Listening says whether s waits for connections: a TCP socket in
// LISTEN, or an unconnected UDP socket.
func (s Socket) Listening() bool {
	return s.State == "LISTEN" || s.Proto == "udp" && !s.Remote.IsValid()
}

// NetGroup is a row of the Network tab: a listening socket and the
// connections to it, or a process and its other connections (those it
// opened), or, with PID 0, the connections of processes that cannot be
// read.
type NetGroup struct {
	Listen bool
	Proto  string         // "tcp" or "udp"; for a process, its connections' ("tcp", "udp" or "tcp+udp")
	Local  netip.AddrPort // where it listens (Listen)
	PID    int
	Name   string
	Conns  []Socket

	RemoteIPs          int // different remote addresses of Conns
	Sent, Received     uint64
	SendRate, RecvRate float64
	HasBytes           bool // Conns have traffic counts (ss, TCP)
}

// Key tells a row from the others, the same from one read to the next.
func (g NetGroup) Key() string {
	if g.Listen {
		return g.Proto + " " + g.Local.String() + " " + strconv.Itoa(g.PID)
	}
	return "pid " + strconv.Itoa(g.PID)
}

// Title names the row in its tab: "0.0.0.0:110", "[::]:22" or
// "7267-java".
func (g NetGroup) Title() string {
	if g.Listen {
		return g.Local.String()
	}
	if g.PID == 0 {
		return "-"
	}
	return strconv.Itoa(g.PID) + "-" + g.Name
}

// NetSnapshot is one read of the server's sockets.
type NetSnapshot struct {
	Groups []NetGroup
	// Sockets is how many sockets there are; Unknown how many of them
	// have no process, as reading it needs root.
	Sockets, Unknown int
	Root             bool // read as root, so every process is known
	FromProc         bool // read from /proc/net, without ss: no traffic counts
}

// netMark starts each section of the network script's output.
const netMark = "---FINALLYSHELL:"

// ssBytes keeps of ss's TCP information only the byte counts, so that a
// server with thousands of connections answers in a few hundred
// kilobytes; without awk the lines are sent whole.
const ssBytes = `awk '/^[ \t]/ { o = ""; for (i = 1; i <= NF; i++) if ($i ~ /^bytes_(acked|received|sent):/) o = o " " $i; print "\t" o; next } { print }'`

// netScript lists the sockets in one round trip: with ss when the
// server has it (iproute2), else from /proc/net and the socket links in
// /proc/<pid>/fd, which BusyBox systems have. useSS false is the second
// alone, for tests.
func netScript(useSS bool) string {
	ss := "false"
	if useSS {
		ss = "command -v ss >/dev/null 2>&1 && s=$(ss -tunapiH 2>/dev/null || ss -tunapi 2>/dev/null) && [ -n \"$s\" ]"
	}
	return `export LC_ALL=C
m() { echo "` + netMark + `$1---"; }
m uname; uname -s
m uid; id -u 2>/dev/null
if ` + ss + `; then
  m ss
  if command -v awk >/dev/null 2>&1; then printf '%s\n' "$s" | ` + ssBytes + `; else printf '%s\n' "$s"; fi
else
  m tcp; cat /proc/net/tcp 2>/dev/null
  m tcp6; cat /proc/net/tcp6 2>/dev/null
  m udp; cat /proc/net/udp 2>/dev/null
  m udp6; cat /proc/net/udp6 2>/dev/null
  m fd; ls -l /proc/[0-9]*/fd/ 2>/dev/null | grep -E ':$|socket:'
  m stat; cat /proc/[0-9]*/stat 2>/dev/null
fi
m end
`
}

// NetLister reads the server's sockets, again and again: it keeps the
// byte counts of each connection between reads, for their rates. Its
// zero value is ready; it is not safe for concurrent use.
type NetLister struct {
	prev map[sockKey][2]uint64
	at   time.Time
	// NoSS reads /proc/net even when the server has ss, for tests.
	NoSS bool
}

type sockKey struct {
	proto         string
	local, remote netip.AddrPort
}

// List runs the script once over a new exec channel of client.
func (l *NetLister) List(ctx context.Context, client *ssh.Client) (NetSnapshot, error) {
	out, _, err := RunScript(ctx, client, netScript(!l.NoSS))
	if ctx.Err() != nil {
		return NetSnapshot{}, ctx.Err()
	}
	if !strings.Contains(out, netMark+"end---") {
		if err != nil {
			return NetSnapshot{}, err
		}
		return NetSnapshot{}, ErrScriptOutput
	}
	return l.parse(out, time.Now())
}

// parse reads the script's output, and the rates against the last
// parse, at now.
func (l *NetLister) parse(out string, now time.Time) (NetSnapshot, error) {
	sec := procSections(out)
	if sys := strings.TrimSpace(sec["uname"]); sys != "" && sys != "Linux" {
		return NetSnapshot{}, ErrNotLinux
	}
	var snap NetSnapshot
	snap.Root = strings.TrimSpace(sec["uid"]) == "0"
	var socks []Socket
	if s, ok := sec["ss"]; ok {
		socks = parseSS(s)
	} else {
		snap.FromProc = true
		owners := parseSocketLinks(sec["fd"])
		stats := parseProcStats(sec["stat"])
		for _, name := range []string{"tcp", "tcp6", "udp", "udp6"} {
			for _, ps := range parseProcNet(sec[name], strings.TrimSuffix(name, "6")) {
				s := ps.Socket
				if pid, ok := owners[ps.inode]; ok && ps.inode != 0 {
					s.PID = pid
					s.Name = stats[pid].name
				}
				socks = append(socks, s)
			}
		}
	}

	// Rates, from the counts of the last read.
	elapsed := now.Sub(l.at).Seconds()
	prev := l.prev
	l.prev = make(map[sockKey][2]uint64, len(socks))
	for i := range socks {
		s := &socks[i]
		if !s.HasBytes {
			continue
		}
		k := sockKey{s.Proto, s.Local, s.Remote}
		l.prev[k] = [2]uint64{s.Sent, s.Received}
		if p, ok := prev[k]; ok && elapsed > 0 {
			if s.Sent >= p[0] {
				s.SendRate = float64(s.Sent-p[0]) / elapsed
			}
			if s.Received >= p[1] {
				s.RecvRate = float64(s.Received-p[1]) / elapsed
			}
		}
	}
	l.at = now

	snap.Sockets = len(socks)
	for _, s := range socks {
		// A connection waiting to be closed has no process any more:
		// that is not what needs root.
		if s.PID == 0 && s.State != "TIME-WAIT" {
			snap.Unknown++
		}
	}
	snap.Groups = GroupSockets(socks)
	return snap, nil
}

// GroupSockets makes the rows of socks: each listening socket with the
// connections to its address and port, then each process with the rest
// of its connections, sorted by port and PID.
func GroupSockets(socks []Socket) []NetGroup {
	var groups []NetGroup
	byPort := map[string][]int{} // proto + port: the listeners
	for _, s := range socks {
		if s.Listening() {
			groups = append(groups, NetGroup{Listen: true, Proto: s.Proto, Local: s.Local, PID: s.PID, Name: s.Name})
			k := s.Proto + strconv.Itoa(int(s.Local.Port()))
			byPort[k] = append(byPort[k], len(groups)-1)
		}
	}
	// The listener a connection came through: the one on its local
	// address, else one on any address of its family, else of the other
	// (a dual-stack [::] takes IPv4 too).
	listener := func(s Socket) int {
		if s.Proto != "tcp" {
			return -1
		}
		cands := byPort[s.Proto+strconv.Itoa(int(s.Local.Port()))]
		best, rank := -1, 0
		for _, i := range cands {
			g := groups[i].Local.Addr()
			r := 0
			switch {
			case g == s.Local.Addr():
				r = 3
			case g.IsUnspecified() && g.Is4() == s.Local.Addr().Is4():
				r = 2
			case g.IsUnspecified():
				r = 1
			}
			if r > rank {
				best, rank = i, r
			}
		}
		return best
	}
	procs := map[int]int{} // PID: its group
	for _, s := range socks {
		if s.Listening() {
			continue
		}
		i := listener(s)
		if i < 0 {
			var ok bool
			if i, ok = procs[s.PID]; !ok {
				groups = append(groups, NetGroup{Proto: s.Proto, PID: s.PID, Name: s.Name})
				i = len(groups) - 1
				procs[s.PID] = i
			}
		}
		g := &groups[i]
		if !g.Listen && g.Proto != s.Proto {
			g.Proto = "tcp+udp"
		}
		g.Conns = append(g.Conns, s)
	}
	for i := range groups {
		g := &groups[i]
		ips := map[netip.Addr]bool{}
		for _, c := range g.Conns {
			ips[c.Remote.Addr()] = true
			g.Sent += c.Sent
			g.Received += c.Received
			g.SendRate += c.SendRate
			g.RecvRate += c.RecvRate
			g.HasBytes = g.HasBytes || c.HasBytes
		}
		g.RemoteIPs = len(ips)
	}
	slices.SortStableFunc(groups, func(a, b NetGroup) int {
		if a.Listen != b.Listen {
			if a.Listen {
				return -1
			}
			return 1
		}
		if a.Listen {
			return cmp.Or(cmp.Compare(a.Local.Port(), b.Local.Port()), cmp.Compare(a.Proto, b.Proto),
				a.Local.Addr().Compare(b.Local.Addr()), cmp.Compare(a.PID, b.PID))
		}
		// The processes that cannot be read last.
		if (a.PID == 0) != (b.PID == 0) {
			return cmp.Compare(b.PID, a.PID)
		}
		return cmp.Compare(a.PID, b.PID)
	})
	return groups
}

// ssUser is a process of ss's users:((…)): its name and PID.
var ssUser = regexp.MustCompile(`\("((?:[^"\\]|\\.)*)",pid=(\d+),`)

// parseSS reads the lines of ss -tunapi: a socket on each line, with its
// TCP information on the indented line after it. A header line, as ss
// prints without -H, is skipped.
func parseSS(out string) []Socket {
	var socks []Socket
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			if n := len(socks); n > 0 {
				parseSSInfo(&socks[n-1], line)
			}
			continue
		}
		f := strings.Fields(line)
		if len(f) < 6 || f[0] != "tcp" && f[0] != "udp" {
			continue
		}
		local, ok1 := parseSSAddr(f[4])
		remote, ok2 := parseSSAddr(f[5])
		if !ok1 || !ok2 {
			continue
		}
		s := Socket{Proto: f[0], State: f[1], Local: local}
		if remote.Port() != 0 || !remote.Addr().IsUnspecified() {
			s.Remote = remote
		}
		if i := strings.Index(line, "users:(("); i >= 0 {
			for _, m := range ssUser.FindAllStringSubmatch(line[i:], -1) {
				pid, _ := strconv.Atoi(m[2])
				if pid > 0 && (s.PID == 0 || pid < s.PID) {
					s.PID, s.Name = pid, strings.ReplaceAll(m[1], `\"`, `"`)
				}
			}
		}
		socks = append(socks, s)
	}
	return socks
}

// parseSSInfo reads the byte counts of a socket's TCP information:
// bytes_acked (or bytes_sent, before Linux 4.1 has acked) and
// bytes_received, each missing while it is 0.
func parseSSInfo(s *Socket, line string) {
	if s.Proto != "tcp" || s.State == "LISTEN" {
		return
	}
	var acked, sent uint64
	var hasAcked bool
	for _, f := range strings.Fields(line) {
		name, v, ok := strings.Cut(f, ":")
		if !ok {
			continue
		}
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			continue
		}
		switch name {
		case "bytes_acked":
			acked, hasAcked = n, true
		case "bytes_sent":
			sent = n
		case "bytes_received":
			s.Received = n
		}
	}
	s.Sent = sent
	if hasAcked {
		// The SYN and the FIN count as a byte each.
		s.Sent = acked
		if acked > 0 {
			s.Sent = acked - 1
		}
	}
	s.HasBytes = true
}

// parseSSAddr reads an address of ss: 127.0.0.53%lo:53, [::]:22,
// [::ffff:1.2.3.4]:443, *:22, 0.0.0.0:*, or, from an older ss, :::22.
func parseSSAddr(s string) (netip.AddrPort, bool) {
	i := strings.LastIndexByte(s, ':')
	if i < 0 {
		return netip.AddrPort{}, false
	}
	host, port := s[:i], s[i+1:]
	var p uint64
	if port != "*" {
		var err error
		if p, err = strconv.ParseUint(port, 10, 16); err != nil {
			return netip.AddrPort{}, false
		}
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if j := strings.IndexByte(host, '%'); j >= 0 {
		host = host[:j]
	}
	var addr netip.Addr
	if host == "*" || host == "" {
		addr = netip.IPv6Unspecified()
	} else {
		var err error
		if addr, err = netip.ParseAddr(host); err != nil {
			return netip.AddrPort{}, false
		}
	}
	return netip.AddrPortFrom(addr.Unmap(), uint16(p)), true
}

// procSocket is a socket of /proc/net, with its inode, which tells its
// process.
type procSocket struct {
	Socket
	inode uint64
}

// procStates are the TCP states of /proc/net/tcp, by their numbers, as
// ss names them.
var procStates = map[string]string{
	"01": "ESTAB", "02": "SYN-SENT", "03": "SYN-RECV", "04": "FIN-WAIT-1", "05": "FIN-WAIT-2",
	"06": "TIME-WAIT", "07": "CLOSE", "08": "CLOSE-WAIT", "09": "LAST-ACK", "0A": "LISTEN", "0B": "CLOSING",
}

// parseProcNet reads /proc/net/tcp, tcp6, udp or udp6.
func parseProcNet(out, proto string) []procSocket {
	var socks []procSocket
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 10 || !strings.HasSuffix(f[0], ":") {
			continue
		}
		local, ok1 := parseProcAddr(f[1])
		remote, ok2 := parseProcAddr(f[2])
		if !ok1 || !ok2 {
			continue
		}
		s := Socket{Proto: proto, Local: local}
		if remote.Port() != 0 || !remote.Addr().IsUnspecified() {
			s.Remote = remote
		}
		st := strings.ToUpper(f[3])
		if proto == "udp" {
			// UDP has 07 (close) unless it is connected (01).
			s.State = "UNCONN"
			if st == "01" {
				s.State = "ESTAB"
			}
		} else if s.State = procStates[st]; s.State == "" {
			s.State = st
		}
		inode, _ := strconv.ParseUint(f[9], 10, 64)
		socks = append(socks, procSocket{Socket: s, inode: inode})
	}
	return socks
}

// parseProcAddr reads an address of /proc/net: the IP in hex, in 32-bit
// words of the host's byte order (little-endian on the servers this
// reads), a colon, and the port in hex.
func parseProcAddr(s string) (netip.AddrPort, bool) {
	h, p, ok := strings.Cut(s, ":")
	if !ok {
		return netip.AddrPort{}, false
	}
	port, err := strconv.ParseUint(p, 16, 16)
	if err != nil {
		return netip.AddrPort{}, false
	}
	b, err := hex.DecodeString(h)
	if err != nil || len(b) != 4 && len(b) != 16 {
		return netip.AddrPort{}, false
	}
	for i := 0; i < len(b); i += 4 {
		b[i], b[i+1], b[i+2], b[i+3] = b[i+3], b[i+2], b[i+1], b[i]
	}
	addr, _ := netip.AddrFromSlice(b)
	return netip.AddrPortFrom(addr.Unmap(), uint16(port)), true
}

// parseSocketLinks reads ls -l of the /proc/<pid>/fd folders, kept to
// their headers and their socket links: the process of each socket
// inode, the lowest PID of those that share it.
func parseSocketLinks(out string) map[uint64]int {
	owners := map[uint64]int{}
	pid := 0
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "/proc/") && strings.HasSuffix(line, ":") {
			num, _, _ := strings.Cut(strings.TrimPrefix(line, "/proc/"), "/")
			pid, _ = strconv.Atoi(num)
			continue
		}
		i := strings.Index(line, "socket:[")
		if i < 0 || pid <= 0 {
			continue
		}
		num, _, _ := strings.Cut(line[i+len("socket:["):], "]")
		inode, err := strconv.ParseUint(num, 10, 64)
		if err != nil {
			continue
		}
		if p, ok := owners[inode]; !ok || pid < p {
			owners[inode] = pid
		}
	}
	return owners
}
