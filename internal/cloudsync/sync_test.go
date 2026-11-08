package cloudsync

import (
	"context"
	"errors"
	"slices"
	"testing"
)

// memRemote keeps the file in memory, with a version that counts writes.
type memRemote struct {
	data    []byte
	ver     int
	puts    int
	onGet   func() // runs after a read, to change the file in between
	noMatch bool   // ignore If-Match, as a gist does
}

func (m *memRemote) tag() string { return string(rune('0' + m.ver)) }

func (m *memRemote) Get(context.Context) ([]byte, Version, error) {
	if m.data == nil {
		return nil, Version{}, ErrNotFound
	}
	d, v := m.data, Version{Tag: m.tag()}
	if m.onGet != nil {
		f := m.onGet
		m.onGet = nil
		f()
	}
	return d, v, nil
}

func (m *memRemote) Put(_ context.Context, data []byte, prev *Version) (Version, error) {
	if !m.noMatch && prev != nil && prev.Tag != m.tag() {
		return Version{}, ErrConflict
	}
	m.data, m.ver, m.puts = data, m.ver+1, m.puts+1
	return Version{Tag: m.tag()}, nil
}

func syncOK(t *testing.T, req Request) *Result {
	t.Helper()
	res, err := Sync(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestSyncTwoDevices(t *testing.T) {
	r := &memRemote{}
	// The first device uploads.
	one := hosts(host("a", "A"))
	res1 := syncOK(t, Request{Remote: r, Password: "pw", Local: one, Device: "one"})
	if !res1.Uploaded || !res1.Created || res1.Changed {
		t.Fatalf("first upload: %+v", res1)
	}
	// The second, empty, pulls it.
	res2 := syncOK(t, Request{Remote: r, Password: "pw", Local: &Payload{}, Device: "two"})
	if res2.Uploaded || !res2.Changed || res2.RemoteDevice != "one" {
		t.Fatalf("pull: %+v", res2)
	}
	if got := names(res2.Merged.Hosts); !slices.Equal(got, []string{"a=A"}) {
		t.Fatalf("pulled %v", got)
	}
	// The second deletes it and adds one; the first then syncs.
	res3 := syncOK(t, Request{Remote: r, Password: "pw", Local: hosts(host("b", "B")), Base: res2.Envelope, Device: "two"})
	if !res3.Uploaded {
		t.Fatalf("delete: %+v", res3)
	}
	res4 := syncOK(t, Request{Remote: r, Password: "pw", Local: one, Base: res1.Envelope, Device: "one"})
	if got := names(res4.Merged.Hosts); !slices.Equal(got, []string{"b=B"}) || res4.Uploaded {
		t.Fatalf("after the delete: %v, %+v", got, res4)
	}
	// Nothing changed: nothing is written.
	puts := r.puts
	syncOK(t, Request{Remote: r, Password: "pw", Local: res4.Merged, Base: res4.Envelope})
	if r.puts != puts {
		t.Error("a sync with no change wrote the file")
	}
}

func TestSyncWrongPassword(t *testing.T) {
	r := &memRemote{}
	syncOK(t, Request{Remote: r, Password: "pw", Local: hosts(host("a", "A"))})
	before := string(r.data)
	_, err := Sync(context.Background(), Request{Remote: r, Password: "other", Local: hosts(host("b", "B"))})
	if !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("err = %v", err)
	}
	if string(r.data) != before {
		t.Error("a wrong password changed the file")
	}
	// Force replaces it, with the new password.
	syncOK(t, Request{Remote: r, Password: "other", Local: hosts(host("b", "B")), Force: true})
	res := syncOK(t, Request{Remote: r, Password: "other", Local: &Payload{}})
	if got := names(res.Merged.Hosts); !slices.Equal(got, []string{"b=B"}) {
		t.Fatalf("after force: %v", got)
	}
}

func TestSyncRetriesOnConflict(t *testing.T) {
	r := &memRemote{}
	syncOK(t, Request{Remote: r, Password: "pw", Local: hosts(host("a", "A"))})
	base := r.data
	// Another device writes between this one's read and its write.
	r.onGet = func() {
		other := syncOK(t, Request{Remote: r, Password: "pw", Local: hosts(host("a", "A"), host("o", "other")), Base: base})
		_ = other
	}
	res := syncOK(t, Request{Remote: r, Password: "pw", Local: hosts(host("a", "A"), host("m", "mine")), Base: base})
	if got := names(res.Merged.Hosts); !slices.Equal(got, []string{"a=A", "m=mine", "o=other"}) {
		t.Fatalf("merged %v", got)
	}
	final := syncOK(t, Request{Remote: r, Password: "pw", Local: &Payload{}})
	if len(final.Merged.Hosts) != 3 {
		t.Fatalf("remote has %v", names(final.Merged.Hosts))
	}
}

func TestSyncKeepsRemoteSecretsWhenExcluded(t *testing.T) {
	r := &memRemote{}
	withPw := &Payload{Hosts: hosts(host("a", "A")).Hosts, Secrets: map[string]string{"host/a/password": "s3cret"}}
	res1 := syncOK(t, Request{Remote: r, Password: "pw", Local: withPw, Secrets: true})
	// A device without passwords edits a host.
	res2 := syncOK(t, Request{Remote: r, Password: "pw", Local: hosts(host("a", "A2")), Base: res1.Envelope})
	if !res2.Uploaded || res2.Merged.Secrets["host/a/password"] != "s3cret" {
		t.Fatalf("secrets dropped: %+v", res2.Merged)
	}
}

func TestSyncNewerSchema(t *testing.T) {
	r := &memRemote{}
	env, _ := Seal("pw", []byte(`{"schema":99,"hosts":[]}`), nil)
	r.data = env
	if _, err := Sync(context.Background(), Request{Remote: r, Password: "pw", Local: &Payload{}}); !errors.Is(err, ErrNewerFormat) {
		t.Fatalf("err = %v", err)
	}
}

// A device that synced without its passwords, then includes them, does
// not take the passwords it never received for ones it deleted.
func TestSyncBaseSecretsNotOwn(t *testing.T) {
	r := &memRemote{}
	withPw := &Payload{Hosts: hosts(host("a", "A")).Hosts, Secrets: map[string]string{"host/a/password": "s3cret"}}
	syncOK(t, Request{Remote: r, Password: "pw", Local: withPw, Secrets: true})
	without := syncOK(t, Request{Remote: r, Password: "pw", Local: &Payload{}})
	local := without.Merged.Clone()
	local.Secrets = nil // never applied here
	res := syncOK(t, Request{
		Remote: r, Password: "pw", Local: local, Secrets: true,
		Base: without.Envelope, BaseSecrets: false,
	})
	if res.Merged.Secrets["host/a/password"] != "s3cret" || res.Uploaded || !res.Changed {
		t.Fatalf("passwords lost: %+v", res)
	}
}
