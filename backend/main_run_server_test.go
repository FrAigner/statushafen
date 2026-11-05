package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// runServer startet den Server auf PORT, beantwortet Anfragen und kehrt nach
// SIGTERM zurück.
func TestRunServer_ServesAndStopsOnSIGTERM(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	require.NoError(t, ln.Close())
	t.Setenv("PORT", fmt.Sprint(port))

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })

	done := make(chan struct{})
	go func() {
		defer close(done)
		runServer(r)
	}()

	require.Eventually(t, func() bool {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/ping", port)) //nolint:noctx // Test-Polling
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 10*time.Second, 50*time.Millisecond)
	// signal.Notify wird direkt nach dem Start des Listeners registriert.
	time.Sleep(300 * time.Millisecond)
	require.NoError(t, syscall.Kill(os.Getpid(), syscall.SIGTERM))

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("runServer kehrt nach SIGTERM nicht zurück")
	}
}
