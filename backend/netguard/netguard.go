// Package netguard decides whether an IP address is allowed as the target of a request
// originating from the server (SSRF protection).
//
// The list used to live as isBlockedWebhookIP in
// handlers/subscriber.go. Fetching external status pages (package
// externalstatus) needs exactly the same check, but cannot
// import handlers. Hence central here; handlers delegates.
package netguard

import (
	"net"
	"net/netip"
)

// IsBlockedIP reports internal or non-publicly-routable addresses:
// loopback, private (RFC 1918, unique-local fc00::/7), link-local,
// unspecified, plus the IANA special-purpose ranges from
// blockedPrefixes (CGNAT, benchmark and documentation networks,
// reserved, multicast, NAT64, 6to4). IPv4-mapped IPv6 (::ffff:10.0.0.1)
// is unpacked beforehand. Transition formats that embed an IPv4 address
// (::/96, SIIT, NAT64, Teredo, 6to4) are blocked as whole ranges instead of
// checking the embedded address: as the target of a webhook or fetch
// there is nothing legitimate there, and a network stack with a matching gateway
// could otherwise reach an internal IPv4 target. An unreadable address
// counts as blocked.
func IsBlockedIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	return IsBlockedAddr(addr)
}

// IsBlockedAddr is IsBlockedIP for netip.Addr.
//
// An address with an IPv6 zone (fe80::1%eth0) is always considered blocked: netip.Prefix.Contains
// always returns false for zoned addresses, so 6to4, NAT64 or
// site-local addresses with an appended zone would otherwise slip past blockedPrefixes.
// For a public target there is no legitimate reason for
// a zone.
func IsBlockedAddr(addr netip.Addr) bool {
	if addr.Zone() != "" {
		return true
	}
	addr = addr.Unmap()
	if addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsUnspecified() {
		return true
	}
	for _, p := range blockedPrefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),       // "this network" (RFC 791)
	netip.MustParsePrefix("100.64.0.0/10"),   // carrier-grade NAT (RFC 6598)
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF Protocol Assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // TEST-NET-1
	netip.MustParsePrefix("198.18.0.0/15"),   // Benchmarking
	netip.MustParsePrefix("198.51.100.0/24"), // TEST-NET-2
	netip.MustParsePrefix("203.0.113.0/24"),  // TEST-NET-3
	netip.MustParsePrefix("224.0.0.0/4"),     // Multicast
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved incl. broadcast
	netip.MustParsePrefix("ff00::/8"),        // Multicast
	netip.MustParsePrefix("fec0::/10"),       // site-local (deprecated)
	netip.MustParsePrefix("::/96"),           // IPv4-compatible (deprecated), e.g. ::127.0.0.1
	netip.MustParsePrefix("::ffff:0:0:0/96"), // SIIT (RFC 7915), z. B. ::ffff:0:127.0.0.1
	netip.MustParsePrefix("64:ff9b::/96"),    // NAT64
	netip.MustParsePrefix("64:ff9b:1::/48"),  // local NAT64 (RFC 8215)
	netip.MustParsePrefix("2001::/32"),       // Teredo (RFC 4380)
	netip.MustParsePrefix("2002::/16"),       // 6to4
	netip.MustParsePrefix("2001:db8::/32"),   // documentation
}
