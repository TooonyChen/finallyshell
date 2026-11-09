package geo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"finallyshell/internal/geo/geotest"
)

func TestLookup(t *testing.T) {
	v4, err := Open(geotest.V4())
	if err != nil {
		t.Fatal(err)
	}
	v6, err := Open(geotest.V6())
	if err != nil {
		t.Fatal(err)
	}
	if v4.IPv6() || !v6.IPv6() {
		t.Fatal("versions")
	}
	for ip, want := range map[string]string{
		"1.0.1.7":              "中国 福建省 福州市 电信",
		"1.0.3.255":            "中国 福建省 福州市 电信",
		"8.8.8.8":              "美国 Google",
		"45.56.78.90":          "美国 新泽西州",
		"103.21.245.1":         "Australia Victoria Melbourne Cloudflare",
		"::ffff:114.114.1.1":   "中国 江苏省 南京市",
		"2606:4700::1111":      "美国 加利福尼亚州 旧金山 Cloudflare",
		"2001:200:dff:fff1::1": "Japan Tokyo WIDE Project",
	} {
		addr := netip.MustParseAddr(ip)
		db := v4
		if addr.Unmap().Is6() {
			db = v6
		}
		loc, ok := db.Lookup(addr)
		if !ok || loc.String() != want {
			t.Errorf("%s: %q %v, want %q", ip, loc.String(), ok, want)
		}
	}
	for _, ip := range []string{"9.9.9.9", "1.0.4.0", "255.255.255.255", "0.0.0.0"} {
		if loc, ok := v4.Lookup(netip.MustParseAddr(ip)); ok {
			t.Errorf("%s: %+v", ip, loc)
		}
	}
	// The other version is not known.
	if _, ok := v4.Lookup(netip.MustParseAddr("2606:4700::1")); ok {
		t.Error("IPv6 in the IPv4 data")
	}
	if _, ok := v6.Lookup(netip.MustParseAddr("8.8.8.8")); ok {
		t.Error("IPv4 in the IPv6 data")
	}
	if loc, _ := v4.Lookup(netip.MustParseAddr("8.8.8.8")); loc.Complete() || loc.City != "" || loc.ISP != "Google" {
		t.Errorf("8.8.8.8: %+v", loc)
	}
	if _, err := Open([]byte("not an xdb file")); err == nil {
		t.Error("opened junk")
	}
}

