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

// SSRF-Schutz: Admins geben die Basis-URL einer Quelle ein, der
// Server ruft sie ab. Jede Prüfung sitzt so tief wie möglich:
//
//   - Schema/Port/Credentials prüft validateTargetURL - bei der Eingabe
//     (ValidateBaseURL) und für jedes Redirect-Ziel (CheckRedirect).
//   - Die Ziel-IP prüft guardedDial NACH der DNS-Auflösung und verbindet sich
//     direkt mit der geprüften Adresse. Es gibt keinen zweiten Lookup, der bei
//     DNS-Rebinding ein anderes Ergebnis liefern könnte, und auch jedes
//     Redirect-Ziel läuft durch denselben Dialer.
//   - Kein Proxy (sonst ginge die Verbindung an den Proxy und die IP-Prüfung
//     liefe ins Leere), kein Cookie-Jar, keine Credentials.
const (
	// UserAgent kennzeichnet unsere Abrufe gegenüber den Anbietern.
	UserAgent = "Statushafen-StatusImport/1.0 (+https://github.com/fraigner/statushafen)"
	// MaxResponseBytes begrenzt die gelesene Antwort (typische Antworten sind ~50 kB).
	MaxResponseBytes = 2 << 20
	// MaxRedirects: mehr Weiterleitungen gelten als Fehler.
	MaxRedirects = 3
	// RequestTimeout gilt für den gesamten Abruf inkl. Redirects und Body.
	RequestTimeout = 10 * time.Second
)

// ErrBlockedTarget kennzeichnet ein aus Sicherheitsgründen abgelehntes Ziel.
var ErrBlockedTarget = errors.New("ziel nicht erlaubt")

// Resolver löst Hostnamen auf; injizierbar, damit Tests DNS-Antworten
// (private IPs, Rebinding) vorgeben können. *net.Resolver erfüllt es.
type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// ClientConfig konfiguriert NewSafeClient. Leere Felder = Produktionswerte.
type ClientConfig struct {
	Resolver Resolver
	// Dial verbindet mit einer bereits geprüften "IP:Port"-Adresse. Tests
	// leiten sie auf einen lokalen httptest-Server um; die Prüfung davor
	// läuft trotzdem vollständig.
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
	// TLSConfig: nur für Tests (RootCAs des httptest-Servers).
	TLSConfig *tls.Config
}

// NewSafeClient baut den HTTP-Client für alle Provider.
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

// checkRedirect erlaubt höchstens MaxRedirects Weiterleitungen, und nur auf
// https-Ziele, die validateTargetURL bestehen (die IP prüft danach guardedDial).
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > MaxRedirects {
		return fmt.Errorf("%w: mehr als %d Weiterleitungen", ErrBlockedTarget, MaxRedirects)
	}
	return validateTargetURL(req.URL)
}

// validateTargetURL: nur https, Port 443, keine Zugangsdaten in der URL.
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

// guardedDial löst den Host selbst auf, verwirft das Ziel, sobald EINE der
// Adressen gesperrt ist (eine gemischte Antwort öffentlich/intern ist
// verdächtig und würde sonst vom Zufall der Reihenfolge abhängen), und
// verbindet sich direkt mit einer geprüften Adresse.
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

// resolveHost liefert die Adressen zu host: IP-Literale direkt, sonst per DNS.
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
		// net.IP kommt oft in 16-Byte-Form; ungemappt wählen wir IPv4 als
		// IPv4 statt als "[::ffff:…]" (ein reiner IPv4-Host scheitert sonst).
		addrs = append(addrs, a.Unmap())
	}
	return addrs, nil
}

// ValidateBaseURL prüft und normalisiert die Basis-URL einer Quelle:
// https, Port 443, ohne Zugangsdaten, Query oder Fragment; Ergebnis ohne
// ":443" und ohne abschließenden Schrägstrich. Die IP-Prüfung folgt beim
// Abruf (guardedDial), weil sich DNS bis dahin ändern kann.
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

// fetchResult ist die Rohantwort von getJSON.
type fetchResult struct {
	NotModified  bool
	Body         []byte
	ETag         string
	LastModified string
}

// getJSON ruft target mit Conditional-Request-Headern ab und liest höchstens
// MaxResponseBytes. Gemeinsamer Unterbau für alle Provider (Teil B ruft damit
// auch incidents.json ab).
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
