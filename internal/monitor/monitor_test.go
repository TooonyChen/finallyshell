package monitor

import "testing"

func TestParse(t *testing.T) {
	idle, total := parseCPU("cpu  100 0 50 800 50 0 0 0 0 0")
	if idle != 850 || total != 1000 {
		t.Errorf("cpu %d %d", idle, total)
	}
	mt, mu, st, su := parseMem("MemTotal: 1000 kB\nMemAvailable: 250 kB\nSwapTotal: 100 kB\nSwapFree: 100 kB\n")
	if mt != 1000*1024 || mu != 750*1024 || st != 100*1024 || su != 0 {
		t.Errorf("mem %d %d %d %d", mt, mu, st, su)
	}
	iface, rx, tx := parseNet("Inter-|   Receive\n face |bytes\n    lo: 999999 1 0 0 0 0 0 0 999999 1 0 0 0 0 0 0\n  eth0: 5000 10 0 0 0 0 0 0 3000 8 0 0 0 0 0 0\n")
	if iface != "eth0" || rx != 5000 || tx != 3000 {
		t.Errorf("net %s %d %d", iface, rx, tx)
	}
	d := parseDisks("Filesystem 1024-blocks Used Available Capacity Mounted on\n/dev/sda1 1000 400 600 40% /\n")
	if len(d) != 1 || d[0].Mount != "/" || d[0].Avail != 600*1024 {
		t.Errorf("disks %+v", d)
	}
	p := parseProcs("  2000 3.5 java\n 100 0.1 my daemon\n")
	if len(p) != 2 || p[0].RSS != 2000*1024 || p[1].Name != "my daemon" {
		t.Errorf("procs %+v", p)
	}
	if l := parseLoad("0.28 0.31 0.27 1/200 999"); l != [3]float64{0.28, 0.31, 0.27} {
		t.Errorf("load %v", l)
	}
}