// TestLookupRealData reads the real data file, when GEO_XDB names one
// that was downloaded by hand: the tests never download it.
func TestLookupRealData(t *testing.T) {
	path := os.Getenv("GEO_XDB")
	if path == "" {
		t.Skip("GEO_XDB not set")
	}
	db, err := Load(path, V4.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	loc, ok := db.Lookup(netip.MustParseAddr("1.0.1.1"))
	if !ok || loc.Country != "中国" {
		t.Errorf("1.0.1.1: %+v", loc)
	}
	t.Logf("8.8.8.8: %s", func() string { l, _ := db.Lookup(netip.MustParseAddr("8.8.8.8")); return l.String() }())
}

func TestIsLAN(t *testing.T) {
	for ip, want := range map[string]bool{
		"10.1.2.3": true, "172.16.0.1": true, "192.168.1.1": true, "127.0.0.1": true,
		"169.254.1.1": true, "100.64.0.1": true, "100.127.255.255": true, "0.0.0.0": true,
		"::1": true, "fe80::1": true, "fd00::1": true, "::": true, "::ffff:192.168.0.1": true, "224.0.0.1": true,
		"100.128.0.1": false, "8.8.8.8": false, "45.56.78.90": false, "2606:4700::1111": false, "::ffff:8.8.8.8": false,
	} {
		if got := IsLAN(netip.MustParseAddr(ip)); got != want {
			t.Errorf("IsLAN(%s) = %v", ip, got)
		}
	}
	if !IsLAN(netip.Addr{}) {
		t.Error("the zero address")
	}
}

func TestLocationString(t *testing.T) {
	for _, c := range []struct {
		loc  Location
		want string
	}{
		{Location{"美国", "新泽西州", "", "Choopa"}, "美国 新泽西州 Choopa"},
		{Location{"Singapore", "Singapore", "Singapore", "DigitalOcean"}, "Singapore DigitalOcean"},
		{Location{}, ""},
		{Location{Country: "中国"}.Fill(Location{"China", "广东", "深圳", "电信"}), "中国 广东 深圳 电信"},
	} {
		if got := c.loc.String(); got != c.want {
			t.Errorf("%+v: %q, want %q", c.loc, got, c.want)
		}
	}
	if got := parseRegion("中国|0|广东省|深圳市|电信", true); got != (Location{"中国", "广东省", "深圳市", "电信"}) {
		t.Errorf("structure 2.0: %+v", got)
	}
	if got := parseRegion("Reserved|Reserved|Reserved|0|0", false); got != (Location{}) {
		t.Errorf("reserved: %+v", got)
	}
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func TestDownload(t *testing.T) {
	data := geotest.V4()
	var mu sync.Mutex
	status := http.StatusOK
	var block chan struct{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		st, b := status, block
		mu.Unlock()
		if st != http.StatusOK {
			w.WriteHeader(st)
			return
		}
		if r.Header.Get("User-Agent") != userAgent {
			t.Errorf("User-Agent %q", r.Header.Get("User-Agent"))
		}
		w.Header().Set("Content-Length", "")
		w.Write(data[:len(data)/2])
		if b != nil {
			w.(http.Flusher).Flush()
			<-b
		}
		w.Write(data[len(data)/2:])
	}))
	defer srv.Close()
	f := DataFile{Name: "ip2region_v4.xdb", URL: srv.URL + "/v4.xdb", Size: int64(len(data)), SHA256: sum(data)}
	files := func(dir string) []string {
		entries, _ := os.ReadDir(dir)
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return names
	}

	dir := filepath.Join(t.TempDir(), "geo")
	var calls int
	var last int64
	err := Download(context.Background(), nil, f, dir, func(done, total int64) {
		calls++
		last = done
		if total != f.Size {
			t.Errorf("total %d", total)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls == 0 || last != f.Size {
		t.Errorf("progress: %d calls, last %d", calls, last)
	}
	if got := files(dir); !slices.Equal(got, []string{f.Name}) {
		t.Fatalf("files %v", got)
	}
	if _, err := Load(filepath.Join(dir, f.Name), f.SHA256); err != nil {
		t.Fatal(err)
	}

	// A wrong file, a server error and a cancel leave nothing.
	for _, c := range []struct {
		name string
		f    DataFile
		st   int
	}{
		{"checksum", DataFile{Name: f.Name, URL: f.URL, Size: f.Size, SHA256: strings.Repeat("0", 64)}, http.StatusOK},
		{"size", DataFile{Name: f.Name, URL: f.URL, Size: f.Size - 1, SHA256: f.SHA256}, http.StatusOK},
		{"status", f, http.StatusNotFound},
	} {
		dir := filepath.Join(t.TempDir(), "geo")
		mu.Lock()
		status = c.st
		mu.Unlock()
		err := Download(context.Background(), nil, c.f, dir, nil)
		if err == nil {
			t.Errorf("%s: no error", c.name)
		}
		if c.name != "status" && !errors.Is(err, ErrChecksum) {
			t.Errorf("%s: %v", c.name, err)
		}
		if got := files(dir); len(got) != 0 {
			t.Errorf("%s: left %v", c.name, got)
		}
	}
	mu.Lock()
	status = http.StatusOK
	block = make(chan struct{})
	mu.Unlock()
	dir = filepath.Join(t.TempDir(), "geo")
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	var once sync.Once
	go func() { <-started; cancel() }()
	err = Download(ctx, nil, f, dir, func(done, total int64) { once.Do(func() { close(started) }) })
	close(block)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("canceled: %v", err)
	}
	if got := files(dir); len(got) != 0 {
		t.Errorf("canceled: left %v", got)
	}

	// A damaged file is not loaded.
	bad := filepath.Join(t.TempDir(), "bad.xdb")
	os.WriteFile(bad, data[:1000], 0o644)
	if _, err := Load(bad, f.SHA256); !errors.Is(err, ErrChecksum) {
		t.Errorf("damaged: %v", err)
	}
	// Left-over parts are cleaned.
	os.WriteFile(filepath.Join(dir, "ip2region_v4.xdb.123.part"), []byte("x"), 0o644)
	CleanParts(dir)
	if got := files(dir); len(got) != 0 {
		t.Errorf("parts left %v", got)
	}
}

func TestCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "geo", "cache.json")
	c := LoadCache(path)
	if _, ok := c.Get("en", "8.8.8.8"); ok {
		t.Fatal("empty cache has it")
	}
	c.Put("en", "8.8.8.8", Location{"United States", "Virginia", "Ashburn", "Google LLC"})
	c.Put("zh-CN", "8.8.8.8", Location{"美国", "弗吉尼亚州", "阿什本", "Google LLC"})
	c.Put("en", "192.0.2.1", Location{})
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	c = LoadCache(path)
	if loc, ok := c.Get("zh-CN", "8.8.8.8"); !ok || loc.City != "阿什本" {
		t.Errorf("zh-CN: %+v %v", loc, ok)
	}
	if loc, ok := c.Get("en", "192.0.2.1"); !ok || loc != (Location{}) {
		t.Errorf("nothing known: %+v %v", loc, ok)
	}
	// Entries older than CacheTTL are dropped as the file is read.
	data, _ := os.ReadFile(path)
	var file map[string]map[string]map[string]any
	json.Unmarshal(data, &file)
	file["entries"]["en 8.8.8.8"]["at"] = time.Now().Add(-CacheTTL - time.Hour)
	data, _ = json.Marshal(file)
	os.WriteFile(path, data, 0o600)
	c = LoadCache(path)
	if _, ok := c.Get("en", "8.8.8.8"); ok {
		t.Error("an old entry was kept")
	}
	if _, ok := c.Get("zh-CN", "8.8.8.8"); !ok {
		t.Error("a new entry was dropped")
	}
	os.WriteFile(path, []byte("{junk"), 0o600)
	if c := LoadCache(path); len(c.entries) != 0 {
		t.Error("junk read")
	}
}

