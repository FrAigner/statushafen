package netguard

import (
	"net"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// jeder Sperrfall für IPv4 und IPv6, dazu Gegenproben mit
// öffentlichen Adressen.
func TestIsBlockedIP(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "127.8.9.10", "::1", // Loopback
		"10.1.2.3", "172.16.0.1", "192.168.1.1", // privat
		"fc00::1", "fd12:3456::1", // Unique-Local
		"169.254.169.254", "fe80::1", // Link-Local (inkl. Cloud-Metadaten)
		"224.0.0.1", "239.255.255.250", "ff02::1", // Multicast
		"100.64.0.1", "100.127.255.254", // CGNAT
		"0.0.0.0", "0.1.2.3", "::", // unspezifiziert / dieses Netz
		"::ffff:10.0.0.1", "::ffff:127.0.0.1", // IPv4-mapped
		"192.0.2.1", "198.18.0.1", "198.51.100.1", "203.0.113.1", "240.0.0.1", "255.255.255.255",
		"fec0::1", "64:ff9b::a00:1", "2002:a00:1::1", "2001:db8::1", "192.0.0.8",
	}
	for _, s := range blocked {
		assert.True(t, IsBlockedIP(net.ParseIP(s)), s)
	}
	allowed := []string{"1.1.1.1", "8.8.8.8", "93.184.216.34", "100.128.0.1", "2a00:1450:4001:80b::200e", "2606:4700::1111"}
	for _, s := range allowed {
		assert.False(t, IsBlockedIP(net.ParseIP(s)), s)
	}
	assert.True(t, IsBlockedIP(net.IP{1, 2, 3}), "ungültige Länge gilt als gesperrt")
}

// IPv6-Übergangsformate tragen eine IPv4-Adresse im Inneren. Ein Ziel wie
// ::127.0.0.1 darf nicht an der IPv4-Sperrliste vorbeikommen, auch wenn ein
// Resolver oder Netzstack es auf IPv4 abbildet.
func TestIsBlockedIP_EingebetteteIPv4(t *testing.T) {
	blocked := []string{
		// IPv4-kompatibel ::/96 (veraltet)
		"::127.0.0.1", "::a00:1", "::808:808",
		// SIIT ::ffff:0:0:0/96
		"::ffff:0:127.0.0.1", "::ffff:0:10.0.0.1", "::ffff:0:8.8.8.8",
		// lokales NAT64 64:ff9b:1::/48
		"64:ff9b:1::7f00:1", "64:ff9b:1:ffff::a00:1",
		// Teredo 2001::/32
		"2001::7f00:1", "2001:0:4136:e378:8000:63bf:3fff:fdd2",
		// 6to4 2002::/16 mit Loopback und mit öffentlicher IPv4
		"2002:7f00:1::", "2002:808:808::1",
		// NAT64 64:ff9b::/96
		"64:ff9b::7f00:1", "64:ff9b::808:808",
	}
	for _, s := range blocked {
		assert.True(t, IsBlockedIP(net.ParseIP(s)), s)
	}
	// Gegenprobe: öffentliche Adressen bleiben erreichbar, auch solche, deren
	// Präfix den gesperrten nur ähnelt.
	allowed := []string{
		"93.184.216.34", "::ffff:8.8.8.8", "2606:4700::1111",
		"2001:4860:4860::8888", "2001:1::1", "2a00:1450:4001:80b::200e",
		"64:ff9a::1", "1::1",
	}
	for _, s := range allowed {
		assert.False(t, IsBlockedIP(net.ParseIP(s)), s)
	}
}

// Eine IPv6-Zone (%eth0) lässt netip.Prefix.Contains immer false liefern.
// Ohne Gegenmaßnahme käme so z. B. eine 6to4- oder NAT64-Adresse mit Zone an
// der Sperrliste vorbei. Adressen mit Zone gelten daher als gesperrt, egal
// welcher Bereich dahinter steht.
func TestIsBlockedAddr_ZoneUmgehtSperrlisteNicht(t *testing.T) {
	cases := []string{
		"::%lo", "fec0::1%eth0", "64:ff9b::a9fe:a9fe%1", "64:ff9b::a00:1%eth0",
		"2002:c0a8:1::1%eth0", "2002:7f00:1::%lo", "ff02::1%eth0", "2001::7f00:1%eth0",
		"64:ff9b:1::7f00:1%eth0", "::ffff:0:127.0.0.1%eth0",
		"fe80::1%eth0", "::1%lo", "fc00::1%eth0", "2606:4700::1111%eth0",
	}
	for _, s := range cases {
		addr, err := netip.ParseAddr(s)
		require.NoError(t, err, s)
		require.NotEmpty(t, addr.Zone(), s)
		assert.True(t, IsBlockedAddr(addr), s)
	}
	// Gegenprobe: dieselben öffentlichen Adressen ohne Zone bleiben erlaubt.
	for _, s := range []string{"2606:4700::1111", "2a00:1450:4001:80b::200e"} {
		assert.False(t, IsBlockedAddr(netip.MustParseAddr(s)), s)
	}
}

func FuzzIsBlockedAddr_Zone(f *testing.F) {
	for _, s := range []string{"::", "fec0::1", "64:ff9b::a9fe:a9fe", "2002:c0a8:1::1", "ff02::1"} {
		f.Add(s, "eth0")
	}
	f.Fuzz(func(t *testing.T, ip, zone string) {
		base, err := netip.ParseAddr(ip)
		if err != nil || !base.Is6() || base.Zone() != "" || zone == "" {
			t.Skip()
		}
		// Die Zone darf das Ergebnis nie von gesperrt auf erlaubt kippen.
		if IsBlockedAddr(base) {
			assert.True(t, IsBlockedAddr(base.WithZone(zone)))
		}
	})
}
