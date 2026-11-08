package app

import (
	"slices"
	"testing"

	"finallyshell/internal/config"
	"finallyshell/internal/monitor"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// demoIfaces gives s's stats several interfaces, eth0 the busiest.
func demoIfaces(s *Session) {
	wave := func(base float64, step int) []float64 {
		var h []float64
		for i := range monitor.HistoryLen {
			h = append(h, base*(0.4+float64((i*step)%13)/10))
		}
		return h
	}
	st := &s.stats
	st.Ifaces = []monitor.Iface{
		{Name: "eth0", RxRate: st.RxRate, TxRate: st.TxRate, RxHist: st.RxHist, TxHist: st.TxHist},
		{Name: "wg0", RxRate: 310_000, TxRate: 95_000, RxHist: wave(250_000, 5), TxHist: wave(80_000, 3)},
		{Name: "docker0", RxRate: 2_100, TxRate: 900, RxHist: wave(2_000, 2), TxHist: wave(900, 4)},
		{Name: "veth3f2a1c9", RxRate: 900, TxRate: 2_100, RxHist: wave(900, 4), TxHist: wave(2_000, 2)},
		{Name: "lo", RxRate: 12_000, TxRate: 12_000, RxHist: wave(12_000, 1), TxHist: wave(12_000, 1)},
	}
}

func TestNetInterfaceMenu(t *testing.T) {
	a, p := testApp(t)
	a.saveHosts = config.SaveHosts
	s := demoSession(a, "web-1", "45.56.78.90")
	demoIfaces(s)
	a.active = 0
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	tt.Frame()
	if !tt.HasText("eth0") {
		t.Fatalf("no interface label; texts %q", tt.Texts())
	}

	open := func() {
		t.Helper()
		if err := tt.Click(locales.NetIfaces()); err != nil {
			t.Fatal(err)
		}
		want := []string{locales.NetIfaceAutomatic(), "-", "eth0", "wg0", "docker0", "veth3f2a1c9", "lo"}
		if got := tt.Menu(); !slices.Equal(got, want) {
			t.Fatalf("menu %q, want %q", got, want)
		}
	}
	choose := func(item string, want ...string) {
		t.Helper()
		open()
		if err := tt.ChooseMenuItem(item); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if !slices.Equal(s.Host.MonitorIfaces, want) {
			t.Fatalf("after %s: %q, want %q", item, s.Host.MonitorIfaces, want)
		}
		saved, err := config.LoadHosts()
		if err != nil || !slices.Equal(saved.Host(s.Host.ID).MonitorIfaces, want) {
			t.Fatalf("hosts.json after %s: %v (%v)", item, saved.Host(s.Host.ID), err)
		}
	}

	open()
	saveShot(t, tt, "netifaces-menu-en.png")
	tt.CloseMenu()

	choose("wg0", "wg0")
	if !tt.HasText("wg0") || !tt.HasText("↓303K") {
		t.Errorf("wg0 alone; texts %q", tt.Texts())
	}
	choose("eth0", "wg0", "eth0")
	if !tt.HasText("eth0 + wg0") || !tt.HasText("↓426K") {
		t.Errorf("summed; texts %q", tt.Texts())
	}
	saveShot(t, tt, "netifaces-two-en.png")

	a.setLanguage("zh-Hans")
	tt.Frame()
	open()
	saveShot(t, tt, "netifaces-menu-zh.png")
	tt.CloseMenu()
	tt.Frame()
	saveShot(t, tt, "netifaces-two-zh.png")
	a.setLanguage("en")
	tt.Frame()

	// Unchecking one leaves the other; unchecking the last is automatic.
	choose("wg0", "eth0")
	choose("eth0")

	// Chosen interfaces the server no longer has: automatic, said so,
	// and listed to be unchecked.
	s.Host.MonitorIfaces = []string{"tun0"}
	tt.Frame()
	if !tt.HasText(locales.NetIfaceFallback("eth0")) {
		t.Errorf("no fallback label; texts %q", tt.Texts())
	}
	if err := tt.Click(locales.NetIfaces()); err != nil {
		t.Fatal(err)
	}
	if got := tt.Menu(); len(got) != 8 || got[7] != "tun0" {
		t.Fatalf("menu with a missing interface %q", got)
	}
	if err := tt.ChooseMenuItem("tun0"); err != nil {
		t.Fatal(err)
	}
	if tt.Frame(); s.Host.MonitorIfaces != nil || !tt.HasText("eth0") || tt.HasText(locales.NetIfaceFallback("eth0")) {
		t.Errorf("tun0 unchecked: %q; texts %q", s.Host.MonitorIfaces, tt.Texts())
	}
}

// The label says which interfaces the graph shows, and which chosen are
// missing.
func TestNetLabel(t *testing.T) {
	for _, c := range []struct {
		v          monitor.NetView
		label, tip string
	}{
		{monitor.NetView{Auto: true, Names: []string{"eth0"}}, "eth0", ""},
		{monitor.NetView{Auto: true}, locales.NetIfaceAuto(), ""},
		{monitor.NetView{Names: []string{"eth0", "wg0"}}, "eth0 + wg0", ""},
		{monitor.NetView{Names: []string{"eth0"}, Missing: []string{"tun0"}}, "eth0", locales.NetIfacesPartlyMissing("tun0")},
		{monitor.NetView{Auto: true, Names: []string{"eth0"}, Missing: []string{"tun0", "wg1"}}, locales.NetIfaceFallback("eth0"), locales.NetIfacesMissing("tun0, wg1")},
		{monitor.NetView{Auto: true, Missing: []string{"tun0"}}, locales.NetIfaceAuto(), locales.NetIfacesMissing("tun0")},
	} {
		label, tip := netLabel(c.v)
		if label != c.label || tip != c.tip {
			t.Errorf("netLabel(%+v) = %q, %q; want %q, %q", c.v, label, tip, c.label, c.tip)
		}
	}
}
