package cloudsync

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeDAV is a WebDAV server in memory, for one user.
type fakeDAV struct {
	t *testing.T
	// noETag makes the server send no ETags.
	noETag bool

	mu      sync.Mutex
	folders map[string]bool   // paths ending in "/"
	files   map[string][]byte // by path
	etags   map[string]string
	n       int
	mkcols  []string
	srv     *httptest.Server
}

func newFakeDAV(t *testing.T) *fakeDAV {
	f := &fakeDAV{t: t, folders: map[string]bool{"/": true}, files: map[string][]byte{}, etags: map[string]string{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeDAV) remote(folder string) *WebDAV {
	return &WebDAV{URL: f.srv.URL + folder, User: "alice", Password: "dav-secret", Client: f.srv.Client()}
}

// write changes a file, as another client would.
func (f *fakeDAV) write(p string, data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	f.files[p] = data
	f.etags[p] = `"` + strconv.Itoa(f.n) + `"`
}

func (f *fakeDAV) mkcolCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.mkcols...)
}

func (f *fakeDAV) serve(w http.ResponseWriter, r *http.Request) {
	if u, p, ok := r.BasicAuth(); !ok || u != "alice" || p != "dav-secret" {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, "<error>bad login</error>")
		return
	}
	if r.Header.Get("If-None-Match") != "" {
		f.t.Errorf("%s %s: If-None-Match sent", r.Method, r.URL.Path)
	}
	p := r.URL.Path
	parent := path.Dir(strings.TrimSuffix(p, "/"))
	if parent != "/" {
		parent += "/"
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.Method {
	case http.MethodGet:
		data, ok := f.files[p]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if !f.noETag {
			w.Header().Set("ETag", f.etags[p])
		}
		w.Write(data)
	case http.MethodPut:
		if !f.folders[parent] {
			w.WriteHeader(http.StatusConflict)
			return
		}
		if m := r.Header.Get("If-Match"); m != "" && m != f.etags[p] {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		if r.Header.Get("Content-Type") != "application/json" {
			f.t.Errorf("PUT with Content-Type %q", r.Header.Get("Content-Type"))
		}
		data, _ := io.ReadAll(r.Body)
		_, existed := f.files[p]
		f.n++
		f.files[p] = data
		f.etags[p] = `"` + strconv.Itoa(f.n) + `"`
		if !f.noETag {
			w.Header().Set("ETag", f.etags[p])
		}
		if existed {
			w.WriteHeader(http.StatusNoContent)
		} else {
			w.WriteHeader(http.StatusCreated)
		}
	case "MKCOL":
		f.mkcols = append(f.mkcols, p)
		switch {
		case f.folders[p]:
			w.WriteHeader(http.StatusMethodNotAllowed)
		case !f.folders[parent]:
			w.WriteHeader(http.StatusConflict)
		default:
			f.folders[p] = true
			w.WriteHeader(http.StatusCreated)
		}
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func TestWebDAVRoundTripCreatesFolders(t *testing.T) {
	f := newFakeDAV(t)
	f.folders["/dav/"] = true
	w := f.remote("/dav/finallyshell/sub") // no trailing slash
	ctx := context.Background()

	if _, _, err := w.Get(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get with no file: %v, want ErrNotFound", err)
	}
	v, err := w.Put(ctx, []byte(`{"a":1}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/dav/", "/dav/finallyshell/", "/dav/finallyshell/sub/"}
	if got := f.mkcolCalls(); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("MKCOL calls %q, want %q", got, want)
	}
	data, v2, err := w.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"a":1}` || v2 != v || v.Tag == "" {
		t.Fatalf("Get: %s, %v; Put gave %v", data, v2, v)
	}
	v3, err := w.Put(ctx, []byte(`{"a":2}`), &v2)
	if err != nil {
		t.Fatal(err)
	}
	if v3 == v2 {
		t.Fatalf("Put kept the ETag %q", v3.Tag)
	}
	if n := len(f.mkcolCalls()); n != 3 {
		t.Fatalf("a Put into an existing folder made MKCOL calls: %d in all", n)
	}
}

func TestWebDAVConflict(t *testing.T) {
	f := newFakeDAV(t)
	f.folders["/dav/"] = true
	f.write("/dav/"+FileName, []byte("{}"))
	w := f.remote("/dav/")
	ctx := context.Background()
	_, v, err := w.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	f.write("/dav/"+FileName, []byte(`{"other":1}`))
	if _, err := w.Put(ctx, []byte(`{"mine":1}`), &v); !errors.Is(err, ErrConflict) {
		t.Fatalf("Put over a changed file: %v, want ErrConflict", err)
	}
}

func TestWebDAVAuth(t *testing.T) {
	f := newFakeDAV(t)
	w := f.remote("/dav/")
	w.Password = "wrong-password"
	_, _, err := w.Get(context.Background())
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("Get with a bad password: %v, want ErrAuth", err)
	}
	if strings.Contains(err.Error(), w.Password) {
		t.Fatalf("error text %q has the password", err)
	}
	if _, err := w.Put(context.Background(), []byte("{}"), nil); !errors.Is(err, ErrAuth) {
		t.Fatalf("Put with a bad password: %v, want ErrAuth", err)
	}
}

func TestWebDAVNoETag(t *testing.T) {
	f := newFakeDAV(t)
	f.noETag = true
	f.folders["/dav/"] = true
	w := f.remote("/dav/")
	ctx := context.Background()
	v, err := w.Put(ctx, []byte("{}"), nil)
	if err != nil || v.Tag != "" {
		t.Fatalf("Put: %v, %v; want no tag", v, err)
	}
	_, v, err = w.Get(ctx)
	if err != nil || v.Tag != "" {
		t.Fatalf("Get: %v, %v; want no tag", v, err)
	}
	// With no tag the write is not conditional, so it succeeds.
	f.write("/dav/"+FileName, []byte(`{"other":1}`))
	if _, err := w.Put(ctx, []byte(`{"mine":1}`), &v); err != nil {
		t.Fatal(err)
	}
}

func TestWebDAVFileURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://dav.jianguoyun.com/dav/finallyshell/": "https://dav.jianguoyun.com/dav/finallyshell/" + FileName,
		"https://dav.jianguoyun.com/dav/finallyshell":  "https://dav.jianguoyun.com/dav/finallyshell/" + FileName,
		"https://nas.local/dav/my%20sync/":             "https://nas.local/dav/my%20sync/" + FileName,
	} {
		got, err := (&WebDAV{URL: in}).fileURL()
		if err != nil || got != want {
			t.Errorf("fileURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := (&WebDAV{URL: "dav.example.com/x"}).fileURL(); err == nil {
		t.Error("fileURL of a URL with no scheme: no error")
	}
}
