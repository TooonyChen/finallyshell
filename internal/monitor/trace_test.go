package monitor

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestParseNextTrace(t *testing.T) {
	hops := ParseNextTrace(readFixture(t, "nexttrace-raw-cn.txt"))
	if len(hops) != 5 {
		t.Fatalf("%d hops: %+v", len(hops), hops)
	}
	if h := hops[0]; h.TTL != 1 || h.IP != "10.0.0.1" || len(h.RTTs) != 3 || h.RTTs[0] != 510*time.Microsecond ||
		!slices.Equal(h.Where, []string{"局域网"}) || h.ASN != "" {
		t.Errorf("hop 1: %+v", h)
	}
	if h := hops[1]; h.TTL != 2 || h.IP != "" || len(h.RTTs) != 0 {
		t.Errorf("hop 2: %+v", h)
	}
	// The first router that answered, with its own probes' times.
	if h := hops[2]; h.IP != "103.21.244.1" || h.Host != "ae1.mel01.example.net" || !slices.Equal(h.RTTs, []time.Duration{11200 * time.Microsecond}) ||
		h.ASN != "13335" || !slices.Equal(h.Where, []string{"澳大利亚", "维多利亚州", "墨尔本", "Cloudflare"}) {
		t.Errorf("hop 3: %+v", h)
	}
	if h := hops[3]; !slices.Equal(h.Where, []string{"中国", "福建省", "福州市", "电信"}) || h.ASN != "4134" || len(h.RTTs) != 3 || h.Host != "" {
		t.Errorf("hop 4: %+v", h)
	}
	en := ParseNextTrace(readFixture(t, "nexttrace-raw-en.txt"))
	if h := en[2]; !slices.Equal(h.Where, []string{"Melbourne", "Victoria", "Australia", "Cloudflare"}) {
		t.Errorf("hop 3 in English: %+v", h)
	}
	if h := en[4]; h.Host != "dns.google" || h.IP != "8.8.8.8" || !slices.Equal(h.Where, []string{"United States", "Google"}) || len(h.RTTs) != 3 {
		t.Errorf("hop 5: %+v", h)
	}

	// A line at a time, as they arrive: the header changes nothing, and
	// a TTL's first probe makes its hop.
	var got []TraceHop
	changes := 0
	for _, line := range strings.Split(readFixture(t, "nexttrace-raw-en.txt"), "\n") {
		var changed bool
		got, changed = addNextTraceLine(got, line)
		if changed {
			changes++
		}
		if strings.HasPrefix(line, "3|103.21.244.1|") && len(got) != 3 {
			t.Errorf("after hop 3's first probe: %+v", got)
		}
	}
	if changes != 15 {
		t.Errorf("%d changes", changes)
	}

	// A timeout in 12 columns; an answer after a timeout; a name with a
	// dot at the end; lines that are not probes.
	got = ParseNextTrace("6|*||||||||||\n" +
		"7|*||||||\n7|2001:db8::1|r1.example.|8.5|64500|德国|黑森州|法兰克福||Example GmbH|50.1109|8.6821\n" +
		"8|1.1.1.1|one.one|9.1|13335|\n" +
		"9 | not | a | probe | of | NextTrace | at | all\n" +
		"traceroute to 8.8.8.8, 30 hops max, 52 byte packets, ICMP mode\n" +
		"x|*||||||\n")
	if len(got) != 2 || got[0].TTL != 6 || got[0].IP != "" {
		t.Fatalf("hops %+v", got)
	}
	if h := got[1]; h.TTL != 7 || h.IP != "2001:db8::1" || h.Host != "r1.example" || h.ASN != "64500" ||
		!slices.Equal(h.RTTs, []time.Duration{8500 * time.Microsecond}) || !slices.Equal(h.Where, []string{"德国", "黑森州", "法兰克福", "Example GmbH"}) {
		t.Errorf("hop 7: %+v", h)
	}
	if got := ParseNextTrace("nexttrace: permission denied\n"); len(got) != 0 {
		t.Errorf("hops %+v", got)
	}
}

