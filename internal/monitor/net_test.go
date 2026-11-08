package monitor

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// procNetDev is /proc/net/dev with the interfaces' rx and tx bytes.
func procNetDev(ifaces ...any) string {
	var b strings.Builder
	b.WriteString("Inter-|   Receive                                                |  Transmit\n")
	b.WriteString(" face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed\n")
	for i := 0; i+2 < len(ifaces); i += 3 {
		fmt.Fprintf(&b, "%6s: %d 10 0 0 0 0 0 0 %d 8 0 0 0 0 0 0\n", ifaces[i], ifaces[i+1], ifaces[i+2])
	}
	return b.String()
}

func names(ifaces []Iface) []string {
	var out []string
	for _, it := range ifaces {
		out = append(out, it.Name)
	}
	return out
}

func TestParseNetAll(t *testing.T) {
	got := parseNet(procNetDev("lo", 999999, 999999, "eth0", 5000, 3000, "wg0", 10, 20) + "garbage: x\n")
	want := []netCounters{{"lo", 999999, 999999}, {"eth0", 5000, 3000}, {"wg0", 10, 20}}
	if !slices.Equal(got, want) {
		t.Errorf("parseNet = %v, want %v", got, want)
	}
}

func TestNetUpdate(t *testing.T) {
	var (
		n  netState
		st Stats
		t0 = time.Unix(1000, 0)
	)
	// lo is the busiest by far, and still never the automatic one; veth
	// pairs come after the others, lo last.
	n.update(&st, procNetDev("lo", 900000, 900000, "veth1a", 0, 0, "eth0", 5000, 3000, "docker0", 100, 100), t0)
	if got := names(st.Ifaces); !slices.Equal(got, []string{"eth0", "docker0", "veth1a", "lo"}) {
		t.Errorf("order %v", got)
	}
	if st.Iface != "eth0" || st.RxRate != 0 || len(st.RxHist) != 0 {
		t.Errorf("first sample: %q %v %v", st.Iface, st.RxRate, st.RxHist)
	}

	n.update(&st, procNetDev("lo", 900000, 900000, "veth1a", 0, 0, "eth0", 7000, 4000, "docker0", 300, 100), t0.Add(2*time.Second))
	if st.Iface != "eth0" || st.RxRate != 1000 || st.TxRate != 500 || !slices.Equal(st.RxHist, []float64{1000}) {
		t.Errorf("eth0: %q %v %v %v", st.Iface, st.RxRate, st.TxRate, st.RxHist)
	}
	if d := st.Ifaces[1]; d.Name != "docker0" || d.RxRate != 100 || !slices.Equal(d.TxHist, []float64{0}) {
		t.Errorf("docker0: %+v", d)
	}

	// wg0 comes up: listed at once, with history from its second sample.
	// docker0 is made again, its counters back at 0: a zero sample.
	n.update(&st, procNetDev("lo", 900000, 900000, "eth0", 9000, 4000, "docker0", 0, 0, "wg0", 50, 50), t0.Add(4*time.Second))
	if got := names(st.Ifaces); !slices.Equal(got, []string{"eth0", "docker0", "wg0", "lo"}) {
		t.Errorf("order with wg0 %v", got)
	}
	if w := st.Ifaces[2]; w.RxRate != 0 || len(w.RxHist) != 0 {
		t.Errorf("wg0 new: %+v", w)
	}
	if d := st.Ifaces[1]; d.RxRate != 0 || !slices.Equal(d.RxHist, []float64{100, 0}) {
		t.Errorf("docker0 reset: %+v", d)
	}
	n.update(&st, procNetDev("eth0", 9000, 4000, "docker0", 0, 0, "wg0", 250, 450), t0.Add(6*time.Second))
	if w := st.Ifaces[2]; w.RxRate != 100 || w.TxRate != 200 || !slices.Equal(w.TxHist, []float64{200}) {
		t.Errorf("wg0: %+v", w)
	}
	if e := st.Ifaces[0]; !slices.Equal(e.RxHist, []float64{1000, 1000, 0}) {
		t.Errorf("eth0 history %v", e.RxHist)
	}

	// Only lo left: no automatic interface.
	n.update(&st, procNetDev("lo", 1, 1), t0.Add(8*time.Second))
	if st.Iface != "" || st.RxHist != nil || len(st.Ifaces) != 1 {
		t.Errorf("only lo: %+v", st)
	}
}

