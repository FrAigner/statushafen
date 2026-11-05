package main

import (
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Der Server hat Grenzen für Header, Body, Antwort und Leerlauf. Ohne sie hält
// jede Verbindung, die nie fertig sendet, einen Goroutine- und Dateideskriptor-
// Platz belegt (Slowloris).
func TestNewHTTPServer_SetsAllTimeouts(t *testing.T) {
	srv := newHTTPServer(":0", gin.New(), defaultServerTimeouts)
	assert.Equal(t, defaultServerTimeouts.ReadHeader, srv.ReadHeaderTimeout)
	assert.Equal(t, defaultServerTimeouts.Read, srv.ReadTimeout)
	assert.Equal(t, defaultServerTimeouts.Write, srv.WriteTimeout)
	assert.Equal(t, defaultServerTimeouts.Idle, srv.IdleTimeout)

	assert.Positive(t, srv.ReadHeaderTimeout)
	assert.LessOrEqual(t, srv.ReadHeaderTimeout, 15*time.Second, "Header kommen in Sekunden, nicht Minuten")
	assert.GreaterOrEqual(t, srv.ReadTimeout, srv.ReadHeaderTimeout)
	// Handler rufen externe Dienste auf (Übersetzung 15 s, Webhook-Test), die Antwort darf nicht davor abreißen.
	assert.GreaterOrEqual(t, srv.WriteTimeout, 45*time.Second)
	assert.Positive(t, srv.IdleTimeout)
}

func TestNewHTTPServer_ClosesStalledHeaderConnection(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	short := serverTimeouts{ReadHeader: 200 * time.Millisecond, Read: time.Second, Write: time.Second, Idle: time.Second}
	srv := newHTTPServer("", gin.New(), short)
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	conn, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	_, err = conn.Write([]byte("GET /healthz HTTP/1.1\r\nHost: a\r\nX-Slow: "))
	require.NoError(t, err)
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(3*time.Second)))
	buf := make([]byte, 512)
	_, err = conn.Read(buf)
	var ne net.Error
	timedOut := errors.As(err, &ne) && ne.Timeout()
	assert.False(t, timedOut, "Server muss die unvollständige Anfrage selbst beenden, Fehler: %v", err)

	// Gegenprobe: eine vollständige Anfrage wird normal beantwortet.
	resp, err := http.Get("http://" + ln.Addr().String() + "/nichts")
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}
