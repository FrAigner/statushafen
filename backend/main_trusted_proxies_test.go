package main

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/logging/loggingtest"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Wer 0.0.0.0/0 (oder ein ähnlich weites Netz) als vertrauenswürdigen Proxy
// einträgt, lässt jeden Client X-Forwarded-For frei wählen: IP-Limits und
// Audit-Adressen sind dann wirkungslos.
func TestOverlyWideTrustedProxies(t *testing.T) {
	cases := []struct {
		name    string
		entries []string
		wantN   int
	}{
		{"leer", nil, 0},
		{"einzelne Adresse", []string{"192.0.2.10"}, 0},
		{"übliche private Netze", []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}, 0},
		{"IPv6 /32 und Einzeladresse", []string{"2001:db8::/32", "2001:db8::1"}, 0},
		{"IPv6 genau /16", []string{"2001::/16"}, 0},
		{"alles IPv4", []string{"0.0.0.0/0"}, 1},
		{"alles IPv6", []string{"::/0"}, 1},
		{"IPv4 /7", []string{"10.0.0.0/7"}, 1},
		{"IPv4 /8 ist erlaubt", []string{"10.0.0.0/8"}, 0},
		{"IPv6 /15", []string{"2000::/15"}, 1},
		{"IPv4 in IPv6 als alles", []string{"::ffff:0.0.0.0/96"}, 1},
		{"IPv4 in IPv6 entspricht /8 und ist erlaubt", []string{"::ffff:10.0.0.0/104"}, 0},
		{"IPv4 in IPv6 entspricht /7 und ist zu weit", []string{"::ffff:10.0.0.0/103"}, 1},
		{"mehrere, zwei zu weit", []string{"10.0.0.1", "0.0.0.0/0", "::/0"}, 2},
		{"Leerraum um Eintrag", []string{" 0.0.0.0/0 "}, 1},
		{"ungültig wird hier nicht gemeldet", []string{"kein-cidr"}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Len(t, overlyWideTrustedProxies(tc.entries), tc.wantN)
		})
	}
}

func TestNewRouter_TooWideTrustedProxiesAreFatalOutsideDevelopment(t *testing.T) {
	for _, appEnv := range []string{"production", "", "staging"} {
		t.Run("APP_ENV="+appEnv, func(t *testing.T) {
			t.Setenv("APP_ENV", appEnv)
			t.Setenv("TRUSTED_PROXIES", "10.0.0.0/8, 0.0.0.0/0")
			h := handlers.NewHandler(testutils.SetupTestDB())
			got := loggingtest.ExpectFatal(t, func() { newRouter(h) })
			assert.Equal(t, "TRUSTED_PROXIES vertraut zu weiten Netzen", got.Message)
			assert.Contains(t, got.Attrs["entries"], "0.0.0.0/0")
		})
	}
}

func TestNewRouter_TooWideTrustedProxiesOnlyWarnInDevelopment(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	t.Setenv("APP_ENV", "development")
	t.Setenv("TRUSTED_PROXIES", "0.0.0.0/0")

	require.NotPanics(t, func() { newRouter(handlers.NewHandler(testutils.SetupTestDB())) })
	assert.Contains(t, buf.String(), "TRUSTED_PROXIES vertraut zu weiten Netzen")
	assert.Contains(t, buf.String(), "level=WARN")
}

func TestNewRouter_NarrowTrustedProxiesPassInProduction(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("TRUSTED_PROXIES", "10.0.0.0/8,192.0.2.7")
	require.NotPanics(t, func() { newRouter(handlers.NewHandler(testutils.SetupTestDB())) })
}
