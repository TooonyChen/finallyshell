package geo

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
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

// Both sources of a file are pinned to Release, the same tag.
func TestSources(t *testing.T) {
	want := []string{
		"https://raw.githubusercontent.com/lionsoul2014/ip2region/v3.18.0/data/ip2region_v4.xdb",
		"https://cdn.jsdelivr.net/gh/lionsoul2014/ip2region@v3.18.0/data/ip2region_v4.xdb",
	}
	if !slices.Equal(V4.URLs, want) {
		t.Errorf("V4 %q", V4.URLs)
	}
	// jsDelivr serves GitHub files of up to 20 MB: not the IPv6 one.
	if want := []string{"https://raw.githubusercontent.com/lionsoul2014/ip2region/v3.18.0/data/ip2region_v6.xdb"}; !slices.Equal(V6.URLs, want) {
		t.Errorf("V6 %q", V6.URLs)
	}
	for _, f := range []DataFile{V4, V6} {
		for _, u := range f.URLs {
			if !strings.Contains(u, "/"+Release+"/") && !strings.Contains(u, "@"+Release+"/") || !strings.HasSuffix(u, "/data/"+f.Name) {
				t.Errorf("%s: not %s of release %s", u, f.Name, Release)
			}
		}
		if f.Size > 20<<20 && slices.ContainsFunc(f.URLs, func(u string) bool { return hostOf(u) == "cdn.jsdelivr.net" }) {
			t.Errorf("%s: too large for jsDelivr", f.Name)
		}
	}
}

// source is a fake download server, counting its requests.
type source struct {
	*httptest.Server
	mu   sync.Mutex
	hits int
}

func (s *source) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits
}

func newSource(t *testing.T, h http.HandlerFunc) *source {
	s := &source{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.hits++
		s.mu.Unlock()
		h(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

// serve is a handler sending b, with its length when length is set.
func serve(b []byte, length bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if length {
			w.Header().Set("Content-Length", strconv.Itoa(len(b)))
		}
		w.Write(b)
	}
}

// refusedURL is a URL nothing listens at.
func refusedURL(t *testing.T) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return "http://" + addr + "/v4.xdb"
}

func dirFiles(dir string) []string {
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func setFirstByteTimeout(t *testing.T, d time.Duration) {
	old := firstByteTimeout
	firstByteTimeout = d
	t.Cleanup(func() { firstByteTimeout = old })
}

// The first source failing in each way falls back to the second, with
// the progress from 0 again.
func TestDownloadFallback(t *testing.T) {
	setFirstByteTimeout(t, 100*time.Millisecond)
	data := geotest.V4()
	wrong := bytes.Clone(data)
	wrong[len(wrong)/2] ^= 1
	good := newSource(t, serve(data, true))
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	wait := func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-stop:
		}
	}
	for _, c := range []struct {
		name string
		url  func() string
		err  error // the first source's, when known
	}{
		{"refused", func() string { return refusedURL(t) }, nil},
		{"404", func() string { return newSource(t, http.NotFound).URL + "/v4.xdb" }, nil},
		{"checksum", func() string { return newSource(t, serve(wrong, true)).URL + "/v4.xdb" }, ErrChecksum},
		{"size", func() string { return newSource(t, serve(data[:len(data)-1], true)).URL + "/v4.xdb" }, ErrChecksum},
		{"size unsaid", func() string { return newSource(t, serve(append(bytes.Clone(data), 0), false)).URL + "/v4.xdb" }, ErrChecksum},
		{"no headers", func() string { return newSource(t, wait).URL + "/v4.xdb" }, ErrNoAnswer},
		{"no body", func() string {
			return newSource(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Length", strconv.Itoa(len(data)))
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				wait(w, r)
			}).URL + "/v4.xdb"
		}, ErrNoAnswer},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := V4
			f.URLs = []string{c.url(), good.URL + "/v4.xdb"}
			f.Size, f.SHA256 = int64(len(data)), sum(data)
			dir := filepath.Join(t.TempDir(), "geo")
			var dones []int64
			before := good.count()
			if err := Download(context.Background(), nil, f, dir, func(done, total int64) { dones = append(dones, done) }); err != nil {
				t.Fatal(err)
			}
			if good.count() != before+1 {
				t.Error("the second source was not asked")
			}
			if got := dirFiles(dir); !slices.Equal(got, []string{f.Name}) {
				t.Errorf("files %v", got)
			}
			if _, err := Load(filepath.Join(dir, f.Name), f.SHA256); err != nil {
				t.Error(err)
			}
			if i := slices.Index(dones, 0); i < 0 || dones[len(dones)-1] != f.Size || slices.Contains(dones[i+1:], 0) {
				t.Errorf("progress %v", dones)
			}
			if c.name == "checksum" && dones[0] == 0 {
				t.Errorf("the wrong file's progress was not shown: %v", dones)
			}
		})
	}
}