// TestTraceScript runs the trace script with this machine's sh and a
// NextTrace that writes its arguments: --traceroute when its --help has
// it, bare --raw before, the language, and the target as one argument.
func TestTraceScript(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil || runtime.GOOS == "windows" {
		t.Skip("no sh")
	}
	for _, tc := range []struct {
		help, target, lang string
		want               []string
	}{
		{"  -k  --traceroute  Use traditional traceroute mode", "8.8.8.8", "cn", []string{"--traceroute", "--raw", "-M", "-g", "cn", "8.8.8.8"}},
		{"  -M  --map  Disable Print Trace Map", "example.com", "en", []string{"--raw", "-M", "-g", "en", "example.com"}},
		// Not a target Trace takes, but quoted all the same.
		{"", "x'y;$(id)`id` \"z\"", "zh", []string{"--raw", "-M", "-g", "en", "x'y;$(id)`id` \"z\""}},
	} {
		dir := t.TempDir()
		fake := "#!/bin/sh\nif [ \"$1\" = --help ]; then echo 'Usage: nexttrace'; echo '" + tc.help + "'; exit 0; fi\n" +
			"echo 'NextTrace v1.6.4'\nfor a; do echo \"arg:$a\"; done\n"
		if err := os.WriteFile(filepath.Join(dir, "nexttrace"), []byte(fake), 0o755); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("sh", "-s")
		cmd.Stdin = strings.NewReader("PATH=" + dir + ":$PATH\n" + traceScript(tc.target, tc.lang))
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		var args []string
		for _, line := range strings.Split(string(out), "\n") {
			if a, ok := strings.CutPrefix(line, "arg:"); ok {
				args = append(args, a)
			}
		}
		if !slices.Equal(args, tc.want) || !strings.HasPrefix(string(out), netMark+"nexttrace---\n") {
			t.Errorf("help %q: %q", tc.help, out)
		}
	}
}

func TestParseTraceroute(t *testing.T) {
	var hops []TraceHop
	for _, line := range strings.Split(readFixture(t, "traceroute.txt"), "\n") {
		hops, _ = addTracerouteLine(hops, line)
	}
	if len(hops) != 5 {
		t.Fatalf("%d hops: %+v", len(hops), hops)
	}
	if h := hops[0]; h.TTL != 1 || h.IP != "10.0.0.1" || len(h.RTTs) != 3 || h.RTTs[1] != 430*time.Microsecond {
		t.Errorf("hop 1: %+v", h)
	}
	if h := hops[1]; h.IP != "" || len(h.RTTs) != 0 {
		t.Errorf("hop 2: %+v", h)
	}
	if h := hops[2]; h.IP != "103.21.244.1" || len(h.RTTs) != 1 {
		t.Errorf("hop 3: %+v", h)
	}
	if h := hops[3]; h.IP != "1.0.1.1" || len(h.RTTs) != 2 {
		t.Errorf("hop 4: %+v", h)
	}
}

func TestParseTracepath(t *testing.T) {
	var hops []TraceHop
	for _, line := range strings.Split(readFixture(t, "tracepath.txt"), "\n") {
		hops, _ = addTracepathLine(hops, line)
	}
	if len(hops) != 5 {
		t.Fatalf("%d hops: %+v", len(hops), hops)
	}
	if h := hops[0]; h.TTL != 1 || h.IP != "10.0.0.1" || len(h.RTTs) != 2 {
		t.Errorf("hop 1: %+v", h)
	}
	if h := hops[1]; h.TTL != 2 || h.IP != "" {
		t.Errorf("hop 2: %+v", h)
	}
	if h := hops[4]; h.IP != "8.8.8.8" || len(h.RTTs) != 1 || h.RTTs[0] != 44*time.Millisecond {
		t.Errorf("hop 5: %+v", h)
	}
}

func TestValidTraceTarget(t *testing.T) {
	for s, want := range map[string]bool{
		"8.8.8.8": true, "2606:4700::1111": true, "example.com": true, "a-b.example": true,
		"": false, "-x": false, "a b": false, "a;rm -rf /": false, "$(id)": false, "'x'": false, ".x": false,
		"fe80::1%eth0": false, "fe80::1%'$(id)'": false,
	} {
		if got := ValidTraceTarget(s); got != want {
			t.Errorf("%q: %v", s, got)
		}
	}
	if !strings.Contains(traceScript("8.8.8.8", "en"), "nexttrace --traceroute --raw -M -g en '8.8.8.8'") {
		t.Error(traceScript("8.8.8.8", "en"))
	}
}
