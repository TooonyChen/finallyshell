// Package monitor reads what the side panel shows about a Linux server:
// uptime, load, CPU, memory, processes, network, latency and disks.
package monitor

import (
	"bufio"
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// HistoryLen is how many samples the graphs keep.
const HistoryLen = 60

// Stats is what the side panel shows about a server.
type Stats struct {
	OK        bool // the server answered the script (Linux)
	Uptime    time.Duration
	Load      [3]float64
	CPU       float64 // percent, over the last interval
	MemUsed   uint64
	MemTotal  uint64
	SwapUsed  uint64
	SwapTotal uint64
	Procs     []Proc
	Iface     string
	RxRate    float64 // bytes per second
	TxRate    float64
	RxHist    []float64
	TxHist    []float64
	Latency   time.Duration
	LatHist   []float64 // milliseconds
	Disks     []Disk

	// Ifaces are every network interface, the loopback last; Net sums
	// those chosen. Iface and its rates and history are the busiest.
	Ifaces []Iface
}

type Proc struct {
	RSS  uint64 // bytes
	CPU  float64
	Name string
}

type Disk struct {
	Mount string
	Avail uint64
	Size  uint64
}

const sep = "---FINALLYSHELL---"

// statsScript reads everything in one round trip. It needs only /proc,
// df and ps, as on any Linux server.
const statsScript = `export LC_ALL=C
cat /proc/uptime; echo ` + sep + `
cat /proc/loadavg; echo ` + sep + `
head -1 /proc/stat; echo ` + sep + `
grep -E '^(MemTotal|MemAvailable|SwapTotal|SwapFree):' /proc/meminfo; echo ` + sep + `
cat /proc/net/dev; echo ` + sep + `
df -kP -x tmpfs -x devtmpfs -x overlay -x squashfs 2>/dev/null || df -kP; echo ` + sep + `
ps -eo rss=,pcpu=,comm= --sort=-rss 2>/dev/null | head -5
`

// scriptTimeout is how long a read of the stats waits for the server:
// one that takes longer, as when df hangs on a stale mount, is given up
// and the next read starts at the next interval.
const scriptTimeout = 10 * time.Second

// Run polls the server every interval and sends what it reads to
// publish, until ctx ends or the connection closes.
func Run(ctx context.Context, client *ssh.Client, interval time.Duration, publish func(Stats)) {
	run(ctx, client, interval, scriptTimeout, publish)
}

// run is Run, with each read of the stats given timeout.
func run(ctx context.Context, client *ssh.Client, interval, timeout time.Duration, publish func(Stats)) {
	var (
		st        Stats
		prevIdle  uint64
		prevTotal uint64
		net       netState
	)
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		start := time.Now()
		if _, _, err := client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
			return
		}
		st.Latency = time.Since(start)
		st.LatHist = pushHist(st.LatHist, float64(st.Latency.Microseconds())/1000)

		if out, err := runOutput(client, statsScript, timeout); err == nil {
			parts := strings.Split(out, sep+"\n")
			if len(parts) >= 7 {
				st.OK = true
				st.Uptime = parseUptime(parts[0])
				st.Load = parseLoad(parts[1])
				idle, total := parseCPU(parts[2])
				if prevTotal != 0 && total > prevTotal {
					st.CPU = 100 * (1 - float64(idle-prevIdle)/float64(total-prevTotal))
				}
				prevIdle, prevTotal = idle, total
				st.MemTotal, st.MemUsed, st.SwapTotal, st.SwapUsed = parseMem(parts[3])
				net.update(&st, parts[4], time.Now())
				st.Disks = parseDisks(parts[5])
				st.Procs = parseProcs(parts[6])
			}
		}
		publish(cloneStats(st))

		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// errScriptTimeout is a script that did not end in its time.
var errScriptTimeout = errors.New("the server did not answer in time")

// runOutput runs script in an exec channel of its own and returns what
// it prints, giving up after timeout: the channel is then closed, and
// the connection is left as it is.
func runOutput(client *ssh.Client, script string, timeout time.Duration) (string, error) {
	type result struct {
		out []byte
		err error
	}
	var (
		mu       sync.Mutex
		sess     *ssh.Session
		gaveUp   bool
		finished = make(chan result, 1)
	)
	go func() {
		s, err := client.NewSession()
		if err != nil {
			finished <- result{nil, err}
			return
		}
		mu.Lock()
		if gaveUp { // opened too late
			mu.Unlock()
			s.Close()
			return
		}
		sess = s
		mu.Unlock()
		defer s.Close()
		out, err := s.Output(script)
		finished <- result{out, err}
	}()
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case r := <-finished:
		return string(r.out), r.err
	case <-t.C:
		mu.Lock()
		gaveUp = true
		if sess != nil {
			sess.Close() // ends Output
		}
		mu.Unlock()
		return "", errScriptTimeout
	}
}

