package externalstatus

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/fraigner/statushafen/netguard"
)

// SSRF protection: admins enter the base URL of a source, the
// server fetches it. Every check sits as deep as possible:
//
//   - validateTargetURL checks scheme/port/credentials - on input
//     (ValidateBaseURL) and for every redirect target (CheckRedirect).
//   - guardedDial checks the target IP AFTER DNS resolution and connects
//     directly to the checked address. There is no second lookup that could
//     return a different result on DNS rebinding, and every redirect target
//     also goes through the same dialer.
//   - No proxy (otherwise the connection would go to the proxy and the IP check
//     would be pointless), no cookie jar, no credentials.
const (
	// UserAgent identifies our requests to the vendors.
	UserAgent = "Statushafen-StatusImport/1.0 (+https://github.com/fraigner/statushafen)"
	// MaxResponseBytes limits the response read (typical responses are ~50 kB).
	MaxResponseBytes = 2 << 20
	// MaxRedirects: more redirects count as an error.
	MaxRedirects = 3
	// RequestTimeout applies to the whole fetch incl. redirects and body.
	RequestTimeout = 10 * time.Second
)

// ErrBlockedTarget marks a target rejected for security reasons.
var ErrBlockedTarget = errors.New("ziel nicht erlaubt")

// Resolver resolves host names; injectable so tests can supply DNS answers
// (private IPs, rebinding). *net.Resolver satisfies it.
type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// ClientConfig configures NewSafeClient. Empty fields = production values.
type ClientConfig struct {
	Resolver Resolver
	// Dial connects to an already checked "IP:port" address. Tests
	// redirect it to a local httptest server; the check beforehand
	// still runs completely.
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
	// TLSConfig: tests only (RootCAs of the httptest server).
	TLSConfig *tls.Config
}

// NewSafeClient builds the HTTP client for all providers.
func NewSafeClient(cfg ClientConfig) *http.Client {
	resolver := cfg.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	dial := cfg.Dial
	if dial == nil {
		dial = (&net.Dialer{Timeout: 5 * time.Second}).DialContext
	}
	return &http.Client{
		Timeout: RequestTimeout,
		Transport: &http.Transport{
			Proxy:                  nil,
			DialContext:            guardedDial(resolver, dial),
			TLSClientConfig:        cfg.TLSConfig,
			TLSHandshakeTimeout:    5 * time.Second,
			ResponseHeaderTimeout:  RequestTimeout,
			MaxResponseHeaderBytes: 64 << 10,
			DisableKeepAlives:      true,
		},
		CheckRedirect: checkRedirect,
	}
}

// checkRedirect allows at most MaxRedirects redirects, and only to
// https targets that pass validateTargetURL (guardedDial checks the IP afterwards).
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > MaxRedirects {
		return fmt.Errorf("%w: mehr als %d Weiterleitungen", ErrBlockedTarget, MaxRedirects)
	}
	return validateTargetURL(req.URL)
}

// validateTargetURL: https only, port 443, no credentials in the URL.
func validateTargetURL(u *url.URL) error {
	if u.Scheme != "https" {
		return fmt.Errorf("%w: nur https erlaubt", ErrBlockedTarget)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("%w: Host fehlt", ErrBlockedTarget)
	}
	if p := u.Port(); p != "" && p != "443" {
		return fmt.Errorf("%w: nur Port 443 erlaubt", ErrBlockedTarget)
	}
	if u.User != nil {
		return fmt.Errorf("%w: keine Zugangsdaten in der URL", ErrBlockedTarget)
	}
	return nil
}

// guardedDial resolves the host itself, rejects the target as soon as ONE of
// the addresses is blocked (a mixed public/internal answer is
// suspicious and would otherwise depend on the luck of the ordering), and
// connects directly to a checked address.
func guardedDial(resolver Resolver, dial func(ctx context.Context, network, addr string) (net.Conn, error)) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		if port != "443" {
			return nil, fmt.Errorf("%w: nur Port 443 erlaubt", ErrBlockedTarget)
		}
		addrs, err := resolveHost(ctx, resolver, host)
		if err != nil {
			return nil, err
		}
		for _, a := range addrs {
			if netguard.IsBlockedAddr(a) {
				return nil, fmt.Errorf("%w: interne Adresse %s", ErrBlockedTarget, a)
			}
		}
		var lastErr error
		for _, a := range addrs {
			conn, dialErr := dial(ctx, network, net.JoinHostPort(a.String(), port))
			if dialErr == nil {
				return conn, nil
			}
			lastErr = dialErr
		}
		return nil, lastErr
	}
}

// resolveHost returns the addresses for host: IP literals directly, otherwise via DNS.
func resolveHost(ctx context.Context, resolver Resolver, host string) ([]netip.Addr, error) {
	if a, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{a}, nil
	}
	ipAddrs, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(ipAddrs) == 0 {
		return nil, fmt.Errorf("keine Adresse für %s", host)
	}
	addrs := make([]netip.Addr, 0, len(ipAddrs))
	for _, ip := range ipAddrs {
		a, ok := netip.AddrFromSlice(ip.IP)
		if !ok {
			return nil, fmt.Errorf("%w: ungültige Adresse für %s", ErrBlockedTarget, host)
		}
		// net.IP often comes in 16-byte form; unmapped, we choose IPv4 as
		// IPv4 rather than as "[::ffff:…]" (a pure IPv4 host would otherwise fail).
		addrs = append(addrs, a.Unmap())
	}
	return addrs, nil
}

// ValidateBaseURL checks and normalizes the base URL of a source:
// https, port 443, without credentials, query or fragment; result without
// ":443" and without a trailing slash. The IP check follows on the
// fetch (guardedDial), because DNS can change until then.
func ValidateBaseURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("ungültige URL")
	}
	if err := validateTargetURL(u); err != nil {
		return "", err
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%w: keine Query oder Fragment", ErrBlockedTarget)
	}
	return "https://" + strings.ToLower(u.Hostname()) + strings.TrimRight(u.EscapedPath(), "/"), nil
}

// fetchResult is the raw response of getJSON.
type fetchResult struct {
	NotModified  bool
	Body         []byte
	ETag         string
	LastModified string
}

// getJSON fetches target with conditional-request headers and reads at most
// MaxResponseBytes. Common foundation for all providers (part B uses it to
// fetch incidents.json as well).
func getJSON(ctx context.Context, client *http.Client, target string, req FetchRequest) (*fetchResult, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("User-Agent", UserAgent)
	httpReq.Header.Set("Accept", "application/json")
	if req.ETag != "" {
		httpReq.Header.Set("If-None-Match", req.ETag)
	}
	if req.LastModified != "" {
		httpReq.Header.Set("If-Modified-Since", req.LastModified)
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotModified {
		return &fetchResult{NotModified: true, ETag: req.ETag, LastModified: req.LastModified}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d von %s", resp.StatusCode, target)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > MaxResponseBytes {
		return nil, fmt.Errorf("antwort größer als %d Bytes", MaxResponseBytes)
	}
	return &fetchResult{
		Body:         body,
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
	}, nil
}