// Every source failing leaves no file and says why for each host.
func TestDownloadAllFail(t *testing.T) {
	data := geotest.V4()
	missing := newSource(t, http.NotFound)
	wrong := newSource(t, serve(data[:len(data)/2], false))
	refused := refusedURL(t)
	f := DataFile{Name: "ip2region_v4.xdb", URLs: []string{missing.URL + "/v4.xdb", wrong.URL + "/v4.xdb", refused}, Size: int64(len(data)), SHA256: sum(data)}
	dir := filepath.Join(t.TempDir(), "geo")
	err := Download(context.Background(), nil, f, dir, nil)
	var de *DownloadError
	if !errors.As(err, &de) || len(de.Sources) != 3 {
		t.Fatalf("%T %v", err, err)
	}
	var st *StatusError
	if de.Sources[0].Host != hostOf(missing.URL+"/") || !errors.As(de.Sources[0].Err, &st) || st.Code != http.StatusNotFound {
		t.Errorf("first: %v", de.Sources[0])
	}
	if de.Sources[1].Host != hostOf(wrong.URL+"/") || !errors.Is(de.Sources[1].Err, ErrChecksum) {
		t.Errorf("second: %v", de.Sources[1])
	}
	if de.Sources[2].Host != hostOf(refused) || !strings.Contains(de.Sources[2].Err.Error(), "refused") {
		t.Errorf("third: %v", de.Sources[2])
	}
	if !errors.Is(err, ErrChecksum) {
		t.Error("not ErrChecksum")
	}
	msg := err.Error()
	for _, s := range de.Sources {
		if !strings.Contains(msg, s.Host+": ") {
			t.Errorf("%q does not name %s", msg, s.Host)
		}
	}
	if !strings.Contains(msg, "404 Not Found") {
		t.Errorf("%q", msg)
	}
	if got := dirFiles(dir); len(got) != 0 {
		t.Errorf("left %v", got)
	}
}

// A cancel stops the download without asking the next source.
func TestDownloadCancel(t *testing.T) {
	data := geotest.V4()
	block := make(chan struct{})
	slow := newSource(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write(data[:len(data)/2])
		w.(http.Flusher).Flush()
		select {
		case <-block:
		case <-r.Context().Done():
		}
	})
	next := newSource(t, serve(data, true))
	f := DataFile{Name: "ip2region_v4.xdb", URLs: []string{slow.URL + "/v4.xdb", next.URL + "/v4.xdb"}, Size: int64(len(data)), SHA256: sum(data)}
	dir := filepath.Join(t.TempDir(), "geo")
	ctx, cancel := context.WithCancel(context.Background())
	var once sync.Once
	err := Download(ctx, nil, f, dir, func(done, total int64) { once.Do(cancel) })
	close(block)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("%v", err)
	}
	if next.count() != 0 {
		t.Error("the next source was asked")
	}
	if got := dirFiles(dir); len(got) != 0 {
		t.Errorf("left %v", got)
	}
}

// The timeout is for the first byte only: a file that keeps coming may
// take longer.
func TestDownloadSlow(t *testing.T) {
	setFirstByteTimeout(t, 50*time.Millisecond)
	data := geotest.V4()
	slow := newSource(t, func(w http.ResponseWriter, r *http.Request) {
		for chunk := range slices.Chunk(data, len(data)/4+1) {
			w.Write(chunk)
			w.(http.Flusher).Flush()
			time.Sleep(60 * time.Millisecond)
		}
	})
	f := DataFile{Name: "ip2region_v4.xdb", URLs: []string{slow.URL + "/v4.xdb"}, Size: int64(len(data)), SHA256: sum(data)}
	dir := filepath.Join(t.TempDir(), "geo")
	if err := Download(context.Background(), nil, f, dir, nil); err != nil {
		t.Fatal(err)
	}
}
