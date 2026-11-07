package monitor

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// SysInfo is what the system info popover shows about a server: what it
// is, rather than how busy it is.
type SysInfo struct {
	Hostname  string
	OS        string // PRETTY_NAME of os-release, or the first line of a release file
	KernelOS  string // uname -s: Linux
	Kernel    string // uname -r
	Arch      string // uname -m
	CPUModel  string
	CPUs      int // logical processors
	Cores     int // physical cores, 0 when unknown
	MemTotal  uint64
	SwapTotal uint64
	Uptime    time.Duration
	Addrs     []Addr
	Disks     []BlockDevice
}

// Addr is an address of the server, with its prefix length: 10.0.0.5/24.
type Addr struct {
	Iface string
	Addr  string
}

// BlockDevice is a whole disk, as /sys/block lists it.
type BlockDevice struct {
	Name  string
	Size  uint64 // bytes
	Model string
}

// sysInfoMark starts each section of sysInfoScript's output.
const sysInfoMark = "---FINALSHELLY:"

// sysInfoScript reads everything in one round trip. It is POSIX sh, and
// runs with BusyBox's tools: what a server lacks (lscpu, nproc, ip, an
// os-release) leaves its section empty or falls back to another tool.
const sysInfoScript = `export LC_ALL=C
m() { echo "` + sysInfoMark + `$1---"; }
m hostname; hostname 2>/dev/null || uname -n
m uname; uname -s; uname -r; uname -m
m os-release; { cat /etc/os-release || cat /usr/lib/os-release; } 2>/dev/null | grep -E '^(PRETTY_NAME|NAME|VERSION)='
m release; head -n 1 /etc/redhat-release 2>/dev/null || head -n 1 /etc/issue 2>/dev/null
m cpuinfo; grep -E '^(processor|model name|physical id|core id|Hardware|Model|cpu model|Processor)[[:space:]]*:' /proc/cpuinfo 2>/dev/null
m lscpu; lscpu 2>/dev/null | grep -E '^[[:space:]]*(Model name|Socket\(s\)|Core\(s\) per socket):'
m nproc; nproc 2>/dev/null || getconf _NPROCESSORS_ONLN 2>/dev/null
m meminfo; grep -E '^(MemTotal|SwapTotal):' /proc/meminfo 2>/dev/null
m uptime; cat /proc/uptime 2>/dev/null
m addr; ip -o addr show 2>/dev/null || ifconfig -a 2>/dev/null
m disks; for d in /sys/block/*; do
  [ -r "$d/size" ] || continue
  n=${d##*/}
  case $n in loop*|ram*|zram*|dm-*|sr*|fd*|nbd*) continue;; esac
  echo "$n $(cat "$d/size") $(cat "$d/device/model" 2>/dev/null)"
done
m end
`

// ErrSysInfoOutput is a server whose answer to the script is not the
// script's: it has no POSIX shell.
var ErrSysInfoOutput = errors.New("the server did not run the script")

// ReadSysInfo runs the script once over a new exec channel of client, and
// stops it when ctx ends.
func ReadSysInfo(ctx context.Context, client *ssh.Client) (SysInfo, error) {
	s, err := client.NewSession()
	if err != nil {
		return SysInfo{}, err
	}
	defer s.Close()
	stop := context.AfterFunc(ctx, func() { s.Close() })
	defer stop()
	// The script goes on stdin, so the login shell, which may be fish or
	// csh, only has to start sh.
	s.Stdin = strings.NewReader(sysInfoScript)
	out, err := s.StdoutPipe()
	if err != nil {
		return SysInfo{}, err
	}
	if err := s.Start("sh -s"); err != nil {
		return SysInfo{}, err
	}
	data, rerr := io.ReadAll(io.LimitReader(out, 1<<20))
	werr := s.Wait()
	if ctx.Err() != nil {
		return SysInfo{}, ctx.Err()
	}
	text := string(data)
	if !strings.Contains(text, sysInfoMark+"end---") {
		switch {
		case rerr != nil:
			return SysInfo{}, rerr
		case werr != nil:
			return SysInfo{}, werr
		}
		return SysInfo{}, ErrSysInfoOutput
	}
	return ParseSysInfo(text), nil
}

