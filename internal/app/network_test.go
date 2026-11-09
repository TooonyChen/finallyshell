package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"finallyshell/internal/config"
	"finallyshell/internal/geo"
	"finallyshell/internal/geo/geotest"
	"finallyshell/internal/monitor"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
	"golang.org/x/crypto/ssh"
)

// fakeNetwork stands in for a server's sockets and trace tools.
type fakeNetwork struct {
	mu     sync.Mutex
	socks  []monitor.Socket
	lists  int
	traces []string // "target lang"
	// trace is what a trace answers, as its updates.
	route func(target string) []monitor.TraceResult
}

func (f *fakeNetwork) list(ctx context.Context, client *ssh.Client, l *monitor.NetLister) (monitor.NetSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lists++
	socks := slices.Clone(f.socks)
	unknown := 0
	for _, s := range socks {
		if s.PID == 0 {
			unknown++
		}
	}
	return monitor.NetSnapshot{Groups: monitor.GroupSockets(socks), Sockets: len(socks), Unknown: unknown}, nil
}

func (f *fakeNetwork) trace(ctx context.Context, client *ssh.Client, target, lang string, update func(monitor.TraceResult)) monitor.TraceResult {
	f.mu.Lock()
	f.traces = append(f.traces, target+" "+lang)
	steps := f.route(target)
	f.mu.Unlock()
	var last monitor.TraceResult
	for _, r := range steps {
		update(r)
		last = r
	}
	return last
}

func (f *fakeNetwork) set(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn()
}

func conn(proto, local, remote, state string, pid int, name string, sent, recv uint64, sendRate, recvRate float64) monitor.Socket {
	s := monitor.Socket{Proto: proto, Local: netip.MustParseAddrPort(local), State: state, PID: pid, Name: name,
		Sent: sent, Received: recv, HasBytes: proto == "tcp" && state != "LISTEN" && state != "TIME-WAIT", SendRate: sendRate, RecvRate: recvRate}
	if remote != "" {
		s.Remote = netip.MustParseAddrPort(remote)
	}
	return s
}

// demoSockets are a web server's sockets, as FinalShell's screenshot
// has them.
func demoSockets() []monitor.Socket {
	return []monitor.Socket{
		conn("tcp", "0.0.0.0:22", "", "LISTEN", 1290, "sshd", 0, 0, 0, 0),
		conn("tcp", "[::]:22", "", "LISTEN", 1290, "sshd", 0, 0, 0, 0),
		conn("tcp", "0.0.0.0:80", "", "LISTEN", 1200, "nginx", 0, 0, 0, 0),
		conn("tcp", "0.0.0.0:110", "", "LISTEN", 880, "dovecot", 0, 0, 0, 0),
		conn("tcp", "127.0.0.1:3306", "", "LISTEN", 812, "mysqld", 0, 0, 0, 0),
		conn("udp", "127.0.0.53:53", "", "UNCONN", 612, "systemd-resolve", 0, 0, 0, 0),
		conn("tcp", "10.0.0.5:22", "45.56.78.90:51234", "ESTAB", 4001, "sshd", 1_234_567, 56_789, 12_400, 820),
		conn("tcp", "10.0.0.5:22", "192.168.1.20:50110", "ESTAB", 4100, "sshd", 88_000, 9_100, 0, 0),
		conn("tcp", "10.0.0.5:80", "1.0.1.7:40001", "ESTAB", 1201, "nginx", 5_600_000, 41_000, 210_000, 3_100),
		conn("tcp", "10.0.0.5:80", "8.8.8.8:60000", "ESTAB", 1202, "nginx", 920_000, 12_000, 48_000, 900),
		conn("tcp", "10.0.0.5:80", "114.114.114.114:51001", "ESTAB", 1201, "nginx", 74_000, 3_000, 0, 0),
		conn("tcp", "10.0.0.5:80", "8.8.8.8:60010", "TIME-WAIT", 0, "", 0, 0, 0, 0),
		conn("tcp", "10.0.0.5:110", "203.0.113.9:40100", "ESTAB", 880, "dovecot", 18_000, 2_000, 0, 0),
		conn("tcp", "10.0.0.5:43182", "103.21.244.10:443", "ESTAB", 7267, "java", 209_730, 34_234, 1_200, 4_400),
		conn("tcp", "10.0.0.5:43186", "103.21.244.11:443", "ESTAB", 7267, "java", 100, 100, 0, 0),
		conn("tcp", "127.0.0.1:43300", "127.0.0.1:3306", "ESTAB", 7267, "java", 51_000, 920_000, 300, 9_800),
		conn("tcp", "10.0.0.5:33482", "34.149.66.165:443", "ESTAB", 0, "", 12_473, 3_968, 0, 0),
	}
}

