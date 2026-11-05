package middleware

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLimiterKey(t *testing.T) {
	cases := map[string]string{
		"192.0.2.1":                   "192.0.2.1",
		"::ffff:192.0.2.1":            "192.0.2.1",
		"2001:db8:1234:5678::1":       "2001:db8:1234:5678::/64",
		"2001:db8:1234:5678:aa:bb::9": "2001:db8:1234:5678::/64",
		"fe80::1%eth0":                "fe80::/64",
		"":                            "", // unlesbare Adresse bleibt, wie sie ist
		"kein-ip":                     "kein-ip",
	}
	for in, want := range cases {
		assert.Equal(t, want, limiterKey(in), in)
	}
}
