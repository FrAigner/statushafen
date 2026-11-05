package handlers

import (
	"net"
	"testing"
	"time"

	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// silentSMTPServer nimmt Verbindungen an und antwortet nie (hängender
// SMTP-Server).
func silentSMTPServer(t *testing.T) (host, port string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var conns []net.Conn
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			conns = append(conns, c)
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
		for _, c := range conns {
			_ = c.Close()
		}
	})
	host, port, err = net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)
	return host, port
}

func TestDeliverMail_HangingSMTPServerTimesOut(t *testing.T) {
	host, port := silentSMTPServer(t)
	oldTotal, oldDial := smtpTotalTimeout, smtpDialTimeout
	smtpTotalTimeout, smtpDialTimeout = 300*time.Millisecond, 200*time.Millisecond
	t.Cleanup(func() { smtpTotalTimeout, smtpDialTimeout = oldTotal, oldDial })

	h := NewHandler(testutils.SetupTestDB())
	cfg := smtpConfig{Host: host, Port: port, From: "alerts@example.com"}

	start := time.Now()
	sent, err := h.deliverMail(cfg, "reader@example.org", []byte("Subject: x\n\nbody"), mailAuditTarget{})
	elapsed := time.Since(start)

	assert.True(t, sent, "der Versand wurde versucht")
	require.Error(t, err)
	assert.Less(t, elapsed, 3*time.Second, "darf nicht dauerhaft blockieren")
}

func TestDeliverMail_UnreachableServerFailsWithoutBlocking(t *testing.T) {
	// Gegenprobe zum Verbindungsfehler: nichts lauscht auf dem Port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	_ = ln.Close()

	h := NewHandler(testutils.SetupTestDB())
	_, err = h.deliverMail(smtpConfig{Host: host, Port: port, From: "alerts@example.com"}, "reader@example.org", []byte("Subject: x\n\nbody"), mailAuditTarget{})
	require.Error(t, err)
}
