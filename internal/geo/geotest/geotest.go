// Package geotest makes small ip2region data files (.xdb) for tests, so
// that no test downloads the real ones. It writes the format of
// ip2region's maker (structure 3.0), with the segments given.
package geotest

import (
	"encoding/binary"
	"net/netip"
	"time"
)

// Segment is a range of addresses and its region, as ip2region's source
// files have them: "country|province|city|ISP|ISO code", "0" for
// unknown.
type Segment struct {
	Start, End string
	Region     string
}

const (
	headerLen   = 256
	vectorCells = 256 * 256
	vectorSize  = 8
)

// Xdb is the contents of a data file of segs, all IPv4 or all IPv6, in
// order and not overlapping.
func Xdb(segs []Segment) []byte {
	ipv6 := len(segs) > 0 && netip.MustParseAddr(segs[0].Start).Is6()
	n := 4
	if ipv6 {
		n = 16
	}
	entrySize := 2*n + 6

	out := make([]byte, headerLen+vectorCells*vectorSize)
	binary.LittleEndian.PutUint16(out[0:], 3) // structure 3.0
	binary.LittleEndian.PutUint16(out[2:], 1) // vector index
	binary.LittleEndian.PutUint32(out[4:], uint32(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC).Unix()))
	version := uint16(4)
	if ipv6 {
		version = 6
	}
	binary.LittleEndian.PutUint16(out[16:], version)
	binary.LittleEndian.PutUint16(out[18:], 4) // 4-byte pointers

	// The regions, each once.
	ptr := map[string]uint32{}
	for _, s := range segs {
		if _, ok := ptr[s.Region]; !ok {
			ptr[s.Region] = uint32(len(out))
			out = append(out, s.Region...)
		}
	}

	// The index: each segment split at the /16 boundaries (of the first
	// two bytes), which the vector index points into.
	put := func(b []byte, ip []byte) {
		if ipv6 {
			copy(b, ip)
			return
		}
		for i := range ip { // IPv4 is little-endian
			b[i] = ip[len(ip)-1-i]
		}
	}
	first, last := -1, -1
	for _, s := range segs {
		start := netip.MustParseAddr(s.Start).AsSlice()
		end := netip.MustParseAddr(s.End).AsSlice()
		for {
			cellEnd := append([]byte(nil), start...)
			for i := 2; i < len(cellEnd); i++ {
				cellEnd[i] = 0xff
			}
			e := end
			if compare(cellEnd, end) < 0 {
				e = cellEnd
			}
			pos := len(out)
			entry := make([]byte, entrySize)
			put(entry, start)
			put(entry[n:], e)
			binary.LittleEndian.PutUint16(entry[2*n:], uint16(len(s.Region)))
			binary.LittleEndian.PutUint32(entry[2*n+2:], ptr[s.Region])
			out = append(out, entry...)
			cell := headerLen + (int(start[0])*256+int(start[1]))*vectorSize
			if binary.LittleEndian.Uint32(out[cell:]) == 0 {
				binary.LittleEndian.PutUint32(out[cell:], uint32(pos))
			}
			binary.LittleEndian.PutUint32(out[cell+4:], uint32(pos+entrySize))
			if first < 0 {
				first = pos
			}
			last = pos
			if compare(e, end) >= 0 {
				break
			}
			start = next(e)
		}
	}
	binary.LittleEndian.PutUint32(out[8:], uint32(first))
	binary.LittleEndian.PutUint32(out[12:], uint32(last))
	return out
}

func compare(a, b []byte) int {
	for i := range a {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// next is ip plus one.
func next(ip []byte) []byte {
	out := append([]byte(nil), ip...)
	for i := len(out) - 1; i >= 0; i-- {
		out[i]++
		if out[i] != 0 {
			break
		}
	}
	return out
}

// V4 is a small IPv4 data file: a few addresses of the tests, with the
// names ip2region gives them.
func V4() []byte {
	return Xdb([]Segment{
		{"1.0.1.0", "1.0.3.255", "中国|福建省|福州市|电信|CN"},
		{"8.8.8.0", "8.8.8.255", "美国|0|0|Google|US"},
		{"45.56.64.0", "45.56.127.255", "美国|新泽西州|0|0|US"},
		{"103.21.244.0", "103.21.247.255", "Australia|Victoria|Melbourne|Cloudflare|AU"},
		{"114.114.0.0", "114.114.255.255", "中国|江苏省|南京市|0|CN"},
		{"203.0.113.0", "203.0.113.255", "日本|东京都|东京|IIJ|JP"},
	})
}

// V6 is a small IPv6 data file.
func V6() []byte {
	return Xdb([]Segment{
		{"2001:200::", "2001:200:ffff:ffff:ffff:ffff:ffff:ffff", "Japan|Tokyo|Tokyo|WIDE Project|JP"},
		{"2606:4700::", "2606:4700:ffff:ffff:ffff:ffff:ffff:ffff", "美国|加利福尼亚州|旧金山|Cloudflare|US"},
	})
}
