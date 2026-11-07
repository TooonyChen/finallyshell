package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestTunnelHelpers(t *testing.T) {
	local := Tunnel{Kind: TunnelLocal, ListenPort: 8080, TargetHost: "db.internal", TargetPort: 5432}
	if local.ListenAddr() != "127.0.0.1:8080" || local.TargetAddr() != "db.internal:5432" || !local.Enabled() {
		t.Errorf("local %q %q %v", local.ListenAddr(), local.TargetAddr(), local.Enabled())
	}
	v6 := Tunnel{Kind: TunnelRemote, ListenHost: "::", ListenPort: 9000, TargetHost: "::1", TargetPort: 3000, Disabled: true}
	if v6.ListenAddr() != "[::]:9000" || v6.TargetAddr() != "[::1]:3000" || v6.Enabled() {
		t.Errorf("v6 %q %q %v", v6.ListenAddr(), v6.TargetAddr(), v6.Enabled())
	}
	socks := Tunnel{Kind: TunnelDynamic, ListenPort: 1080, TargetHost: "ignored", TargetPort: 1}
	if socks.TargetAddr() != "" {
		t.Errorf("dynamic target %q", socks.TargetAddr())
	}
	for host, want := range map[string]bool{"": true, "localhost": true, "127.0.0.1": true, "127.0.0.2": true, "::1": true,
		"0.0.0.0": false, "::": false, "192.168.1.5": false, "example.com": false} {
		if IsLoopback(host) != want {
			t.Errorf("IsLoopback(%q) = %v", host, !want)
		}
	}
}

// A tunnel keeps the keys this build does not know, and a kind it does
// not know, as a newer build wrote them.
func TestTunnelUnknownKeys(t *testing.T) {
	t.Setenv("FINALSHELLY_DIR", t.TempDir())
	newer, err := os.ReadFile("testdata/hosts_newer.json")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(Dir(), "hosts.json"), newer, 0o600)
	s, err := LoadHosts()
	if err != nil {
		t.Fatal(err)
	}
	tunnels := s.Hosts[0].Tunnels
	if len(tunnels) != 2 || tunnels[0].Kind != TunnelLocal || tunnels[0].ListenPort != 8080 || tunnels[0].TargetAddr() != "localhost:80" ||
		tunnels[1].Kind != "x11" {
		t.Fatalf("tunnels %+v", tunnels)
	}
	tunnels[0].ListenPort = 8081
	data, _ := json.Marshal(tunnels)
	if want := `[{"kind":"local","listenPort":8081,"targetHost":"localhost","targetPort":80,"note":"admin"},{"kind":"x11"}]`; string(data) != want {
		t.Errorf("encoded %s\nwant    %s", data, want)
	}
}