func TestNetHistoryKeepsLength(t *testing.T) {
	var (
		n  netState
		st Stats
	)
	for i := range HistoryLen + 10 {
		n.update(&st, procNetDev("eth0", i*100, i*10), time.Unix(int64(i), 0))
	}
	if len(st.RxHist) != HistoryLen || st.RxHist[0] != 100 {
		t.Errorf("history %d %v", len(st.RxHist), st.RxHist[:2])
	}
}

func TestNetView(t *testing.T) {
	st := Stats{
		Iface: "eth0", RxRate: 10, TxRate: 1, RxHist: []float64{8, 9, 10}, TxHist: []float64{1, 1, 1},
		Ifaces: []Iface{
			{Name: "eth0", RxRate: 10, TxRate: 1, RxHist: []float64{8, 9, 10}, TxHist: []float64{1, 1, 1}},
			{Name: "wg0", RxRate: 5, TxRate: 2, RxHist: []float64{4, 5}, TxHist: []float64{2, 2}},
			{Name: "lo", RxRate: 100, TxRate: 100, RxHist: []float64{100, 100, 100}, TxHist: []float64{100, 100, 100}},
		},
	}

	v := st.Net(nil)
	if !v.Auto || !slices.Equal(v.Names, []string{"eth0"}) || v.RxRate != 10 || len(v.Missing) != 0 {
		t.Errorf("automatic: %+v", v)
	}

	// Summed, in the order of Ifaces, wg0's shorter history lined up at
	// the end.
	v = st.Net([]string{"wg0", "eth0"})
	if v.Auto || !slices.Equal(v.Names, []string{"eth0", "wg0"}) || v.RxRate != 15 || v.TxRate != 3 {
		t.Errorf("sum: %+v", v)
	}
	if !slices.Equal(v.RxHist, []float64{8, 13, 15}) || !slices.Equal(v.TxHist, []float64{1, 3, 3}) {
		t.Errorf("sum history: %v %v", v.RxHist, v.TxHist)
	}
	if !slices.Equal(st.Ifaces[0].RxHist, []float64{8, 9, 10}) {
		t.Errorf("summing changed eth0's history: %v", st.Ifaces[0].RxHist)
	}

	// lo can be chosen by hand.
	if v = st.Net([]string{"lo"}); v.Auto || v.RxRate != 100 {
		t.Errorf("lo: %+v", v)
	}

	// A missing one is left out; with none left, automatic.
	v = st.Net([]string{"wg0", "tun0"})
	if v.Auto || !slices.Equal(v.Names, []string{"wg0"}) || !slices.Equal(v.Missing, []string{"tun0"}) {
		t.Errorf("one missing: %+v", v)
	}
	v = st.Net([]string{"tun0", "tun0"})
	if !v.Auto || !slices.Equal(v.Names, []string{"eth0"}) || !slices.Equal(v.Missing, []string{"tun0"}) || v.RxRate != 10 {
		t.Errorf("all missing: %+v", v)
	}

	if v = (&Stats{}).Net([]string{"eth0"}); !v.Auto || v.Names != nil {
		t.Errorf("no interfaces: %+v", v)
	}
}

func TestCloneStatsCopiesIfaces(t *testing.T) {
	st := Stats{Ifaces: []Iface{{Name: "eth0", RxHist: []float64{1}, TxHist: []float64{2}}}}
	c := cloneStats(st)
	st.Ifaces[0].RxHist[0], st.Ifaces[0].Name = 9, "x"
	if c.Ifaces[0].RxHist[0] != 1 || c.Ifaces[0].Name != "eth0" {
		t.Errorf("clone shares: %+v", c.Ifaces)
	}
}
