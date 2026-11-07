package config

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"
)

func TestProxyJSON(t *testing.T) {
	// Every field is read, and keys this build does not know are kept.
	in := `{"id": "h1", "addr": "x", "proxy": {"kind": "http", "addr": "proxy.example.com", "port": 3128, "user": "u", "jumpHost": "h2", "timeout": 5}}`
	var h Host
	if err := json.Unmarshal([]byte(in), &h); err != nil {
		t.Fatal(err)
	}
	want := &ProxyConfig{Kind: ProxyHTTP, Addr: "proxy.example.com", Port: 3128, User: "u", JumpHost: "h2"}
	got := *h.Proxy
	got.extra = nil
	if !reflect.DeepEqual(&got, want) {
		t.Errorf("proxy %+v, want %+v", got, want)
	}
	data, _ := json.Marshal(h)
	var back struct {
		Proxy map[string]any `json:"proxy"`
	}
	json.Unmarshal(data, &back)
	if back.Proxy["timeout"] != 5.0 || back.Proxy["port"] != 3128.0 || back.Proxy["kind"] != "http" || back.Proxy["jumpHost"] != "h2" {
		t.Errorf("written %s", data)
	}
	// A proxy of no kind has none of the keys.
	data, _ = json.Marshal(ProxyConfig{})
	if string(data) != "{}" {
		t.Errorf("empty proxy %s", data)
	}
}

func TestProxyPorts(t *testing.T) {
	for _, c := range []struct {
		p    ProxyConfig
		want int
	}{
		{ProxyConfig{Kind: ProxySOCKS5}, 1080},
		{ProxyConfig{Kind: ProxyHTTP}, 8080},
		{ProxyConfig{Kind: ProxyHTTP, Port: 3128}, 3128},
		{ProxyConfig{Kind: ProxyJump}, 0},
	} {
		if got := c.p.PortOrDefault(); got != c.want {
			t.Errorf("%+v: port %d, want %d", c.p, got, c.want)
		}
	}
	if ProxyOf(&Host{}) != nil || ProxyOf(&Host{Proxy: &ProxyConfig{}}) != nil {
		t.Error("a host without a proxy has one")
	}
}

func TestJumpHosts(t *testing.T) {
	s := NewStore()
	jump := func(id string) *ProxyConfig { return &ProxyConfig{Kind: ProxyJump, JumpHost: id} }
	s.Add(&Host{ID: "direct", Name: "direct"})
	s.Add(&Host{ID: "socks", Name: "socks", Proxy: &ProxyConfig{Kind: ProxySOCKS5, Addr: "p"}})
	s.Add(&Host{ID: "a", Name: "a", Proxy: jump("b")})
	s.Add(&Host{ID: "b", Name: "b", Proxy: jump("socks")})
	s.Add(&Host{ID: "x", Name: "x", Proxy: jump("y")})
	s.Add(&Host{ID: "y", Name: "y", Proxy: jump("x")})
	s.Add(&Host{ID: "self", Name: "self", Proxy: jump("self")})
	s.Add(&Host{ID: "lost", Name: "lost", Proxy: jump("gone")})
	s.Add(&Host{ID: "old", Name: "old", DeletedAt: time.Now()})
	s.Add(&Host{ID: "toOld", Name: "toOld", Proxy: jump("old")})
	s.Add(&Host{ID: "intoLoop", Name: "intoLoop", Proxy: jump("x")})

	for _, c := range []struct {
		id    string
		jumps []string
		loop  []string
		from  string // the host whose jump host is missing
	}{
		{id: "direct"},
		{id: "socks"},
		{id: "a", jumps: []string{"b", "socks"}},
		{id: "b", jumps: []string{"socks"}},
		{id: "x", loop: []string{"x", "y", "x"}},
		{id: "self", loop: []string{"self", "self"}},
		{id: "intoLoop", loop: []string{"intoLoop", "x", "y", "x"}},
		{id: "lost", from: "lost"},
		{id: "toOld", from: "toOld"},
	} {
		got, err := s.JumpHosts(s.Host(c.id))
		var loop *JumpLoopError
		var missing *JumpError
		switch {
		case c.loop != nil:
			if !errors.As(err, &loop) || !slices.Equal(loop.Names, c.loop) {
				t.Errorf("%s: %v, want a loop %v", c.id, err, c.loop)
			}
		case c.from != "":
			if !errors.As(err, &missing) || missing.From != c.from || !errors.Is(err, ErrJumpHostMissing) {
				t.Errorf("%s: %v, want a missing jump host", c.id, err)
			}
		case err != nil || !slices.Equal(hostIDs(got), c.jumps):
			t.Errorf("%s: jumps %v %v, want %v", c.id, hostIDs(got), err, c.jumps)
		}
	}

	// A host being edited, not saved yet, whose new jump host leads back
	// to it.
	edited := &Host{ID: "socks", Name: "socks", Proxy: jump("a")}
	var loop *JumpLoopError
	if _, err := s.JumpHosts(edited); !errors.As(err, &loop) || !slices.Equal(loop.Names, []string{"socks", "a", "b", "socks"}) {
		t.Errorf("edited: %v", err)
	}
	// A new host has no ID: nothing leads to it.
	if got, err := s.JumpHosts(&Host{Proxy: jump("a")}); err != nil || !slices.Equal(hostIDs(got), []string{"a", "b", "socks"}) {
		t.Errorf("new: %v %v", hostIDs(got), err)
	}
}