func pushHist(h []float64, v float64) []float64 {
	h = append(h, v)
	if len(h) > HistoryLen {
		h = h[len(h)-HistoryLen:]
	}
	return h
}

// cloneStats copies the slices, so the UI never reads what the poller
// is about to change.
func cloneStats(s Stats) Stats {
	s.Procs = append([]Proc(nil), s.Procs...)
	s.Ifaces = cloneIfaces(s.Ifaces)
	s.RxHist = append([]float64(nil), s.RxHist...)
	s.TxHist = append([]float64(nil), s.TxHist...)
	s.LatHist = append([]float64(nil), s.LatHist...)
	s.Disks = append([]Disk(nil), s.Disks...)
	return s
}

func parseUptime(s string) time.Duration {
	f := strings.Fields(s)
	if len(f) == 0 {
		return 0
	}
	secs, _ := strconv.ParseFloat(f[0], 64)
	return time.Duration(secs * float64(time.Second))
}

func parseLoad(s string) (l [3]float64) {
	f := strings.Fields(s)
	for i := 0; i < 3 && i < len(f); i++ {
		l[i], _ = strconv.ParseFloat(f[i], 64)
	}
	return l
}

// parseCPU reads the first line of /proc/stat: idle (with iowait) and
// total jiffies.
func parseCPU(s string) (idle, total uint64) {
	f := strings.Fields(s)
	if len(f) < 5 || f[0] != "cpu" {
		return 0, 0
	}
	for i, v := range f[1:] {
		n, _ := strconv.ParseUint(v, 10, 64)
		if i >= 8 { // guest time is already counted in user
			break
		}
		total += n
		if i == 3 || i == 4 {
			idle += n
		}
	}
	return idle, total
}

func parseMem(s string) (memTotal, memUsed, swapTotal, swapUsed uint64) {
	vals := map[string]uint64{}
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) >= 2 {
			n, _ := strconv.ParseUint(f[1], 10, 64)
			vals[strings.TrimSuffix(f[0], ":")] = n * 1024
		}
	}
	memTotal = vals["MemTotal"]
	if a := vals["MemAvailable"]; a <= memTotal {
		memUsed = memTotal - a
	}
	swapTotal = vals["SwapTotal"]
	if f := vals["SwapFree"]; f <= swapTotal {
		swapUsed = swapTotal - f
	}
	return
}

func parseDisks(s string) []Disk {
	var disks []Disk
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 6 || f[0] == "Filesystem" {
			continue
		}
		size, err1 := strconv.ParseUint(f[1], 10, 64)
		avail, err2 := strconv.ParseUint(f[3], 10, 64)
		if err1 != nil || err2 != nil || size == 0 {
			continue
		}
		disks = append(disks, Disk{Mount: strings.Join(f[5:], " "), Avail: avail * 1024, Size: size * 1024})
	}
	return disks
}

func parseProcs(s string) []Proc {
	var procs []Proc
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 3 {
			continue
		}
		rss, _ := strconv.ParseUint(f[0], 10, 64)
		cpu, _ := strconv.ParseFloat(f[1], 64)
		procs = append(procs, Proc{RSS: rss * 1024, CPU: cpu, Name: strings.Join(f[2:], " ")})
	}
	return procs
}