// sysInfoSections splits the script's output by its marks.
func sysInfoSections(out string) map[string]string {
	sections := map[string]string{}
	name := ""
	var b strings.Builder
	flush := func() {
		if name != "" {
			sections[name] = b.String()
		}
		b.Reset()
	}
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if rest, ok := strings.CutPrefix(line, sysInfoMark); ok && strings.HasSuffix(rest, "---") {
			flush()
			name = strings.TrimSuffix(rest, "---")
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	flush()
	return sections
}

// ParseSysInfo reads the output of the script. What is missing stays
// empty.
func ParseSysInfo(out string) SysInfo {
	sec := sysInfoSections(out)
	var info SysInfo
	info.Hostname = firstLine(sec["hostname"])
	u := lines(sec["uname"])
	for i, p := range []*string{&info.KernelOS, &info.Kernel, &info.Arch} {
		if i < len(u) {
			*p = u[i]
		}
	}
	info.OS = parseOSRelease(sec["os-release"])
	if info.OS == "" {
		info.OS = cleanIssue(firstLine(sec["release"]))
	}
	if info.OS == "" {
		info.OS = info.KernelOS
	}
	info.CPUModel, info.CPUs, info.Cores = parseCPUInfo(sec["cpuinfo"])
	model, sockets, perSocket := parseLscpu(sec["lscpu"])
	if info.CPUModel == "" {
		info.CPUModel = model
	}
	if info.Cores == 0 && sockets > 0 && perSocket > 0 {
		info.Cores = sockets * perSocket
	}
	if n, err := strconv.Atoi(firstLine(sec["nproc"])); err == nil && n > 0 {
		info.CPUs = n
	}
	mem := parseMeminfo(sec["meminfo"])
	info.MemTotal, info.SwapTotal = mem["MemTotal"], mem["SwapTotal"]
	info.Uptime = parseUptime(sec["uptime"]).Round(time.Second)
	info.Addrs = parseAddrs(sec["addr"])
	info.Disks = parseBlockDevices(sec["disks"])
	return info
}

func lines(s string) []string {
	var out []string
	for l := range strings.Lines(s) {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func firstLine(s string) string {
	if l := lines(s); len(l) > 0 {
		return l[0]
	}
	return ""
}

// parseOSRelease returns PRETTY_NAME, or NAME and VERSION.
func parseOSRelease(s string) string {
	vals := map[string]string{}
	for _, l := range lines(s) {
		k, v, ok := strings.Cut(l, "=")
		if !ok {
			continue
		}
		if uq, err := strconv.Unquote(v); err == nil {
			v = uq
		} else {
			v = strings.Trim(v, `'"`)
		}
		vals[k] = v
	}
	if v := vals["PRETTY_NAME"]; v != "" {
		return v
	}
	return strings.TrimSpace(vals["NAME"] + " " + vals["VERSION"])
}

// cleanIssue drops the escapes getty expands in /etc/issue: "Welcome to
// Alpine Linux 3.19\n Kernel \r on an \m (\l)".
func cleanIssue(s string) string {
	if i := strings.Index(s, `\`); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// parseCPUInfo reads the lines of /proc/cpuinfo the script keeps: the
// model, the number of processors, and the physical cores, counted by
// their physical and core ids where the kernel gives them.
func parseCPUInfo(s string) (model string, cpus, cores int) {
	byKey := map[string]string{}
	type core struct{ pkg, id string }
	seen := map[core]bool{}
	var pkg string
	for _, l := range lines(s) {
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "processor":
			cpus++
		case "physical id":
			pkg = v
		case "core id":
			seen[core{pkg, v}] = true
		default:
			if _, ok := byKey[k]; !ok && v != "" {
				byKey[k] = v
			}
		}
	}
	for _, k := range []string{"model name", "cpu model", "Processor", "Hardware", "Model"} {
		if v := byKey[k]; v != "" {
			model = strings.Join(strings.Fields(v), " ")
			break
		}
	}
	return model, cpus, len(seen)
}

func parseLscpu(s string) (model string, sockets, perSocket int) {
	for _, l := range lines(s) {
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch k {
		case "Model name":
			if v != "-" {
				model = strings.Join(strings.Fields(v), " ")
			}
		case "Socket(s)":
			sockets, _ = strconv.Atoi(v)
		case "Core(s) per socket":
			perSocket, _ = strconv.Atoi(v)
		}
	}
	return
}

// parseMeminfo reads lines of /proc/meminfo, in bytes.
func parseMeminfo(s string) map[string]uint64 {
	vals := map[string]uint64{}
	for _, l := range lines(s) {
		f := strings.Fields(l)
		if len(f) >= 2 {
			n, _ := strconv.ParseUint(f[1], 10, 64)
			vals[strings.TrimSuffix(f[0], ":")] = n * 1024
		}
	}
	return vals
}

// parseAddrs reads `ip -o addr show`, or ifconfig's output where there is
// no ip: net-tools' old and new formats, and BusyBox's. Loopback and
// link-local addresses are left out.
func parseAddrs(s string) []Addr {
	var addrs []Addr
	add := func(iface, ip string, prefix int) {
		iface = strings.TrimSuffix(iface, ":")
		if i := strings.IndexByte(iface, '@'); i >= 0 {
			iface = iface[:i]
		}
		if i := strings.IndexByte(ip, '%'); i >= 0 { // fe80::1%eth0
			ip = ip[:i]
		}
		parsed := net.ParseIP(ip)
		if parsed == nil || parsed.IsLoopback() || parsed.IsLinkLocalUnicast() || iface == "lo" {
			return
		}
		a := Addr{Iface: iface, Addr: ip}
		if prefix >= 0 {
			a.Addr += "/" + strconv.Itoa(prefix)
		}
		addrs = append(addrs, a)
	}
	if isIPOneLine(s) {
		for _, l := range lines(s) {
			f := strings.Fields(l)
			if len(f) < 4 || (f[2] != "inet" && f[2] != "inet6") {
				continue
			}
			ip, pfx, _ := strings.Cut(f[3], "/")
			prefix, err := strconv.Atoi(pfx)
			if err != nil {
				prefix = -1
			}
			add(f[1], ip, prefix)
		}
		return addrs
	}
	iface := ""
	for l := range strings.Lines(s) {
		if strings.TrimSpace(l) == "" {
			continue
		}
		f := strings.Fields(l)
		if l[0] != ' ' && l[0] != '\t' {
			iface = f[0]
			continue
		}
		if len(f) < 2 || (f[0] != "inet" && f[0] != "inet6") {
			continue
		}
		var ip string
		prefix := -1
		rest := f[1:]
		switch {
		case f[1] == "addr:" && len(f) > 2: // inet6 addr: fe80::1/64 Scope:Link
			ip, rest = f[2], f[3:]
		case strings.HasPrefix(f[1], "addr:"): // inet addr:10.0.0.5  Bcast:...  Mask:255.255.255.0
			ip, rest = strings.TrimPrefix(f[1], "addr:"), f[2:]
		default: // inet 10.0.0.5  netmask 255.255.255.0
			ip, rest = f[1], f[2:]
		}
		if a, p, ok := strings.Cut(ip, "/"); ok {
			ip = a
			if n, err := strconv.Atoi(p); err == nil {
				prefix = n
			}
		}
		for i, w := range rest {
			switch {
			case strings.HasPrefix(w, "Mask:"):
				prefix = maskBits(strings.TrimPrefix(w, "Mask:"))
			case (w == "netmask" || w == "prefixlen") && i+1 < len(rest):
				if w == "netmask" {
					prefix = maskBits(rest[i+1])
				} else if n, err := strconv.Atoi(rest[i+1]); err == nil {
					prefix = n
				}
			}
		}
		add(iface, ip, prefix)
	}
	return addrs
}

// isIPOneLine tells whether s is `ip -o addr`'s output: "2: eth0    inet
// 10.0.0.5/24 ...".
func isIPOneLine(s string) bool {
	f := strings.Fields(firstLine(s))
	if len(f) < 3 || !strings.HasSuffix(f[0], ":") {
		return false
	}
	_, err := strconv.Atoi(strings.TrimSuffix(f[0], ":"))
	return err == nil
}

// maskBits is the prefix length of a dotted netmask, or of 0xffffff00 as
// BSD's ifconfig writes it; -1 when it is not one.
func maskBits(m string) int {
	var mask net.IPMask
	if h, ok := strings.CutPrefix(m, "0x"); ok {
		n, err := strconv.ParseUint(h, 16, 32)
		if err != nil {
			return -1
		}
		mask = net.IPv4Mask(byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	} else if ip := net.ParseIP(m).To4(); ip != nil {
		mask = net.IPMask(ip)
	} else {
		return -1
	}
	ones, bits := mask.Size()
	if bits == 0 {
		return -1
	}
	return ones
}

// parseBlockDevices reads the script's lines of /sys/block: a name, a
// size in 512-byte sectors, and a model.
func parseBlockDevices(s string) []BlockDevice {
	var disks []BlockDevice
	for _, l := range lines(s) {
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		sectors, err := strconv.ParseUint(f[1], 10, 64)
		if err != nil || sectors == 0 {
			continue
		}
		disks = append(disks, BlockDevice{Name: f[0], Size: sectors * 512, Model: strings.Join(f[2:], " ")})
	}
	return disks
}
