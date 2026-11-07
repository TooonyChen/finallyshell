package monitor

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func readSample(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestParseSysInfoSamples(t *testing.T) {
	tests := []struct {
		file string
		want SysInfo
	}{
		{"sysinfo-debian.txt", SysInfo{
			Hostname: "web-1", OS: "Debian GNU/Linux 12 (bookworm)", KernelOS: "Linux", Kernel: "6.1.0-18-amd64", Arch: "x86_64",
			CPUModel: "Intel(R) Xeon(R) CPU E5-2680 v4 @ 2.40GHz", CPUs: 4, Cores: 2,
			MemTotal: 4009216 * 1024, SwapTotal: 2306044 * 1024, Uptime: 35425043 * time.Second,
			Addrs: []Addr{{"ens3", "45.56.78.90/24"}, {"ens3", "2600:3c01::f03c:93ff:fe4a:1b2c/64"}, {"docker0", "172.17.0.1/16"}},
			Disks: []BlockDevice{{Name: "vda", Size: 52428800 * 512}},
		}},
		// BusyBox: no lscpu, an arm64 cpuinfo without "model name".
		{"sysinfo-alpine.txt", SysInfo{
			Hostname: "pi-gate", OS: "Alpine Linux v3.20", KernelOS: "Linux", Kernel: "6.6.31-0-rpi", Arch: "aarch64",
			CPUModel: "Raspberry Pi 4 Model B Rev 1.4", CPUs: 4,
			MemTotal: 3884296 * 1024, Uptime: 93785 * time.Second,
			Addrs: []Addr{{"eth0", "192.168.1.40/24"}, {"wlan0", "192.168.1.41/24"}},
			Disks: []BlockDevice{{Name: "mmcblk0", Size: 62333952 * 512}, {Name: "sda", Size: 1953525168 * 512, Model: "Extreme SSD"}},
		}},
		// CentOS 6: no os-release, two sockets with hyper-threading.
		{"sysinfo-centos.txt", SysInfo{
			Hostname: "db-legacy.example.com", OS: "CentOS release 6.10 (Final)", KernelOS: "Linux", Kernel: "2.6.32-754.35.1.el6.x86_64", Arch: "x86_64",
			CPUModel: "Intel(R) Xeon(R) CPU E5620 @ 2.40GHz", CPUs: 8, Cores: 4,
			MemTotal: 16330708 * 1024, SwapTotal: 8191996 * 1024, Uptime: 4021 * time.Second,
			Addrs: []Addr{{"eth0", "10.10.1.20/24"}, {"eth0", "10.10.1.21/24"}},
			Disks: []BlockDevice{{Name: "sda", Size: 209715200 * 512, Model: "Virtual disk"}, {Name: "sdb", Size: 1073741824 * 512, Model: "Virtual disk"}},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.file, func(t *testing.T) {
			got := ParseSysInfo(readSample(t, tc.file))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got  %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

// A server without most tools still gives what it has.
func TestParseSysInfoMissingTools(t *testing.T) {
	out := "---FINALSHELLY:hostname---\nbox\n---FINALSHELLY:uname---\nLinux\n5.10.0\nmips\n" +
		"---FINALSHELLY:os-release---\n---FINALSHELLY:release---\n---FINALSHELLY:cpuinfo---\n" +
		"processor\t: 0\ncpu model\t: MIPS 24Kc V7.4\n---FINALSHELLY:nproc---\n---FINALSHELLY:addr---\n" +
		"---FINALSHELLY:disks---\n---FINALSHELLY:end---\n"
	got := ParseSysInfo(out)
	want := SysInfo{Hostname: "box", OS: "Linux", KernelOS: "Linux", Kernel: "5.10.0", Arch: "mips", CPUModel: "MIPS 24Kc V7.4", CPUs: 1}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
	if got := ParseSysInfo(""); !reflect.DeepEqual(got, SysInfo{}) {
		t.Errorf("empty output: %+v", got)
	}
}

func TestParseAddrsIfconfig(t *testing.T) {
	busybox := `eth0      Link encap:Ethernet  HWaddr 02:FC:00:00:00:01
          inet addr:192.0.2.2  Bcast:192.0.2.255  Mask:255.255.255.0
          inet6 addr: 2001:db8::2/64 Scope:Global
          inet6 addr: fe80::fc:ff:fe00:1/64 Scope:Link
          UP BROADCAST RUNNING MULTICAST  MTU:1400  Metric:1

lo        Link encap:Local Loopback
          inet addr:127.0.0.1  Mask:255.0.0.0
`
	netTools := `eth0: flags=4163<UP,BROADCAST,RUNNING,MULTICAST>  mtu 1400
        inet 192.0.2.2  netmask 255.255.255.0  broadcast 192.0.2.255
        inet6 2001:db8::2  prefixlen 64  scopeid 0x0<global>
        inet6 fe80::fc:ff:fe00:1  prefixlen 64  scopeid 0x20<link>
        ether 02:fc:00:00:00:01  txqueuelen 1000  (Ethernet)

lo: flags=73<UP,LOOPBACK,RUNNING>  mtu 65536
        inet 127.0.0.1  netmask 255.0.0.0
`
	want := []Addr{{"eth0", "192.0.2.2/24"}, {"eth0", "2001:db8::2/64"}}
	for name, s := range map[string]string{"busybox": busybox, "net-tools": netTools} {
		if got := parseAddrs(s); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %+v", name, got)
		}
	}
	if got := maskBits("0xffffff00"); got != 24 {
		t.Errorf("hex mask: %d", got)
	}
}

func TestSysInfoScriptSections(t *testing.T) {
	// Each section the parser reads is one the script writes.
	for _, name := range []string{"hostname", "uname", "os-release", "release", "cpuinfo", "lscpu", "nproc", "meminfo", "uptime", "addr", "disks", "end"} {
		if !strings.Contains(sysInfoScript, "m "+name+";") && !strings.Contains(sysInfoScript, "m "+name+"\n") {
			t.Errorf("the script has no %s section", name)
		}
	}
	if got := cleanIssue(`Ubuntu 24.04.5 LTS \n \l`); got != "Ubuntu 24.04.5 LTS" {
		t.Errorf("issue %q", got)
	}
}
