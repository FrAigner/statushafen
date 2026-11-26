package externalstatus

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// fakeResolver returns predefined DNS answers. answers[host] is consumed
// in order (the last answer stays), so tests can simulate
// DNS rebinding.
type fakeResolver struct {
	mu      sync.Mutex
	answers map[string][][]string
	err     error
	calls   int
}

func (r *fakeResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	list := r.answers[host]
	if len(list) == 0 {
		return nil, nil
	}
	cur := list[0]
	if len(list) > 1 {
		r.answers[host] = list[1:]
	}
	out := make([]net.IPAddr, 0, len(cur))
	for _, s := range cur {
		out = append(out, net.IPAddr{IP: net.ParseIP(s)})
	}
	return out, nil
}

// testTarget is a local TLS server to which the dialer redirects every already
// checked address. The httptest certificate is valid for example.com,
// so the tests use this host name.
type testTarget struct {
	srv      *httptest.Server
	resolver *fakeResolver
	mu       sync.Mutex
	dialed   []string
	client   *http.Client
}

func newTestTarget(t *testing.T, handler http.Handler) *testTarget {
	t.Helper()
	tt := &testTarget{
		srv:      httptest.NewTLSServer(handler),
		resolver: &fakeResolver{answers: map[string][][]string{"example.com": {{"93.184.216.34"}}}},
	}
	t.Cleanup(tt.srv.Close)
	pool := tt.srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	tt.client = NewSafeClient(ClientConfig{
		Resolver: tt.resolver,
		Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			tt.mu.Lock()
			tt.dialed = append(tt.dialed, addr)
			tt.mu.Unlock()
			var d net.Dialer
			return d.DialContext(ctx, network, tt.srv.Listener.Addr().String())
		},
		TLSConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
	})
	return tt
}

func (tt *testTarget) dialedAddrs() []string {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	return append([]string(nil), tt.dialed...)
}
