package cloudsync

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/net/webdav"
)

// TestWebDAVRealServer syncs through x/net/webdav, a full WebDAV server:
// its ETags, If-Match and MKCOL, rather than the fake's.
func TestWebDAVRealServer(t *testing.T) {
	dav := &webdav.Handler{FileSystem: webdav.NewMemFS(), LockSystem: webdav.NewMemLS()}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "me" || p != "pw" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		dav.ServeHTTP(w, r)
	}))
	defer srv.Close()
	ctx := context.Background()
	r := &WebDAV{URL: srv.URL + "/a/b/finallyshell", User: "me", Password: "pw"}

	one := hosts(host("h1", "web"))
	res, err := Sync(ctx, Request{Remote: r, Password: "sync", Local: one})
	if err != nil || !res.Created {
		t.Fatalf("first sync: %+v, %v", res, err)
	}
	two, err := Sync(ctx, Request{Remote: r, Password: "sync", Local: hosts(host("h2", "db"))})
	if err != nil || !two.Uploaded || len(two.Merged.Hosts) != 2 {
		t.Fatalf("second sync: %+v, %v", two, err)
	}

	// It gives ETags, though it ignores If-Match (it checks only lock
	// tokens): there, as with a gist, a write that races another is not
	// caught. The fake server in webdav_test.go checks If-Match.
	if _, v, err := r.Get(ctx); err != nil || v.Tag == "" {
		t.Fatalf("Get: %v, tag %q", err, v.Tag)
	}
	bad := &WebDAV{URL: r.URL, User: "me", Password: "wrong"}
	if _, _, err := bad.Get(ctx); !errors.Is(err, ErrAuth) {
		t.Fatalf("wrong password: %v", err)
	}
}
