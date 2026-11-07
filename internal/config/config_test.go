package config

import "testing"

func TestHostsAndSettings(t *testing.T) {
	t.Setenv("FINALSHELLY_DIR", t.TempDir())
	if hosts, err := LoadHosts(); err != nil || hosts != nil {
		t.Fatalf("no file: %v %v", hosts, err)
	}
	h := &Host{ID: NewHostID(), Addr: "example.com", Port: 2200, User: "root", Auth: AuthPassword}
	if err := SaveHosts([]*Host{h}); err != nil {
		t.Fatal(err)
	}
	hosts, err := LoadHosts()
	if err != nil || len(hosts) != 1 || *hosts[0] != *h {
		t.Fatalf("loaded %v %v", hosts, err)
	}
	if h.Target() != "root@example.com:2200" || h.Title() != "example.com" {
		t.Errorf("target %q title %q", h.Target(), h.Title())
	}
	if err := SaveSettings(Settings{Language: "zh-Hans"}); err != nil {
		t.Fatal(err)
	}
	if s := LoadSettings(); s.Language != "zh-Hans" {
		t.Errorf("settings %+v", s)
	}
}