// fakeGeo is the download server of the location data and the online
// services, which count what they are asked.
type fakeGeo struct {
	mu      sync.Mutex
	gets    []string
	batches [][]string
	status  int           // of the downloads: 200 when 0
	block   chan struct{} // holds a download halfway when set
}

func (f *fakeGeo) requests() (gets []string, batches [][]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.gets), slices.Clone(f.batches)
}

func (f *fakeGeo) server(t *testing.T) *httptest.Server {
	files := map[string][]byte{"/v4.xdb": geotest.V4(), "/v6.xdb": geotest.V6()}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		if r.URL.Path == "/batch" {
			var ips []string
			body, _ := io.ReadAll(r.Body)
			json.Unmarshal(body, &ips)
			f.batches = append(f.batches, ips)
			f.mu.Unlock()
			var out []map[string]string
			for _, ip := range ips {
				out = append(out, map[string]string{"status": "success", "country": "美国", "regionName": "新泽西州", "city": "皮斯卡特维", "isp": "Choopa", "query": ip})
			}
			w.Header().Set("X-Rl", "14")
			w.Header().Set("X-Ttl", "60")
			json.NewEncoder(w).Encode(out)
			return
		}
		f.gets = append(f.gets, r.URL.Path)
		status, block := f.status, f.block
		f.mu.Unlock()
		data, ok := files[r.URL.Path]
		if !ok || status != 0 {
			w.WriteHeader(max(status, http.StatusNotFound))
			return
		}
		w.Write(data[:len(data)/2])
		if block != nil {
			w.(http.Flusher).Flush()
			select {
			case <-block:
			case <-r.Context().Done():
				return
			}
		}
		w.Write(data[len(data)/2:])
	}))
	t.Cleanup(srv.Close)
	return srv
}

func dataFile(name, url string, data []byte) geo.DataFile {
	h := sha256.Sum256(data)
	return geo.DataFile{Name: name, URLs: []string{url}, Size: int64(len(data)), SHA256: hex.EncodeToString(h[:])}
}

// networkApp is an app showing a connected session's Network tab, on
// fake sockets, with the location data and the online services served
// by a fake server.
func networkApp(t *testing.T) (*App, *fakePlatform, *Session, *fakeNetwork, *fakeGeo, *ui.Tester) {
	t.Helper()
	a, p := testApp(t)
	s := demoSession(a, "web-1", "45.56.78.90")
	a.active = 0
	f := &fakeNetwork{socks: demoSockets(), route: func(string) []monitor.TraceResult { return nil }}
	a.network.api = f
	g := &fakeGeo{}
	srv := g.server(t)
	a.geo.files = [2]geo.DataFile{
		dataFile("ip2region_v4.xdb", srv.URL+"/v4.xdb", geotest.V4()),
		dataFile("ip2region_v6.xdb", srv.URL+"/v6.xdb", geotest.V6()),
	}
	a.geo.online = &geo.Online{IPAPI: srv.URL + "/batch", IPSB: srv.URL + "/geoip/"}
	a.bottomTab = tabNetwork
	a.terminalPct = 28
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	tt.Frame()
	p.until(t, "the sockets", func() bool { return s.net.loaded })
	return a, p, s, f, g, tt
}