// fakeServices is ip-api.com and ip.sb.
type fakeServices struct {
	mu      sync.Mutex
	batches [][]string
	langs   []string
	sb      []string
	busy    bool // ip-api.com answers 429
	left    int  // X-Rl
}

func (f *fakeServices) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/batch", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Method != http.MethodPost {
			t.Errorf("method %s", r.Method)
		}
		var ips []string
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &ips)
		w.Header().Set("X-Ttl", "42")
		if f.busy {
			w.Header().Set("X-Rl", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		f.batches = append(f.batches, ips)
		f.langs = append(f.langs, r.URL.Query().Get("lang"))
		f.left--
		w.Header().Set("X-Rl", strconv.Itoa(f.left))
		var out []map[string]string
		for _, ip := range ips {
			if strings.HasPrefix(ip, "192.0.2.") {
				out = append(out, map[string]string{"status": "fail", "message": "reserved range", "query": ip})
				continue
			}
			out = append(out, map[string]string{"status": "success", "country": "美国", "regionName": "新泽西州", "city": "皮斯卡特维", "isp": "Choopa", "query": ip})
		}
		json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("/geoip/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if ua := r.Header.Get("User-Agent"); ua == "" || strings.HasPrefix(ua, "Go-http-client") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		ip := strings.TrimPrefix(r.URL.Path, "/geoip/")
		f.sb = append(f.sb, ip)
		json.NewEncoder(w).Encode(map[string]any{"ip": ip, "country": "United States", "region": "New Jersey", "city": "Piscataway", "organization": "Choopa", "asn": 20473})
	})
	return mux
}

func TestOnline(t *testing.T) {
	f := &fakeServices{left: 15}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	var slept []time.Duration
	o := &Online{IPAPI: srv.URL + "/batch", IPSB: srv.URL + "/geoip/"}
	o.sleepFunc = func(ctx context.Context, d time.Duration) error {
		slept = append(slept, d)
		return nil
	}
	var ips []string
	for i := range 250 {
		ips = append(ips, "45.56."+strconv.Itoa(i/200)+"."+strconv.Itoa(i%200))
	}
	ips = append(ips, "192.0.2.1")
	got, err := o.Lookup(context.Background(), ips, Lang("zh-Hans"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(ips) {
		t.Errorf("%d answers for %d addresses", len(got), len(ips))
	}
	if len(f.batches) != 3 || len(f.batches[0]) != 100 || len(f.batches[2]) != 51 {
		t.Errorf("batches of %v", func() (n []int) {
			for _, b := range f.batches {
				n = append(n, len(b))
			}
			return
		}())
	}
	if !slices.Equal(f.langs, []string{"zh-CN", "zh-CN", "zh-CN"}) {
		t.Errorf("langs %v", f.langs)
	}
	if loc := got["45.56.0.1"]; loc != (Location{"美国", "新泽西州", "皮斯卡特维", "Choopa"}) {
		t.Errorf("answer %+v", loc)
	}
	if loc, ok := got["192.0.2.1"]; !ok || loc != (Location{}) {
		t.Errorf("failed lookup %+v %v", loc, ok)
	}
	if len(slept) != 0 {
		t.Errorf("slept %v with requests left", slept)
	}

	// The last request of the minute: the next waits for X-Ttl.
	f.mu.Lock()
	f.left = 1
	f.mu.Unlock()
	if _, err := o.Lookup(context.Background(), []string{"8.8.8.8"}, "en"); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Lookup(context.Background(), []string{"8.8.4.4"}, "en"); err != nil {
		t.Fatal(err)
	}
	if len(slept) != 1 || slept[0] < 40*time.Second || slept[0] > 42*time.Second {
		t.Errorf("slept %v, want X-Ttl", slept)
	}

	// 429: ip.sb, one address at a time, with a User-Agent of its own.
	o2 := &Online{IPAPI: srv.URL + "/batch", IPSB: srv.URL + "/geoip/"}
	o2.sleepFunc = func(ctx context.Context, d time.Duration) error { return nil }
	f.mu.Lock()
	f.busy = true
	f.mu.Unlock()
	got, err = o2.Lookup(context.Background(), []string{"1.1.1.1", "9.9.9.9"}, "en")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.sb, []string{"1.1.1.1", "9.9.9.9"}) {
		t.Errorf("ip.sb asked %v", f.sb)
	}
	if loc := got["9.9.9.9"]; loc != (Location{"United States", "New Jersey", "Piscataway", "Choopa"}) {
		t.Errorf("ip.sb answer %+v", loc)
	}
	if Lang("en") != "en" || Lang("") != "en" {
		t.Error("Lang")
	}
}
