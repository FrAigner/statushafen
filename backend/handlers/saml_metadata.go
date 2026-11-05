package handlers

import (
	"bytes"
	"crypto/sha256"
	"encoding/xml"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/fraigner/statushafen/logging"
	"github.com/fraigner/statushafen/models"

	"github.com/crewjam/saml"
	xrv "github.com/mattermost/xml-roundtrip-validator"
	"golang.org/x/sync/singleflight"
)

// IdP-Metadaten einer Statusseite. Der Seiten-Admin hinterlegt
// entweder die Metadaten-URL des IdP oder fügt das XML ein. Die URL wird wie
// die OIDC-Discovery behandelt: nur https, SSRF-Schutz über safeDialContext
// (auch nach Redirects und DNS-Rebinding), Ergebnis eine Stunde im Cache,
// Fehlschläge eine Minute (ein toter IdP kostet nicht bei jedem Login 10 s).
// Über die URL kommen Schlüsselwechsel beim IdP von selbst an; eingefügtes XML
// muss der Admin nach einem Wechsel neu einfügen.
//
// Bewusst keine Ausnahme für http/localhost wie OIDC_DEV_INSECURE_ISSUERS:
// Für den lokalen Mock-IdP reicht das Einfügen des XML (README, Abschnitt
// "Lokale SSO-Tests mit Mock-SAML"), eine weitere Umgebungsvariable mit
// Zugriff auf interne Adressen wäre nur zusätzliche Angriffsfläche.

// samlMetadataMaxBytes begrenzt die Antwort des Metadaten-URL. Metadaten
// großer Föderationen (EntitiesDescriptor mit tausenden IdPs) sind hier nicht
// gemeint; ein einzelner IdP braucht wenige KB.
const samlMetadataMaxBytes = 1 << 20

var (
	errSAMLMetadataInvalid = &oidcError{http.StatusBadGateway, "metadata_invalid", "IdP-Metadaten ungültig"}
	errSAMLInsecureURL     = errors.New("SAML-Metadaten-URL muss https verwenden")
)

func defaultSAMLHTTPClient() *http.Client {
	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{DialContext: safeDialContext},
	}
}

// samlMetadataTTL bzw. samlMetadataFailureTTL: Cache-Dauer eines Abrufs bzw.
// eines Fehlschlags. Als Funktionen wegen gremlins, siehe oidcStateLifetime.
func samlMetadataTTL() time.Duration        { return time.Hour }
func samlMetadataFailureTTL() time.Duration { return time.Minute }

type samlMetadataEntry struct {
	md      *saml.EntityDescriptor
	err     *oidcError
	expires time.Time
}

var (
	samlHTTPClient = defaultSAMLHTTPClient()

	samlMetadataMu      sync.Mutex
	samlMetadataCache   = map[string]samlMetadataEntry{}
	samlXMLCache        = map[uint]pastedMetadataEntry{}
	samlMetadataFetches singleflight.Group
)

func resetSAMLMetadataCache() {
	samlMetadataMu.Lock()
	defer samlMetadataMu.Unlock()
	samlMetadataCache = map[string]samlMetadataEntry{}
	samlXMLCache = map[uint]pastedMetadataEntry{}
}

func cachedSAMLMetadata(u string) (samlMetadataEntry, bool) {
	samlMetadataMu.Lock()
	defer samlMetadataMu.Unlock()
	e, ok := samlMetadataCache[u]
	return e, ok && oidcNow().Before(e.expires)
}

// parseIdPMetadata liest die Metadaten eines IdP: ein EntityDescriptor oder
// ein EntitiesDescriptor (dann zählt der erste Eintrag mit IdP-Rolle). Gefordert
// sind eine Entity-ID, ein SSO-Endpunkt mit HTTP-Redirect-Binding (nur so
// startet der Login, siehe startSAMLFlow) und mindestens ein Zertifikat zum
// Prüfen der Signatur. Wie crewjam bei Antworten prüft xrv vorher, dass das XML
// beim erneuten Serialisieren gleich bleibt (Schutz vor Parser-Tricks von
// encoding/xml).
func parseIdPMetadata(raw []byte) (*saml.EntityDescriptor, error) {
	if err := xrv.Validate(bytes.NewReader(raw)); err != nil {
		return nil, err
	}
	candidates := []saml.EntityDescriptor{}
	var single saml.EntityDescriptor
	if err := xml.Unmarshal(raw, &single); err == nil {
		candidates = append(candidates, single)
	} else {
		var many saml.EntitiesDescriptor
		if err := xml.Unmarshal(raw, &many); err != nil {
			return nil, err
		}
		candidates = many.EntityDescriptors
	}
	for i := range candidates {
		if len(candidates[i].IDPSSODescriptors) > 0 {
			return &candidates[i], validateIdPMetadata(&candidates[i])
		}
	}
	return nil, errors.New("keine IdP-Rolle (IDPSSODescriptor) in den Metadaten")
}

func validateIdPMetadata(md *saml.EntityDescriptor) error {
	if md.EntityID == "" {
		return errors.New("entityID fehlt")
	}
	if idpRedirectSSOURL(md) == "" {
		return errors.New("kein SingleSignOnService mit HTTP-Redirect-Binding")
	}
	for _, d := range md.IDPSSODescriptors {
		for _, kd := range d.KeyDescriptors {
			if kd.Use != "encryption" && len(kd.KeyInfo.X509Data.X509Certificates) > 0 {
				return nil
			}
		}
	}
	return errors.New("kein Signaturzertifikat in den Metadaten")
}

