// Package geo tells where IP addresses are, for the Network tab: offline
// from ip2region's data (https://github.com/lionsoul2014/ip2region,
// Apache-2.0 or MIT), which is downloaded only when the user agrees, and,
// when the user turns it on, online from ip-api.com, with ip.sb when
// ip-api.com is busy. Nothing of it is in the app.
package geo

import (
	"net/netip"
	"strings"
)

// Location is where an IP address is: the parts ip2region and the online
// services give, each "" when unknown.
type Location struct {
	Country string `json:"country,omitempty"`
	Region  string `json:"region,omitempty"`
	City    string `json:"city,omitempty"`
	ISP     string `json:"isp,omitempty"`
}

// Complete says whether l has a city and an ISP: the addresses it does not
// are the ones looked up online, when that is on.
func (l Location) Complete() bool { return l.City != "" && l.ISP != "" }

// Fill is l with its empty parts taken from o.
func (l Location) Fill(o Location) Location {
	if l.Country == "" {
		l.Country = o.Country
	}
	if l.Region == "" {
		l.Region = o.Region
	}
	if l.City == "" {
		l.City = o.City
	}
	if l.ISP == "" {
		l.ISP = o.ISP
	}
	return l
}

// String is the parts of l, separated by spaces, as FinalShell shows
// them ("美国 新泽西州 Choopa"): a part that repeats the one before it, as
// a city-state's region and city do, is left out.
func (l Location) String() string {
	var parts []string
	for _, p := range []string{l.Country, l.Region, l.City, l.ISP} {
		if p == "" || len(parts) > 0 && parts[len(parts)-1] == p {
			continue
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, " ")
}

// cgnat is the shared address space of carrier-grade NAT (RFC 6598).
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// IsLAN says whether ip is in a range no service can locate: private
// (RFC 1918, IPv6 unique local), loopback, link-local, CGNAT, unspecified
// or multicast. These are never looked up, offline or online, and show
// as "LAN".
func IsLAN(ip netip.Addr) bool {
	ip = ip.Unmap()
	return !ip.IsValid() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsMulticast() ||
		ip.IsUnspecified() || cgnat.Contains(ip)
}
