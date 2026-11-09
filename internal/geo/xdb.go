package geo

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strings"
	"sync"

	"github.com/lionsoul2014/ip2region/binding/golang/xdb"
)

// ErrChecksum is a data file whose SHA-256 is not the one the app keeps
// for it: damaged, or not the file the app asked for.
var ErrChecksum = errors.New("the location data's checksum does not match")

// DB is an ip2region data file (.xdb) of IPv4 or IPv6 addresses, read
// into memory, so that deleting the file never waits for it. It is safe
// for concurrent use.
type DB struct {
	mu   sync.Mutex // the searcher counts its reads
	s    *xdb.Searcher
	ipv6 bool
	// structure20 is the format before 2025, whose regions are
	// country|area|province|city|ISP; since, they are
	// country|province|city|ISP|ISO code.
	structure20 bool
}

// Load reads the data file at path, checks it against sum, the hex
// SHA-256 the app keeps for it (no check when ""), and opens it.
func Load(path, sum string) (*DB, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if sum != "" {
		got := sha256.Sum256(data)
		if !strings.EqualFold(hex.EncodeToString(got[:]), sum) {
			return nil, ErrChecksum
		}
	}
	return Open(data)
}

// Open opens the contents of a data file.
func Open(data []byte) (*DB, error) {
	h, err := xdb.LoadHeaderFromBuff(data)
	if err != nil {
		return nil, err
	}
	v, err := xdb.VersionFromHeader(h)
	if err != nil {
		return nil, err
	}
	if len(data) < xdb.HeaderInfoLength+xdb.VectorIndexRows*xdb.VectorIndexCols*xdb.VectorIndexSize {
		return nil, fmt.Errorf("the location data is too short (%d bytes)", len(data))
	}
	s, err := xdb.NewWithBuffer(v, data)
	if err != nil {
		return nil, err
	}
	return &DB{s: s, ipv6: v == xdb.IPv6, structure20: h.Version == xdb.Structure20}, nil
}

// IPv6 says whether d has IPv6 addresses rather than IPv4 ones.
func (d *DB) IPv6() bool { return d.ipv6 }

// Lookup tells where ip is, and whether d knows: an address of the other
// version, or one d has nothing about, is not known.
func (d *DB) Lookup(ip netip.Addr) (loc Location, ok bool) {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.Is6() != d.ipv6 {
		return Location{}, false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	defer func() {
		// A damaged file can point past its end: the searcher indexes
		// its buffer with what it reads.
		if recover() != nil {
			loc, ok = Location{}, false
		}
	}()
	b := ip.AsSlice()
	region, err := d.s.Search(b)
	if err != nil || region == "" {
		return Location{}, false
	}
	loc = parseRegion(region, d.structure20)
	return loc, loc != Location{}
}

// parseRegion reads a region of a data file, whose unknown parts are
// "0".
func parseRegion(region string, structure20 bool) Location {
	f := strings.Split(region, "|")
	for i := range f {
		if f[i] == "0" || strings.EqualFold(f[i], "Reserved") {
			f[i] = ""
		}
	}
	part := func(i int) string {
		if i < len(f) {
			return strings.TrimSpace(f[i])
		}
		return ""
	}
	if structure20 {
		return Location{Country: part(0), Region: part(2), City: part(3), ISP: part(4)}
	}
	return Location{Country: part(0), Region: part(1), City: part(2), ISP: part(3)}
}
