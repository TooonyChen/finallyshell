package monitor

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestParseNextTrace(t *testing.T) {
	out := "warning: something NextTrace says first\n" + readFixture(t, "nexttrace.json")
	hops, err := ParseNextTrace([]byte(out), "cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(hops) != 5 {
		t.Fatalf("%d hops", len(hops))
	}
	if h := hops[0]; h.TTL != 1 || h.IP != "10.0.0.1" || len(h.RTTs) != 3 || h.RTTs[0] != 512*time.Microsecond || !slices.Equal(h.Where, []string{"局域网"}) {
		t.Errorf("hop 1: %+v", h)
	}
	if h := hops[1]; h.TTL != 2 || h.IP != "" || len(h.RTTs) != 0 {
		t.Errorf("hop 2: %+v", h)
	}
	// The first router that answered, with its own probes' times.
	if h := hops[2]; h.IP != "103.21.244.1" || h.Host != "ae1.mel01.example.net" || len(h.RTTs) != 1 || h.ASN != "13335" ||
		!slices.Equal(h.Where, []string{"澳大利亚", "维多利亚州", "墨尔本", "Cloudflare"}) {
		t.Errorf("hop 3: %+v", h)
	}
	if h := hops[3]; !slices.Equal(h.Where, []string{"中国", "福建省", "福州市", "电信"}) || h.ASN != "4134" {
		t.Errorf("hop 4: %+v", h)
	}
	en, _ := ParseNextTrace([]byte(out), "en")
	if h := en[3]; !slices.Equal(h.Where, []string{"China", "Fujian", "Fuzhou", "电信"}) {
		t.Errorf("hop 4 in English: %+v", h)
	}
	if h := en[4]; h.Host != "dns.google" || !slices.Equal(h.Where, []string{"United States", "Google"}) {
		t.Errorf("hop 5: %+v", h)
	}
	if _, err := ParseNextTrace([]byte("nexttrace: permission denied\n"), "en"); err == nil {
		t.Error("no JSON, no error")
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
	} {
		if got := ValidTraceTarget(s); got != want {
			t.Errorf("%q: %v", s, got)
		}
	}
	if !strings.Contains(traceScript("8.8.8.8"), "nexttrace --json -M '8.8.8.8'") {
		t.Error(traceScript("8.8.8.8"))
	}
}
