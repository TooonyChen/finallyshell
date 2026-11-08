package cloudsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeGist is one gist of fakeGitHub.
type fakeGist struct {
	files     map[string]string
	versions  int
	updatedAt string
}

// fakeGitHub is GitHub's gist API in memory, for one token.
type fakeGitHub struct {
	t     *testing.T
	token string
	// truncateAt makes GET /gists/{id} truncate file contents longer
	// than it, 0 for never.
	truncateAt int

	mu      sync.Mutex
	gists   map[string]*fakeGist
	order   []string // list order, newest first, as GitHub lists
	nextID  int
	rawHits int
	srv     *httptest.Server
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	f := &fakeGitHub{t: t, token: "ghp_secret", gists: map[string]*fakeGist{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

// add puts a gist with one file, as another client would.
func (f *fakeGitHub) add(id, updatedAt, name, content string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gists[id] = &fakeGist{files: map[string]string{name: content}, versions: 1, updatedAt: updatedAt}
	f.order = append(f.order, id)
}

// file is a gist's file, "" for none.
func (f *fakeGitHub) file(id string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if g := f.gists[id]; g != nil {
		return g.files[FileName]
	}
	return ""
}

// raws is how many times a raw URL was read.
func (f *fakeGitHub) raws() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rawHits
}

// edit changes a gist's file, as another client would.
func (f *fakeGitHub) edit(id, content string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	g := f.gists[id]
	g.files[FileName] = content
	g.versions++
}

func (f *fakeGitHub) doc(id string, g *fakeGist, truncate bool) map[string]any {
	files := map[string]any{}
	for name, c := range g.files {
		file := map[string]any{"filename": name, "content": c, "truncated": false,
			"raw_url": f.srv.URL + "/raw/" + id + "/" + name}
		if truncate && f.truncateAt > 0 && len(c) > f.truncateAt {
			file["content"] = c[:f.truncateAt]
			file["truncated"] = true
		}
		files[name] = file
	}
	var history []any
	for v := g.versions; v >= 1; v-- {
		history = append(history, map[string]any{"version": id + "-v" + strconv.Itoa(v)})
	}
	return map[string]any{"id": id, "updated_at": g.updatedAt, "files": files, "history": history}
}

func (f *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+f.token {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"message":"Bad credentials"}`)
		return
	}
	if r.Header.Get("User-Agent") == "" || r.Header.Get("X-GitHub-Api-Version") != "2022-11-28" {
		f.t.Errorf("%s %s: missing GitHub headers", r.Method, r.URL)
	}
	writeJSON := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(v)
	}
	readFiles := func() map[string]string {
		var body struct {
			Description string `json:"description"`
			Public      *bool  `json:"public"`
			Files       map[string]struct {
				Content string `json:"content"`
			} `json:"files"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			f.t.Errorf("bad body: %v", err)
		}
		if r.Method == http.MethodPost && (body.Public == nil || *body.Public || body.Description == "") {
			f.t.Errorf("POST /gists: want a secret gist with a description, got %+v", body)
		}
		files := map[string]string{}
		for name, file := range body.Files {
			files[name] = file.Content
		}
		return files
	}
	path := r.URL.Path
	switch {
	case path == "/gists" && r.Method == http.MethodGet:
		if r.URL.Query().Get("per_page") != "100" {
			f.t.Errorf("list without per_page=100: %s", r.URL)
		}
		// Two pages, of the first two gists and of the rest.
		page := r.URL.Query().Get("page")
		var ids []string
		switch page {
		case "", "1":
			ids = f.order[:min(2, len(f.order))]
			if len(f.order) > 2 {
				w.Header().Set("Link", `<`+f.srv.URL+`/gists?per_page=100&page=2>; rel="next", <`+
					f.srv.URL+`/gists?per_page=100&page=2>; rel="last"`)
			}
		case "2":
			ids = f.order[min(2, len(f.order)):]
		}
		var list []any
		for _, id := range ids {
			d := f.doc(id, f.gists[id], false)
			delete(d, "history")
			list = append(list, d)
		}
		writeJSON(http.StatusOK, list)
	case path == "/gists" && r.Method == http.MethodPost:
		f.nextID++
		id := "new" + strconv.Itoa(f.nextID)
		f.gists[id] = &fakeGist{files: readFiles(), versions: 1, updatedAt: "2030-01-01T00:00:00Z"}
		f.order = append([]string{id}, f.order...)
		writeJSON(http.StatusCreated, f.doc(id, f.gists[id], false))
	case strings.HasPrefix(path, "/gists/"):
		id := strings.TrimPrefix(path, "/gists/")
		g := f.gists[id]
		if g == nil {
			writeJSON(http.StatusNotFound, map[string]string{"message": "Not Found"})
			return
		}
		switch r.Method {
		case http.MethodGet:
			writeJSON(http.StatusOK, f.doc(id, g, true))
		case http.MethodPatch:
			for name, c := range readFiles() {
				g.files[name] = c
			}
			g.versions++
			writeJSON(http.StatusOK, f.doc(id, g, false))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	case strings.HasPrefix(path, "/raw/"):
		f.rawHits++
		parts := strings.SplitN(strings.TrimPrefix(path, "/raw/"), "/", 2)
		g := f.gists[parts[0]]
		if g == nil || len(parts) != 2 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		fmt.Fprint(w, g.files[parts[1]])
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeGitHub) remote() *Gist {
	return &Gist{Token: f.token, BaseURL: f.srv.URL, Client: f.srv.Client()}
}

func TestGistDiscoveryAcrossPages(t *testing.T) {
	f := newFakeGitHub(t)
	f.add("other", "2026-01-03T00:00:00Z", "notes.txt", "hello")
	f.add("older", "2025-01-01T00:00:00Z", FileName, `{"old":true}`)
	f.add("newest", "2026-01-02T00:00:00Z", FileName, `{"new":true}`)
	g := f.remote()
	ctx := context.Background()

	data, v, err := g.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if g.ID != "newest" || string(data) != `{"new":true}` || v.Tag != "newest-v1" {
		t.Fatalf("Get: ID %q, data %s, tag %q", g.ID, data, v.Tag)
	}

	v2, err := g.Put(ctx, []byte(`{"n":2}`), &v)
	if err != nil {
		t.Fatal(err)
	}
	if v2.Tag != "newest-v2" {
		t.Fatalf("Put: tag %q, want newest-v2", v2.Tag)
	}
	data, v3, err := g.Get(ctx)
	if err != nil || string(data) != `{"n":2}` || v3 != v2 {
		t.Fatalf("Get after Put: %s %v %v", data, v3, err)
	}
}

func TestGistConflict(t *testing.T) {
	f := newFakeGitHub(t)
	f.add("g", "2026-01-01T00:00:00Z", FileName, "{}")
	g := f.remote()
	ctx := context.Background()
	_, v, err := g.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	f.edit("g", `{"other":1}`)
	if _, err := g.Put(ctx, []byte(`{"mine":1}`), &v); !errors.Is(err, ErrConflict) {
		t.Fatalf("Put over a changed gist: %v, want ErrConflict", err)
	}
	if got := f.file("g"); got != `{"other":1}` {
		t.Fatalf("the conflicting Put wrote %s", got)
	}
	// A Put of nothing read, over a gist that has the file, conflicts too.
	if _, err := g.Put(ctx, []byte(`{"mine":1}`), nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("Put with no prev over an existing file: %v, want ErrConflict", err)
	}
}

func TestGistNotFoundCreates(t *testing.T) {
	f := newFakeGitHub(t)
	f.add("other", "2026-01-01T00:00:00Z", "notes.txt", "hello")
	g := f.remote()
	ctx := context.Background()

	if _, _, err := g.Get(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get with no gist: %v, want ErrNotFound", err)
	}
	if g.ID != "" {
		t.Fatalf("Get with no gist set ID %q", g.ID)
	}
	v, err := g.Put(ctx, []byte(`{"a":1}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if g.ID != "new1" || v.Tag != "new1-v1" {
		t.Fatalf("Put created ID %q, tag %q", g.ID, v.Tag)
	}

	// A gist that is gone: ErrNotFound with ID kept, and Put makes another.
	g2 := f.remote()
	g2.ID = "deleted"
	if _, _, err := g2.Get(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get of a deleted gist: %v, want ErrNotFound", err)
	}
	if g2.ID != "deleted" {
		t.Fatalf("Get of a deleted gist changed ID to %q", g2.ID)
	}
	if _, err := g2.Put(ctx, []byte(`{"b":1}`), nil); err != nil {
		t.Fatal(err)
	}
	if g2.ID != "new2" {
		t.Fatalf("Put over a deleted gist: ID %q, want new2", g2.ID)
	}
	// With a prev, a deleted gist is a change: the caller reads again.
	g2.ID = "deleted"
	if _, err := g2.Put(ctx, []byte(`{"b":1}`), &Version{Tag: "x"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("Put with prev over a deleted gist: %v, want ErrConflict", err)
	}
}

func TestGistAuth(t *testing.T) {
	f := newFakeGitHub(t)
	g := f.remote()
	g.Token = "ghp_wrong_token"
	_, _, err := g.Get(context.Background())
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("Get with a bad token: %v, want ErrAuth", err)
	}
	if !strings.Contains(err.Error(), "Bad credentials") || strings.Contains(err.Error(), g.Token) {
		t.Fatalf("error text %q: want GitHub's message and no token", err)
	}
	g.Token = ""
	if _, err := g.Put(context.Background(), []byte("{}"), nil); !errors.Is(err, ErrAuth) {
		t.Fatalf("Put with no token: %v, want ErrAuth", err)
	}
}

func TestGistTruncated(t *testing.T) {
	f := newFakeGitHub(t)
	f.truncateAt = 10
	big := `{"data":"` + strings.Repeat("x", 100) + `"}`
	f.add("g", "2026-01-01T00:00:00Z", FileName, big)
	g := f.remote()
	g.ID = "g"
	data, _, err := g.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != big || f.raws() != 1 {
		t.Fatalf("Get of a truncated file: %d bytes, %d raw reads", len(data), f.raws())
	}
}

func TestNextLink(t *testing.T) {
	for h, want := range map[string]string{
		`<https://api.github.com/gists?page=2>; rel="next", <https://api.github.com/gists?page=5>; rel="last"`: "https://api.github.com/gists?page=2",
		`<https://api.github.com/gists?page=1>; rel="prev"`:                                                    "",
		``: "",
	} {
		if got := nextLink(h); got != want {
			t.Errorf("nextLink(%q) = %q, want %q", h, got, want)
		}
	}
}
