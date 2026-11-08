package monitor

import (
	"bufio"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Iface is one network interface's traffic.
type Iface struct {
	Name   string
	RxRate float64 // bytes per second
	TxRate float64
	RxHist []float64
	TxHist []float64

	rx, tx uint64 // the byte counters, as /proc/net/dev has them
}

// netCounters is an interface's line of /proc/net/dev.
type netCounters struct {
	name   string
	rx, tx uint64
}

// parseNet reads the byte counters of every interface in /proc/net/dev,
// in its order.
func parseNet(s string) []netCounters {
	var out []netCounters
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		name, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		f := strings.Fields(rest)
		if name == "" || len(f) < 9 {
			continue
		}
		r, err1 := strconv.ParseUint(f[0], 10, 64)
		t, err2 := strconv.ParseUint(f[8], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		out = append(out, netCounters{name, r, t})
	}
	return out
}

// ifaceRank orders the interfaces in the menu: the others first, then
// Docker's veth pairs, which can be many, then the loopback.
func ifaceRank(name string) int {
	switch {
	case name == "lo":
		return 2
	case strings.HasPrefix(name, "veth"):
		return 1
	}
	return 0
}

// netState is what Run keeps between samples of /proc/net/dev: each
// interface's counters and history, so that each has its own.
type netState struct {
	ifaces map[string]Iface
	at     time.Time
}

// update reads out, /proc/net/dev at now, into st: every interface, in
// the menu's order, and the busiest as Iface and its rates and history.
// An interface gets a sample from its second reading on; one whose
// counters went back (it was made again) gets a zero, so that the
// histories of all stay lined up at their ends.
func (n *netState) update(st *Stats, out string, now time.Time) {
	secs := now.Sub(n.at).Seconds()
	next := map[string]Iface{}
	var ifaces []Iface
	for _, c := range parseNet(out) {
		if _, dup := next[c.name]; dup {
			continue
		}
		it := Iface{Name: c.name, rx: c.rx, tx: c.tx}
		if old, ok := n.ifaces[c.name]; ok && secs > 0 {
			it.RxHist, it.TxHist = old.RxHist, old.TxHist
			if c.rx >= old.rx && c.tx >= old.tx {
				it.RxRate = float64(c.rx-old.rx) / secs
				it.TxRate = float64(c.tx-old.tx) / secs
			}
			it.RxHist = pushHist(it.RxHist, it.RxRate)
			it.TxHist = pushHist(it.TxHist, it.TxRate)
		}
		next[c.name] = it
		ifaces = append(ifaces, it)
	}
	slices.SortStableFunc(ifaces, func(a, b Iface) int { return ifaceRank(a.Name) - ifaceRank(b.Name) })
	n.ifaces, n.at = next, now

	st.Ifaces = ifaces
	st.Iface, st.RxRate, st.TxRate, st.RxHist, st.TxHist = "", 0, 0, nil, nil
	if b := busiest(ifaces); b != nil {
		st.Iface, st.RxRate, st.TxRate, st.RxHist, st.TxHist = b.Name, b.RxRate, b.TxRate, b.RxHist, b.TxHist
	}
}

// busiest is the interface with the most traffic since the server
// started, as the one it talks through; never the loopback.
func busiest(ifaces []Iface) *Iface {
	var b *Iface
	for i := range ifaces {
		it := &ifaces[i]
		if it.Name != "lo" && (b == nil || it.rx+it.tx > b.rx+b.tx) {
			b = it
		}
	}
	return b
}

// NetView is what the network graph shows for a choice of interfaces.
type NetView struct {
	// Names are the interfaces shown, in the order of Stats.Ifaces: the
	// busiest alone when Auto.
	Names []string
	// Auto says the graph shows the busiest interface: nothing was
	// chosen, or none of the chosen is on the server (Missing).
	Auto bool
	// Missing are the chosen interfaces the server does not have.
	Missing []string
	RxRate  float64
	TxRate  float64
	RxHist  []float64
	TxHist  []float64
}

// Net is the network graph of the interfaces chosen: their sum, or the
// busiest interface when chosen is empty or names none the server has.
func (s *Stats) Net(chosen []string) NetView {
	var v NetView
	for _, name := range chosen {
		if !slices.ContainsFunc(s.Ifaces, func(it Iface) bool { return it.Name == name }) && !slices.Contains(v.Missing, name) {
			v.Missing = append(v.Missing, name)
		}
	}
	for _, it := range s.Ifaces {
		if !slices.Contains(chosen, it.Name) {
			continue
		}
		v.Names = append(v.Names, it.Name)
		v.RxRate += it.RxRate
		v.TxRate += it.TxRate
		v.RxHist = addHist(v.RxHist, it.RxHist)
		v.TxHist = addHist(v.TxHist, it.TxHist)
	}
	if len(v.Names) == 0 {
		v.Auto = true
		if s.Iface != "" {
			v.Names = []string{s.Iface}
		}
		v.RxRate, v.TxRate = s.RxRate, s.TxRate
		v.RxHist, v.TxHist = s.RxHist, s.TxHist
	}
	return v
}

// addHist is the sum of two histories, lined up at their ends, which are
// the same sample: one that started later adds to the latest samples.
func addHist(sum, h []float64) []float64 {
	if len(h) > len(sum) {
		sum, h = h, sum
	}
	out := slices.Clone(sum)
	off := len(out) - len(h)
	for i, v := range h {
		out[off+i] += v
	}
	return out
}

func cloneIfaces(ifaces []Iface) []Iface {
	out := slices.Clone(ifaces)
	for i := range out {
		out[i].RxHist = slices.Clone(out[i].RxHist)
		out[i].TxHist = slices.Clone(out[i].TxHist)
	}
	return out
}