func geoFiles(t *testing.T) []string {
	t.Helper()
	entries, _ := os.ReadDir(config.GeoDir())
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// group is the row of s's Network tab titled title.
func netGroup(t *testing.T, s *Session, title string) monitor.NetGroup {
	t.Helper()
	for _, g := range s.net.snap.Groups {
		if g.Title() == title {
			return g
		}
	}
	t.Fatalf("no row %s", title)
	return monitor.NetGroup{}
}

// Without the location data: the question, no Location column, nothing
// downloaded or looked up.
func TestNetworkWithoutLocations(t *testing.T) {
	a, p, s, f, g, tt := networkApp(t)
	for _, want := range []string{"Network", "PID", "Listen IP", "Port", "Protocol", "Remote IPs", "Connections", "Upload/s", "Download/s",
		"sshd", "nginx", "0.0.0.0", "7267", "java", "UDP", "Needs root for other users' processes", "8 entries"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q", want)
		}
	}
	ask := locales.GeoAskV4(geoDownloadSize(false))
	if !tt.HasText(ask) || tt.HasText("Location") {
		t.Errorf("the question: %v, Location %v", tt.HasText(ask), tt.HasText("Location"))
	}
	s.net.key = netGroup(t, s, "0.0.0.0:80").Key()
	tt.Frame()
	saveShot(t, tt, "network-ask-en.png")
	a.setLanguage("zh-Hans")
	tt.Frame()
	for _, want := range []string{"网络", "监听 IP", "远程 IP 数", "连接数", "上传速度", "下载", "暂不", "不再询问"} {
		if !tt.HasText(want) {
			t.Errorf("Chinese: missing %q", want)
		}
	}
	saveShot(t, tt, "network-ask-zh.png")
	a.setLanguage("en")

	// Not now: gone for this run, asked again at the next start.
	if err := tt.Click(locales.GeoNotNow()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText(ask) || a.settings.GeoLocations != "" {
		t.Errorf("after Not now: %v, %q", tt.HasText(ask), a.settings.GeoLocations)
	}

	// A row's connections, IPs only.
	a.openNetTab(s, netGroup(t, s, "0.0.0.0:80"))
	tt.Frame()
	for _, want := range []string{"0.0.0.0:80", "1.0.1.7", "114.114.114.114", "TIME-WAIT", "4 connections", "205.1 KB/s", "5.3 MB"} {
		if !tt.HasText(want) {
			t.Errorf("connections: missing %q; texts %q", want, tt.Texts())
		}
	}
	if tt.HasText("Location") {
		t.Error("a Location column without the data")
	}
	saveShot(t, tt, "network-nodata-en.png")
	a.setLanguage("zh-Hans")
	tt.Frame()
	saveShot(t, tt, "network-nodata-zh.png")
	a.setLanguage("en")

	// Don't ask again.
	a.geo.notNow = [2]bool{}
	tt.Frame()
	if err := tt.Click(locales.GeoNever()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText(ask) || a.settings.GeoLocations != config.GeoOff || config.LoadSettings().GeoLocations != config.GeoOff {
		t.Errorf("after Don't ask again: %v, %q", tt.HasText(ask), a.settings.GeoLocations)
	}

	// The filter, and a sort.
	s.net.filter = "dovecot"
	tt.Frame()
	if n := len(s.net.shown()); n != 1 || !tt.HasText("1 of 8 entries") {
		t.Errorf("filtered: %d rows", n)
	}
	s.net.filter = "8.8.8"
	if n := len(s.net.shown()); n != 1 {
		t.Errorf("by a remote IP: %d rows", n)
	}
	s.net.filter = ""
	if err := tt.Click("Connections"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if v := s.net.shown(); s.net.sort.Column != "conns" || len(v[len(v)-1].Conns) != 4 {
		t.Errorf("by connections: %+v", s.net.sort)
	}

	// Refreshed while shown, not while another tab is.
	f.set(func() {
		f.socks = append(f.socks, conn("tcp", "0.0.0.0:8443", "", "LISTEN", 9000, "caddy", 0, 0, 0, 0))
	})
	s.net.poll.pollNow()
	p.until(t, "the new socket", func() bool { return len(s.net.snap.Groups) == 9 })
	a.bottomTab = tabFiles
	tt.Frame()
	tt.Frame()
	if s.net.poll.run != nil {
		t.Error("polling on another tab")
	}

	// Nothing asked of the location server.
	if gets, batches := g.requests(); len(gets) != 0 || len(batches) != 0 {
		t.Errorf("requests without the data: %v %v", gets, batches)
	}
	if names := geoFiles(t); len(names) != 0 {
		t.Errorf("files %v", names)
	}
}

// With the data: download, locations, the IPv6 question, and online
// lookups only once turned on.
func TestNetworkLocations(t *testing.T) {
	a, p, s, f, g, tt := networkApp(t)
	f.set(func() {
		f.socks = append(f.socks, conn("tcp", "[2001:db8::5]:22", "[2606:4700::1111]:60022", "ESTAB", 5000, "sshd", 100, 50, 0, 0))
	})
	s.net.poll.pollNow()
	p.until(t, "the IPv6 connection", func() bool { return len(netGroup(t, s, "[::]:22").Conns) == 1 })
	a.openNetTab(s, netGroup(t, s, "0.0.0.0:80"))
	if err := tt.Click(locales.GeoDownload()); err != nil {
		t.Fatal(err)
	}
	if a.settings.GeoLocations != config.GeoOn {
		t.Errorf("setting %q", a.settings.GeoLocations)
	}
	p.until(t, "the data", func() bool { return a.locationsShown() })
	if names := geoFiles(t); !slices.Equal(names, []string{"ip2region_v4.xdb"}) {
		t.Errorf("files %v", names)
	}
	tt.Frame()
	for _, want := range []string{"Location", "中国 福建省 福州市 电信", "美国 Google", "中国 江苏省 南京市"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q; texts %q", want, tt.Texts())
		}
	}
	// Asked about the IPv6 data, as an IPv6 peer showed.
	askV6 := locales.GeoAskV6(geoDownloadSize(true))
	s.net.poll.pollNow()
	p.until(t, "the IPv6 question", func() bool { return tt.HasText(askV6) })
	a.openNetTab(s, netGroup(t, s, "0.0.0.0:22"))
	tt.Frame()
	if !tt.HasText(locales.GeoLAN()) || !tt.HasText("美国 新泽西州") {
		t.Errorf("sshd's connections: %q", tt.Texts())
	}
	a.geoNotNow(true)
	s.net.key = netGroup(t, s, "0.0.0.0:80").Key()
	s.net.tab = 0
	tt.Frame()
	saveShot(t, tt, "network-locations-en.png")
	a.setLanguage("zh-Hans")
	tt.Frame()
	if !tt.HasText("归属地") {
		t.Error("Chinese: no 归属地")
	}
	saveShot(t, tt, "network-locations-zh.png")
	a.setLanguage("en")
	a.geo.notNow[1] = false
	tt.Frame()
	if err := tt.Click(locales.GeoDownload()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the IPv6 data", func() bool { return a.geo.db[1] != nil })
	a.openNetTab(s, netGroup(t, s, "[::]:22"))
	tt.Frame()
	if !tt.HasText("美国 加利福尼亚州 旧金山 Cloudflare") || a.settings.GeoLocationsV6 != config.GeoOn {
		t.Errorf("IPv6: %q", tt.Texts())
	}
	// Turned off in the options: no IPv6 locations, nothing looked up.
	a.setLocationsV6(false)
	tt.Frame()
	if tt.HasText("美国 加利福尼亚州 旧金山 Cloudflare") || a.placeOf(netip.MustParseAddr("2606:4700::1111"), true) != "" || a.geo.pending["2606:4700::1111"] {
		t.Error("IPv6 locations after turning them off")
	}
	a.setLocationsV6(true)
	p.until(t, "the IPv6 data again", func() bool { return a.geo.db[1] != nil })
	if gets, _ := g.requests(); slices.Index(gets, "/v6.xdb") != len(gets)-1 || len(gets) != 2 {
		t.Errorf("downloads %v: the IPv6 file is read again, not downloaded", gets)
	}

	// Online lookups are off: nothing was sent.
	if _, batches := g.requests(); len(batches) != 0 {
		t.Fatalf("sent %v", batches)
	}
	// On: what the data leaves without a city or ISP, of the rows
	// shown, never a LAN address.
	a.setOnline(true)
	a.openNetTab(s, netGroup(t, s, "0.0.0.0:22"))
	p.until(t, "the online answer", func() bool { return tt.HasText("美国 新泽西州 皮斯卡特维 Choopa") })
	_, batches := g.requests()
	var sent []string
	for _, b := range batches {
		sent = append(sent, b...)
	}
	slices.Sort(sent)
	if slices.Contains(sent, "192.168.1.20") || slices.Contains(sent, "1.0.1.7") || !slices.Contains(sent, "45.56.78.90") {
		t.Errorf("sent %v", sent)
	}
	p.until(t, "the cache", func() bool { _, err := os.Stat(filepath.Join(config.GeoDir(), "cache.json")); return err == nil })

	// Asked once: the next frames send nothing more.
	for range 3 {
		s.net.poll.pollNow()
		tt.Frame()
	}
	if _, b := g.requests(); len(b) != len(batches) {
		t.Errorf("asked again: %v", b[len(batches):])
	}

}

// A download that fails from every source leaves no file, says why for
// each and offers Retry; one whose first source fails comes from the
// next; a canceled one leaves no file either.
func TestNetworkDownloadFails(t *testing.T) {
	a, p, _, _, g, tt := networkApp(t)
	f := &a.geo.files[0]
	host := strings.TrimPrefix(f.URLs[0][:strings.LastIndex(f.URLs[0], "/")], "http://")
	f.URLs = append([]string{"http://" + host + "/gone.xdb"}, f.URLs...)
	g.mu.Lock()
	g.status = http.StatusInternalServerError
	g.mu.Unlock()
	if err := tt.Click(locales.GeoDownload()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the failure", func() bool { return a.geo.dl != nil && a.geo.dl.err != nil })
	tt.Frame()
	want := locales.GeoDownloadFailed(host + ": 500 Internal Server Error; " + host + ": 500 Internal Server Error")
	if !tt.HasText(locales.Retry()) || !tt.HasText(want) {
		t.Errorf("texts %q, want %q", tt.Texts(), want)
	}
	a.setLanguage("zh-Hans")
	tt.Frame()
	if want := "无法下载位置数据：" + host + "：500 Internal Server Error；" + host + "：500 Internal Server Error"; !tt.HasText(want) {
		t.Errorf("Chinese: %q, want %q", tt.Texts(), want)
	}
	a.setLanguage("en")
	if names := geoFiles(t); len(names) != 0 {
		t.Errorf("files %v", names)
	}
	g.mu.Lock()
	g.status = 0
	g.block = make(chan struct{})
	g.mu.Unlock()
	if err := tt.Click(locales.Retry()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the progress", func() bool { return a.geo.dl != nil && a.geo.dl.done > 0 })
	if gets, _ := g.requests(); !slices.Equal(gets[len(gets)-2:], []string{"/gone.xdb", "/v4.xdb"}) {
		t.Errorf("asked %v", gets)
	}
	tt.Frame()
	if pct := int(100 * a.geo.dl.done / a.geo.dl.total); !tt.HasText(locales.GeoDownloading(pct)) {
		t.Errorf("progress: %q", tt.Texts())
	}
	saveShot(t, tt, "network-downloading-en.png")
	if err := tt.Click(locales.Cancel()); err != nil {
		t.Fatal(err)
	}
	close(g.block)
	tt.Frame()
	if a.geo.dl != nil || a.locationsShown() {
		t.Error("still downloading")
	}
	time.Sleep(100 * time.Millisecond)
	p.until(t, "the partial file gone", func() bool { return len(geoFiles(t)) == 0 })
}

func nextTraceHops(t *testing.T) []monitor.TraceHop {
	data, err := os.ReadFile("../monitor/testdata/nexttrace.json")
	if err != nil {
		t.Fatal(err)
	}
	hops, err := monitor.ParseNextTrace(data, traceLang())
	if err != nil {
		t.Fatal(err)
	}
	return hops
}

func TestNetworkTrace(t *testing.T) {
	a, p, s, f, _, tt := networkApp(t)
	a.geoNever(false)
	f.set(func() {
		f.route = func(target string) []monitor.TraceResult {
			hops := nextTraceHops(t)
			return []monitor.TraceResult{{Tool: monitor.ToolNextTrace}, {Tool: monitor.ToolNextTrace, Hops: hops, Done: true}}
		}
	})
	a.openNetTab(s, netGroup(t, s, "0.0.0.0:80"))
	a.startTrace(s, "8.8.8.8")
	p.until(t, "the route", func() bool { return tt.HasText("dns.google") })
	for _, want := range []string{"Trace 8.8.8.8", "Hop", "RTT", "ASN", "AS15169", "Australia Victoria Melbourne Cloudflare", "0.45 ms", "*", "NextTrace"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q; texts %q", want, tt.Texts())
		}
	}
	saveShot(t, tt, "network-trace-en.png")
	a.setLanguage("zh-Hans")
	a.runTrace(s, s.net.tabs[s.net.tab].trace)
	p.until(t, "the route in Chinese", func() bool { return tt.HasText("澳大利亚 维多利亚州 墨尔本 Cloudflare") })
	saveShot(t, tt, "network-trace-zh.png")
	a.setLanguage("en")
	if got := f.traces; !slices.Equal(got, []string{"8.8.8.8 en", "8.8.8.8 cn"}) {
		t.Errorf("traces %v", got)
	}
	if text := a.traceText(s.net.tabs[s.net.tab].trace); !strings.Contains(text, "5\t8.8.8.8\t43.97 ms\t美国 Google\t15169\tdns.google") {
		t.Errorf("copied:\n%s", text)
	}

	// No NextTrace: its install command, and traceroute's route, without
	// locations (no data).
	f.set(func() {
		f.route = func(target string) []monitor.TraceResult {
			r := monitor.TraceResult{Tool: monitor.ToolTraceroute, NoNextTrace: true, Hops: []monitor.TraceHop{
				{TTL: 1, IP: "10.0.0.1", RTTs: []time.Duration{500 * time.Microsecond}},
				{TTL: 2},
				{TTL: 3, IP: "1.0.1.1", RTTs: []time.Duration{41 * time.Millisecond}},
			}}
			done := r
			done.Done = true
			return []monitor.TraceResult{r, done}
		}
	})
	a.startTrace(s, "1.0.1.1")
	p.until(t, "the fallback", func() bool {
		return tt.HasText(monitor.NextTraceInstall) && tt.HasText("41.00 ms") && !s.net.tabs[s.net.tab].trace.running
	})
	if !tt.HasText(locales.NextTraceMissingFallback("traceroute")) || tt.HasText("Location") || tt.HasText("ASN") {
		t.Errorf("texts %q", tt.Texts())
	}
	saveShot(t, tt, "network-trace-fallback-en.png")

	// Nothing at all.
	f.set(func() {
		f.route = func(string) []monitor.TraceResult {
			return []monitor.TraceResult{{NoNextTrace: true, Done: true, Err: monitor.ErrNoTraceTool}}
		}
	})
	a.startTrace(s, "9.9.9.9")
	p.until(t, "no tool", func() bool { return tt.HasText(locales.TraceNoTool()) })
	if !tt.HasText(monitor.NextTraceInstall) {
		t.Error("no install command")
	}
	saveShot(t, tt, "network-trace-missing-en.png")

	// Typed: an address or host is checked first.
	a.openTraceDialog(s, "")
	tt.Frame()
	tt.Type("bad host; rm -rf /")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if !tt.HasText(locales.TraceInvalid()) || a.network.dlg == nil {
		t.Fatalf("invalid: %q", tt.Texts())
	}
	a.network.dlg.target = "example.com"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	p.until(t, "the typed trace", func() bool { return len(f.traces) == 5 })
	if a.network.dlg != nil || f.traces[4] != "example.com en" {
		t.Errorf("traces %v", f.traces)
	}
	// A host whose server monitor is off opens no exec channel for it.
	s.Host.NoMonitor = true
	a.startTrace(s, "8.8.8.8")
	tt.Frame()
	if len(f.traces) != 5 || !tt.HasText(locales.MonitorOff()) || tt.HasText(locales.TraceRouteTo()) {
		t.Errorf("traced with the monitor off: %v", f.traces)
	}
	s.Host.NoMonitor = false

	// Closing a tab stops its trace.
	for len(s.net.tabs) > 0 {
		a.closeNetTab(s, 0)
	}
	tt.Frame()
}

// The options page: turning locations on downloads the data, its size,
// the online lookups, and deleting the data.
func TestNetworkOptionsPage(t *testing.T) {
	a, p, _, _, g, _ := networkApp(t)
	a.optionsNav.Page = pageNetwork
	a.openOptions()
	tt := p.show(t, WindowOptions)
	tt.Frame()
	for _, want := range []string{locales.OptionsNetwork(), locales.OptionShowLocations(geoDownloadSize(false)), locales.OptionGeoOnline(), locales.OptionTraceHeading(), locales.GeoCredit()} {
		if !tt.HasText(want) {
			t.Errorf("missing %q; texts %q", want, tt.Texts())
		}
	}
	saveShot(t, tt, "options-network-en.png")
	a.setLanguage("zh-Hans")
	tt.Frame()
	saveShot(t, tt, "options-network-zh.png")
	a.setLanguage("en")
	tt.Frame()

	if err := tt.Click(locales.OptionShowLocations(geoDownloadSize(false))); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the data", func() bool { return a.locationsShown() })
	a.geo.sizeKnown = false
	p.until(t, "the size", func() bool {
		tt.Frame()
		return tt.HasText(locales.OptionGeoData(formatSize(int64(len(geotest.V4())))))
	})
	if err := tt.Click(locales.OptionGeoOnline()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.settings.GeoOnline || !config.LoadSettings().GeoOnline {
		t.Error("online lookups not on")
	}
	saveShot(t, tt, "options-network-on-en.png")

	p.answer = true
	if err := tt.Click(locales.GeoDeleteData()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the data deleted", func() bool { return a.geo.sizeKnown && a.geo.size == 0 && len(geoFiles(t)) == 0 })
	if a.locationsShown() || a.settings.GeoLocations != config.GeoOff || a.settings.GeoOnline {
		t.Errorf("after delete: %+v", a.settings)
	}
	if gets, _ := g.requests(); !slices.Equal(gets, []string{"/v4.xdb"}) {
		t.Errorf("downloads %v", gets)
	}
}