func idpRedirectSSOURL(md *saml.EntityDescriptor) string {
	for _, d := range md.IDPSSODescriptors {
		for _, s := range d.SingleSignOnServices {
			if s.Binding == saml.HTTPRedirectBinding && s.Location != "" {
				return s.Location
			}
		}
	}
	return ""
}

// loadIdPMetadata ruft den Metadaten-URL ab und legt das Ergebnis (auch einen
// Fehlschlag) im Cache ab. Netzwerk- und HTTP-Fehler zählen als „IdP nicht
// erreichbar“, unbrauchbarer Inhalt als „Metadaten ungültig“.
func loadIdPMetadata(u string) samlMetadataEntry {
	if e, ok := cachedSAMLMetadata(u); ok {
		return e
	}
	e := samlMetadataEntry{expires: oidcNow().Add(samlMetadataFailureTTL())}
	raw, err := fetchSAMLMetadata(u)
	if err != nil {
		slog.Warn("SAML metadata fetch failed", "url", logging.URLForLog(u), "error", logging.SanitizeErr(err))
		e.err = errOIDCProviderUnreachable
	} else if e.md, err = parseIdPMetadata(raw); err != nil {
		slog.Warn("SAML metadata invalid", "url", logging.URLForLog(u), "error", logging.SanitizeErr(err))
		e.err = errSAMLMetadataInvalid
		e.md = nil
	} else {
		e.expires = oidcNow().Add(samlMetadataTTL())
	}
	samlMetadataMu.Lock()
	samlMetadataCache[u] = e
	samlMetadataMu.Unlock()
	return e
}

func fetchSAMLMetadata(u string) ([]byte, error) {
	if !strings.HasPrefix(u, "https://") {
		return nil, errSAMLInsecureURL
	}
	// Eigene Kopie mit Redirect-Regel, damit sie auch für einen in Tests
	// ausgetauschten Client gilt.
	client := *samlHTTPClient
	client.CheckRedirect = samlCheckRedirect
	resp, err := client.Get(u)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("HTTP " + resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, samlMetadataMaxBytes))
}

// samlCheckRedirect (Security-Review): Auch nach einem Redirect
// nur https, und höchstens drei Sprünge. Die Sperre interner Adressen greift
// ohnehin bei jedem Verbindungsaufbau (safeDialContext).
func samlCheckRedirect(req *http.Request, via []*http.Request) error {
	if req.URL.Scheme != "https" {
		return errSAMLInsecureURL
	}
	if len(via) > samlMaxRedirects() {
		return errors.New("zu viele Redirects beim Abruf der SAML-Metadaten")
	}
	return nil
}

// samlMaxRedirects als Funktion wegen gremlins, siehe oidcStateLifetime.
func samlMaxRedirects() int { return 3 }

// pastedIdPMetadata liest eingefügtes XML nur einmal
// je Seite und Inhalt: Der Cache hält je Seite das Ergebnis (auch einen
// Fehler) zum SHA-256 des XML, ein geändertes XML wird neu gelesen. So wächst
// er höchstens mit der Zahl der Seiten, und ein Login kostet kein erneutes
// Parsen. Die gelieferten Metadaten werden nur gelesen, nie verändert.
func pastedIdPMetadata(cfg models.StatusPageSSOConfig) (*saml.EntityDescriptor, *oidcError) {
	sum := sha256.Sum256([]byte(cfg.SAMLIdPMetadataXML))
	samlMetadataMu.Lock()
	e, ok := samlXMLCache[cfg.StatusPageID]
	samlMetadataMu.Unlock()
	if ok && e.sum == sum {
		return e.md, e.err
	}
	e = pastedMetadataEntry{sum: sum}
	md, err := parseIdPMetadata([]byte(cfg.SAMLIdPMetadataXML))
	if err != nil {
		slog.Warn("SAML metadata invalid", "page", cfg.StatusPageID, "error", err)
		e.err = errSAMLMetadataInvalid
	} else {
		e.md = md
	}
	samlMetadataMu.Lock()
	samlXMLCache[cfg.StatusPageID] = e
	samlMetadataMu.Unlock()
	return e.md, e.err
}

type pastedMetadataEntry struct {
	sum [sha256.Size]byte
	md  *saml.EntityDescriptor
	err *oidcError
}

// idpMetadataFor liefert die IdP-Metadaten einer Konfiguration: eingefügtes
// XML (pastedIdPMetadata) oder der gecachte
// Abruf des URL. Gleichzeitige Abrufe desselben URL teilen sich einen.
func idpMetadataFor(cfg models.StatusPageSSOConfig) (*saml.EntityDescriptor, *oidcError) {
	if cfg.SAMLIdPMetadataXML != "" {
		return pastedIdPMetadata(cfg)
	}
	if e, ok := cachedSAMLMetadata(cfg.SAMLIdPMetadataURL); ok {
		return e.md, e.err
	}
	v, _, _ := samlMetadataFetches.Do(cfg.SAMLIdPMetadataURL, func() (interface{}, error) {
		return loadIdPMetadata(cfg.SAMLIdPMetadataURL), nil
	})
	e := v.(samlMetadataEntry)
	return e.md, e.err
}
