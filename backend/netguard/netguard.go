// Package netguard entscheidet, ob eine IP-Adresse als Ziel eines vom Server
// ausgehenden Requests erlaubt ist (SSRF-Schutz).
//
// Die Liste lag bisher als isBlockedWebhookIP in
// handlers/subscriber.go. Der Abruf externer Statusseiten (Paket
// externalstatus) braucht exakt dieselbe Prüfung, kann handlers aber nicht
// importieren. Deshalb hier zentral; handlers delegiert.
package netguard

import (
	"net"
	"net/netip"
)

// IsBlockedIP meldet interne bzw. nicht öffentlich routbare Adressen:
// Loopback, privat (RFC 1918, Unique-Local fc00::/7), Link-Local,
// unspezifiziert, dazu die IANA-Special-Purpose-Bereiche aus
// blockedPrefixes (CGNAT, Benchmark- und Dokumentationsnetze,
// reserviert, Multicast, NAT64, 6to4). IPv4-mapped IPv6 (::ffff:10.0.0.1)
// wird vorher entpackt. Übergangsformate, die eine IPv4-Adresse einbetten
// (::/96, SIIT, NAT64, Teredo, 6to4), sind als ganze Bereiche gesperrt statt
// die eingebettete Adresse zu prüfen: Als Ziel eines Webhooks oder Abrufs
// gibt es dort nichts Legitimes, und ein Netzstack mit passendem Gateway
// könnte sonst ein internes IPv4-Ziel erreichen. Eine nicht lesbare Adresse
// gilt als gesperrt.
func IsBlockedIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	return IsBlockedAddr(addr)
}

// IsBlockedAddr ist IsBlockedIP für netip.Addr.
//
// Eine Adresse mit IPv6-Zone (fe80::1%eth0) gilt immer als gesperrt: netip.Prefix.Contains
// liefert für zonenbehaftete Adressen stets false, sodass 6to4-, NAT64- oder
// Site-Local-Adressen mit angehängter Zone sonst an blockedPrefixes
// vorbeikämen. Für ein öffentliches Ziel gibt es keinen legitimen Grund für
// eine Zone.
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
	netip.MustParsePrefix("0.0.0.0/8"),       // "dieses Netz" (RFC 791)
	netip.MustParsePrefix("100.64.0.0/10"),   // Carrier-Grade-NAT (RFC 6598)
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF Protocol Assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // TEST-NET-1
	netip.MustParsePrefix("198.18.0.0/15"),   // Benchmarking
	netip.MustParsePrefix("198.51.100.0/24"), // TEST-NET-2
	netip.MustParsePrefix("203.0.113.0/24"),  // TEST-NET-3
	netip.MustParsePrefix("224.0.0.0/4"),     // Multicast
	netip.MustParsePrefix("240.0.0.0/4"),     // reserviert inkl. Broadcast
	netip.MustParsePrefix("ff00::/8"),        // Multicast
	netip.MustParsePrefix("fec0::/10"),       // Site-Local (veraltet)
	netip.MustParsePrefix("::/96"),           // IPv4-kompatibel (veraltet), z. B. ::127.0.0.1
	netip.MustParsePrefix("::ffff:0:0:0/96"), // SIIT (RFC 7915), z. B. ::ffff:0:127.0.0.1
	netip.MustParsePrefix("64:ff9b::/96"),    // NAT64
	netip.MustParsePrefix("64:ff9b:1::/48"),  // lokales NAT64 (RFC 8215)
	netip.MustParsePrefix("2001::/32"),       // Teredo (RFC 4380)
	netip.MustParsePrefix("2002::/16"),       // 6to4
	netip.MustParsePrefix("2001:db8::/32"),   // Dokumentation
}
