package monitor

import (
	"net/netip"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// group is the row of snap whose title is title.
func group(t *testing.T, snap NetSnapshot, proto, title string) NetGroup {
	t.Helper()
	for _, g := range snap.Groups {
		if g.Title() == title && (proto == "" || g.Proto == proto) {
			return g
		}
	}
	var titles []string
	for _, g := range snap.Groups {
		titles = append(titles, g.Proto+" "+g.Title())
	}
	t.Fatalf("no row %s %s in %v", proto, title, titles)
	return NetGroup{}
}

func TestNetSS(t *testing.T) {
	var l NetLister
	t0 := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	out := readFixture(t, "net-ss.txt")
	snap, err := l.parse(out, t0)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Root || snap.FromProc || snap.Sockets != 20 || snap.Unknown != 1 {
		t.Errorf("root %v, proc %v, %d sockets, %d unknown", snap.Root, snap.FromProc, snap.Sockets, snap.Unknown)
	}
	var titles []string
	for _, g := range snap.Groups {
		titles = append(titles, g.Proto+" "+g.Title())
	}
	want := []string{
		"tcp 0.0.0.0:22", "tcp [::]:22", "tcp 127.0.0.53:53", "udp 127.0.0.53:53", "udp 0.0.0.0:68", "tcp 0.0.0.0:80",
		"tcp 0.0.0.0:110", "udp [::]:123", "tcp [::]:8080",
		"tcp+udp 7267-java", "tcp 9100-curl \"x\"", "tcp -",
	}
	if !slices.Equal(titles, want) {
		t.Errorf("rows\n%q\nwant\n%q", titles, want)
	}

	// IPv4 to the IPv4 listener, IPv6 to the IPv6 one, and the shared
	// socket is the lowest PID's.
	ssh := group(t, snap, "tcp", "0.0.0.0:22")
	if ssh.PID != 1290 || ssh.Name != "sshd" || len(ssh.Conns) != 2 || ssh.RemoteIPs != 1 || !ssh.HasBytes {
		t.Errorf("sshd: %+v", ssh)
	}
	if c := ssh.Conns[0]; c.PID != 4001 || c.Remote != netip.MustParseAddrPort("45.56.78.90:51234") || c.State != "ESTAB" || c.Sent != 1234567 || c.Received != 56789 {
		t.Errorf("sshd's connection: %+v", c)
	}
	if ssh.Sent != 1234567+2000 || ssh.Received != 56789+3001 {
		t.Errorf("sshd's totals %d %d", ssh.Sent, ssh.Received)
	}
	ssh6 := group(t, snap, "tcp", "[::]:22")
	if len(ssh6.Conns) != 1 || ssh6.Conns[0].Remote.Addr() != netip.MustParseAddr("2606:4700::1111") {
		t.Errorf("sshd on IPv6: %+v", ssh6)
	}
	// An IPv4-mapped connection goes to the dual-stack listener, as
	// IPv4.
	java := group(t, snap, "tcp", "[::]:8080")
	if len(java.Conns) != 1 || java.Conns[0].Remote != netip.MustParseAddrPort("203.0.113.9:55001") || java.Conns[0].Sent != 9000 {
		t.Errorf("java's listener: %+v", java)
	}
	nginx := group(t, snap, "tcp", "0.0.0.0:80")
	if nginx.PID != 1200 || len(nginx.Conns) != 1 || nginx.Conns[0].State != "TIME-WAIT" || nginx.Conns[0].PID != 0 || nginx.HasBytes {
		t.Errorf("nginx: %+v", nginx)
	}
	// java's outgoing connections, TCP and UDP.
	out7267 := group(t, snap, "", "7267-java")
	if len(out7267.Conns) != 3 || out7267.RemoteIPs != 2 || out7267.Sent != 209730+100 {
		t.Errorf("java's connections: %+v", out7267)
	}
	if u := group(t, snap, "udp", "[::]:123"); u.PID != 700 || len(u.Conns) != 0 || !u.Listen {
		t.Errorf("chronyd: %+v", u)
	}
	if g := group(t, snap, "", "-"); g.PID != 0 || len(g.Conns) != 1 {
		t.Errorf("unknown: %+v", g)
	}
	if g := group(t, snap, "", `9100-curl "x"`); g.Conns[0].State != "SYN-SENT" {
		t.Errorf("curl: %+v", g)
	}
	if g := group(t, snap, "udp", "127.0.0.53:53"); g.Name != "systemd-resolve" {
		t.Errorf("resolved: %+v", g)
	}
	// No rates on the first read.
	if ssh.SendRate != 0 || ssh.RecvRate != 0 {
		t.Errorf("first rates %v %v", ssh.SendRate, ssh.RecvRate)
	}

	// Two seconds later: rates from the deltas; a connection gone and
	// one new.
	out2 := strings.Replace(out, "bytes_sent:1234567 bytes_acked:1234568 bytes_received:56789", "bytes_sent:1334567 bytes_acked:1334568 bytes_received:60789", 1)
	out2 = strings.Replace(out2, "45.56.78.90:51240", "45.56.78.90:51299", 1)
	snap, err = l.parse(out2, t0.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	ssh = group(t, snap, "tcp", "0.0.0.0:22")
	if ssh.Conns[0].SendRate != 50000 || ssh.Conns[0].RecvRate != 2000 || ssh.Conns[1].SendRate != 0 {
		t.Errorf("rates %+v", ssh.Conns)
	}
	if ssh.SendRate != 50000 || ssh.RecvRate != 2000 {
		t.Errorf("row's rates %v %v", ssh.SendRate, ssh.RecvRate)
	}
	if g := group(t, snap, "tcp", "0.0.0.0:110"); g.SendRate != 0 || g.Sent != 500 {
		t.Errorf("unchanged %+v", g)
	}
}

func TestNetSSOld(t *testing.T) {
	// Without -H, with the header, and IPv6 without brackets; without
	// awk, the information whole.
	out := `---FINALLYSHELL:uname---
Linux
---FINALLYSHELL:uid---
1000
---FINALLYSHELL:ss---
Netid  State      Recv-Q Send-Q     Local Address:Port       Peer Address:Port
tcp    LISTEN     0      128                   :::22                   :::*
tcp    ESTAB      0      0          ::ffff:10.0.0.5:22    ::ffff:45.56.78.90:51234
	 cubic wscale:7,7 rto:204 rtt:0.5/0.25 bytes_acked:1001 bytes_received:300 segs_out:10 send 100Mbps
tcp    ESTAB      0      0          10.0.0.5:22    1.0.1.7:40000
	 cubic wscale:7,7 rto:204 bytes_sent:77 segs_out:2
---FINALLYSHELL:end---
`
	var l NetLister
	snap, err := l.parse(out, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if snap.Root || snap.Unknown != 3 || len(snap.Groups) != 1 {
		t.Fatalf("%+v", snap)
	}
	g := snap.Groups[0]
	if g.Title() != "[::]:22" || len(g.Conns) != 2 || g.Conns[0].Remote != netip.MustParseAddrPort("45.56.78.90:51234") || g.Conns[0].Sent != 1000 || g.Conns[0].Received != 300 {
		t.Errorf("%+v", g)
	}
	if c := g.Conns[1]; c.Sent != 77 || c.Received != 0 || !c.HasBytes {
		t.Errorf("bytes_sent alone: %+v", c)
	}
}

func TestNetProc(t *testing.T) {
	var l NetLister
	snap, err := l.parse(readFixture(t, "net-proc.txt"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if snap.Root || !snap.FromProc || snap.Sockets != 12 {
		t.Errorf("root %v, proc %v, %d sockets", snap.Root, snap.FromProc, snap.Sockets)
	}
	// Other users' sockets: no process, but the TIME-WAIT is no one's.
	if snap.Unknown != 5 {
		t.Errorf("%d unknown", snap.Unknown)
	}
	var titles []string
	for _, g := range snap.Groups {
		titles = append(titles, g.Proto+" "+g.Title())
	}
	want := []string{"tcp [::]:22", "udp [::]:123", "tcp 127.0.0.1:2222", "tcp 0.0.0.0:47123", "udp 127.0.0.1:47125", "tcp 23204-python3", "tcp+udp 23300-java app", "tcp -"}
	if !slices.Equal(titles, want) {
		t.Errorf("rows\n%q\nwant\n%q", titles, want)
	}
	py := group(t, snap, "tcp", "0.0.0.0:47123")
	if py.PID != 23204 || py.Name != "python3" || len(py.Conns) != 1 || py.Conns[0].Remote != netip.MustParseAddrPort("127.0.0.1:55562") || py.HasBytes {
		t.Errorf("python's listener: %+v", py)
	}
	if g := group(t, snap, "tcp", "23204-python3"); len(g.Conns) != 1 || g.Conns[0].Remote.Port() != 47123 || g.Conns[0].Local.Port() != 55562 {
		t.Errorf("python's connection: %+v", g)
	}
	// The dual-stack listener takes IPv4 too.
	if g := group(t, snap, "tcp", "[::]:22"); g.PID != 0 || len(g.Conns) != 2 || g.Conns[0].Remote != netip.MustParseAddrPort("45.56.78.90:51234") ||
		g.Conns[1].Remote != netip.MustParseAddrPort("[2606:4700::1111]:60038") || g.Conns[1].Local.Addr() != netip.MustParseAddr("2001:db8::5") {
		t.Errorf("sshd: %+v", g)
	}
	java := group(t, snap, "", "23300-java app")
	if len(java.Conns) != 2 || java.Conns[0].Remote != netip.MustParseAddrPort("203.0.113.9:55001") || java.Conns[1].Proto != "udp" || java.Conns[1].State != "ESTAB" {
		t.Errorf("java: %+v", java)
	}
	if g := group(t, snap, "", "-"); len(g.Conns) != 1 || g.Conns[0].State != "TIME-WAIT" || g.Conns[0].Remote != netip.MustParseAddrPort("8.8.8.8:443") {
		t.Errorf("TIME-WAIT: %+v", g)
	}
}

func TestNetNotLinux(t *testing.T) {
	var l NetLister
	if _, err := l.parse("---FINALLYSHELL:uname---\nFreeBSD\n---FINALLYSHELL:end---\n", time.Now()); err != ErrNotLinux {
		t.Errorf("err %v", err)
	}
}

func TestParseSSAddr(t *testing.T) {
	for in, want := range map[string]string{
		"127.0.0.53%lo:53":     "127.0.0.53:53",
		"[::]:22":              "[::]:22",
		"[::ffff:1.2.3.4]:443": "1.2.3.4:443",
		"*:22":                 "[::]:22",
		"0.0.0.0:*":            "0.0.0.0:0",
		"[fe80::1%eth0]:123":   "[fe80::1]:123",
		":::22":                "[::]:22",
		"::ffff:10.0.0.5:22":   "10.0.0.5:22",
		"[2001:db8::5]:22":     "[2001:db8::5]:22",
	} {
		got, ok := parseSSAddr(in)
		if !ok || got.String() != want {
			t.Errorf("%s: %s %v, want %s", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "nothing", "1.2.3.4:http", "[zz]:1"} {
		if _, ok := parseSSAddr(bad); ok {
			t.Errorf("%q read", bad)
		}
	}
}
