package handlers

import (
	"crypto/rsa"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/fraigner/statushafen/models"

	"github.com/beevik/etree"
	"github.com/crewjam/saml"
	"github.com/gin-gonic/gin"
	dsig "github.com/russellhaering/goxmldsig"
	"gorm.io/gorm/clause"
)

// SAML-Anmeldung je Statusseite (Folge, ersetzt das
// frühere Gerüst saml_handler.go). Gleiche Regeln wie beim OIDC-Login
// (oidc_login.go): eigene Konfiguration je Seite, keine Neuanlage von
// Konten, Zuordnung nur über (Seite, Protokoll, Issuer, Subject) und nie über
// die E-Mail, Verknüpfung durch den angemeldeten Nutzer selbst, keine Global
// Admins und keine Konten mit Rolle auf einer anderen Seite, kein Rollen-Mapping
// aus IdP-Attributen, Fehler als Redirect mit festem Code. Kein Single Logout.
//
// Bibliothek: github.com/crewjam/saml (reines Go, XML-Signaturen über
// russellhaering/goxmldsig). xmlsec1 hätte ein C-Programm ins Image gebracht
// (Rückfrage). Genutzt wird nur saml.ServiceProvider, nicht samlsp:
// dessen Sitzung (eigenes Cookie-JWT) passt nicht zur Sitzung der Anwendung.
//
// Ablauf:
//
//  1. GET /api/auth/saml/login?page=N: AuthnRequest (HTTP-Redirect-Binding,
//     mit dem SP-Schlüssel der Seite signiert) erzeugen, Request-ID mit einem
//     zufälligen State in SAMLLoginState ablegen, State als RelayState und als
//     Cookie mitgeben, zum IdP umleiten.
//  2. POST /api/auth/saml/N/acs (HTTP-POST-Binding): State gegen Cookie prüfen
//     und genau einmal einlösen, dann prüft crewjam die Antwort: Signatur von
//     Response oder Assertion (eins von beiden ist Pflicht) mit den
//     Zertifikaten aus den IdP-Metadaten, Issuer, Destination, InResponseTo
//     (nur die Request-ID dieses States; IdP-initiierte Logins sind aus),
//     Recipient, NotBefore/NotOnOrAfter und Ausstellungszeit. Dazu kommen hier
//     eine strenge Audience-Prüfung, eine SubjectConfirmation als Pflicht und
//     die einmalige Assertion-ID (SAMLAssertionUse).
//  3. Konto über UserSSOIdentity finden wie bei OIDC (resolveSSOUser),
//     Sitzung über den Einmal-Code und /api/auth/sso-exchange. Alle Redirects
//     des ACS mit 303 (ssoRedirect), damit der Browser per GET weitergeht.
//
// Die Fehler-Redirects nutzen dieselben Parameter (oidc_error, oidc_test) und
// Codes wie OIDC, damit Login-Seite, Profil und SSO-Seite sie ohne eigene
// Behandlung anzeigen; die Namen stammen aus der Zeit, als es nur OIDC gab.
// Aus demselben Grund teilt SAML den Fehlertyp oidcError.

const (
	samlStateCookieName = "saml_state"
	samlStateCookiePath = "/api/auth/saml"
	// samlMaxResponseBytes begrenzt den POST des IdP. Signierte und
	// verschlüsselte Antworten mit vielen Attributen liegen bei wenigen KB.
	samlMaxResponseBytes = 1 << 20
)

var (
	errSAMLResponseInvalid = &oidcError{http.StatusForbidden, "saml_response_invalid", "SAML-Antwort ungültig"}
	errSAMLReplay          = &oidcError{http.StatusForbidden, "saml_replay", "SAML-Assertion wurde bereits verwendet"}
	errSAMLNameIDFormat    = &oidcError{http.StatusForbidden, "saml_nameid_format", "Der IdP liefert keine persistente NameID"}
	errSAMLTestIncomplete  = &oidcError{http.StatusBadRequest, "saml_test_incomplete", "Für die Testanmeldung fehlen die IdP-Metadaten"}
)

// IsSAMLACSPath erkennt den ACS-Pfad /api/auth/saml/<Seite>/acs (für
// die CORS-Ausnahme in main.go).
func IsSAMLACSPath(path string) bool {
	rest, ok := strings.CutPrefix(path, "/api/auth/saml/")
	if !ok {
		return false
	}
	page, ok := strings.CutSuffix(rest, "/acs")
	return ok && page != "" && !strings.Contains(page, "/")
}

func samlPageURL(pageID uint) string {
	return getBaseURL() + "/api/auth/saml/" + strconv.FormatUint(uint64(pageID), 10)
}

// samlEntityID: Die Entity-ID des SP ist (wie bei crewjam üblich) der URL
// seiner Metadaten, damit ein IdP sie direkt von dort laden kann.
func samlEntityID(pageID uint) string { return samlPageURL(pageID) + "/metadata" }
func samlACSURL(pageID uint) string   { return samlPageURL(pageID) + "/acs" }

// newSAMLServiceProvider baut den SP einer Seite. key und idp dürfen nil sein,
// wenn nur die Metadaten gebraucht werden. BASE_URL setzt der Betreiber, ein
// Parse-Fehler der daraus gebauten URLs würde schon jeden anderen Link brechen.
//
// Verlangt wird eine persistente NameID; nur mit
// allowUnspecified (saml_allow_unspecified_nameid) bleibt das Format dem IdP
// überlassen, damit auch IdPs ohne persistente Kennung antworten.
func newSAMLServiceProvider(pageID uint, key *rsa.PrivateKey, cert *x509.Certificate, idp *saml.EntityDescriptor, allowUnspecified bool) *saml.ServiceProvider {
	metadataURL, _ := url.Parse(samlEntityID(pageID))
	acsURL, _ := url.Parse(samlACSURL(pageID))
	sp := &saml.ServiceProvider{
		EntityID:          metadataURL.String(),
		Key:               key,
		Certificate:       cert,
		MetadataURL:       *metadataURL,
		AcsURL:            *acsURL,
		IDPMetadata:       idp,
		SignatureMethod:   dsig.RSASHA256SignatureMethod,
		AuthnNameIDFormat: saml.PersistentNameIDFormat,
		AllowIDPInitiated: false,
	}
	if allowUnspecified {
		sp.AuthnNameIDFormat = saml.UnspecifiedNameIDFormat
	}
	sp.ValidateAudienceRestriction = func(a *saml.Assertion) error {
		return requireSAMLAudience(a, sp.EntityID)
	}
	return sp
}

// requireSAMLAudience: crewjam akzeptiert eine Assertion ohne
// AudienceRestriction. Hier ist sie Pflicht, sonst wäre eine Assertion, die
// der IdP für einen anderen SP ausgestellt hat, auch hier gültig.
func requireSAMLAudience(a *saml.Assertion, entityID string) error {
	for _, ar := range a.Conditions.AudienceRestrictions {
		if ar.Audience.Value == entityID {
			return nil
		}
	}
	return errors.New("AudienceRestriction mit der Entity-ID des SP fehlt")
}

// samlSPMetadata sind die Metadaten, die der Seiten-Admin beim IdP hinterlegt.
// crewjam bietet zusätzlich einen ACS mit Artifact-Binding an; der fällt weg,
// weil die Artifact-Auflösung eine Anfrage an eine vom Seiten-Admin gepflegte
// Adresse wäre (SSRF) und der ACS sie ohnehin nicht annimmt.
func samlSPMetadata(pageID uint, cert *x509.Certificate, allowUnspecified bool) *saml.EntityDescriptor {
	md := newSAMLServiceProvider(pageID, nil, cert, nil, allowUnspecified).Metadata()
	desc := &md.SPSSODescriptors[0]
	desc.AssertionConsumerServices = desc.AssertionConsumerServices[:1]
	// Security-Review: Keine Verschlüsselung anbieten. crewjam
	// listet das Zertifikat zusätzlich mit use="encryption"; ein IdP würde
	// dann verschlüsseln, der ACS lehnt EncryptedAssertion aber ab
	// (checkSAMLAssertionCount). Es bleibt nur der Signaturschlüssel.
	signing := desc.KeyDescriptors[:0]
	for _, kd := range desc.KeyDescriptors {
		if kd.Use == "signing" {
			signing = append(signing, kd)
		}
	}
	desc.KeyDescriptors = signing
	return md
}

// SAMLMetadataHandler — GET /api/auth/saml/:page_id/metadata (öffentlich).
// Liefert die SP-Metadaten, sobald die Seite eine SSO-Konfiguration mit
// SP-Zertifikat hat, auch vor dem Einschalten von SAML: Der Admin muss den SP
// beim IdP registrieren, bevor er die Anmeldung freigibt. Der Inhalt
// (Entity-ID, ACS-URL, Zertifikat) ist öffentlich.
//
// Gelöschte Seiten liefern 404. Private Seiten
// bewusst nicht: Gerade sie brauchen das SSO, und viele IdPs (Keycloak,
// Authentik) laden die SP-Metadaten per URL. Dass die Seite existiert,
// verrät ohnehin GET /api/public/pages/:slug/sso.
func (h *Handler) SAMLMetadataHandler(c *gin.Context) {
	pageID, err := strconv.ParseUint(c.Param("page_id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ungültige Seite", "code": "invalid_id"})
		return
	}
	var cfg models.StatusPageSSOConfig
	// nicht für gelöschte Seiten (private bleiben,
	// siehe Doku am Handler).
	if err := h.DB.Where("status_page_id = ? AND saml_sp_certificate <> ''", pageID).
		Where("status_page_id IN (?)", h.DB.Model(&models.StatusPage{}).Select("id")).
		First(&cfg).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "SAML für diese Seite nicht eingerichtet", "code": "saml_not_configured"})
		return
	}
	cert, err := parseSAMLCert(cfg.SAMLSPCertificate)
	if err != nil {
		slog.Error("SAML SP certificate invalid", "page", pageID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "SP-Zertifikat ungültig", "code": "server_error"})
		return
	}
	// Das Marshalling fester crewjam-Strukturen kann nicht fehlschlagen.
	buf, _ := xml.MarshalIndent(samlSPMetadata(uint(pageID), cert, cfg.SAMLAllowUnspecifiedNameID), "", "  ")
	c.Data(http.StatusOK, "application/samlmetadata+xml", buf)
}

func (h *Handler) activeSAMLConfig(pageID uint64) (models.StatusPageSSOConfig, bool) {
	var cfg models.StatusPageSSOConfig
	err := h.DB.Where("status_page_id = ? AND saml_enabled = ?", pageID, true).First(&cfg).Error
	return cfg, err == nil
}

// samlServiceProviderFor baut den vollständigen SP einer Konfiguration samt
// IdP-Metadaten.
func samlServiceProviderFor(cfg models.StatusPageSSOConfig) (*saml.ServiceProvider, *oidcError) {
	key, cert, err := parseSAMLKeyPair(cfg.SAMLSPKey, cfg.SAMLSPCertificate)
	if err != nil {
		slog.Error("SAML SP key invalid", "page", cfg.StatusPageID, "error", err)
		return nil, errOIDCStartFailed
	}
	md, e := idpMetadataFor(cfg)
	if e != nil {
		return nil, e
	}
	return newSAMLServiceProvider(cfg.StatusPageID, key, cert, md, cfg.SAMLAllowUnspecifiedNameID), nil
}

// beginSAML startet Login oder Verknüpfung (linkUserID != 0).
func (h *Handler) beginSAML(c *gin.Context, linkUserID uint) (string, *oidcError) {
	pageID, err := strconv.ParseUint(c.Query("page"), 10, 32)
	if err != nil {
		return "", errOIDCBadPage
	}
	cfg, ok := h.activeSAMLConfig(pageID)
	if !ok {
		return "", errOIDCNotConfigured
	}
	return h.startSAMLFlow(c, cfg, models.SAMLLoginState{LinkUserID: linkUserID})
}

// startSAMLFlow erzeugt den signierten AuthnRequest, speichert den State und
// setzt das State-Cookie. Gemeinsam für Login, Verknüpfung und Testanmeldung.
// Nur HTTP-Redirect-Binding: Das bieten alle verbreiteten IdPs an (Entra ID,
// Keycloak, Authentik, ADFS, Okta, SimpleSAMLphp), und Verknüpfung wie
// Testanmeldung brauchen eine URL, die das Frontend öffnen kann.
func (h *Handler) startSAMLFlow(c *gin.Context, cfg models.StatusPageSSOConfig, st models.SAMLLoginState) (string, *oidcError) {
	sp, e := samlServiceProviderFor(cfg)
	if e != nil {
		return "", e
	}
	// Abgelaufene States und Assertion-IDs gleich mit wegräumen (wie OIDC).
	now := time.Now()
	h.DB.Where("expires_at < ?", now).Delete(&models.SAMLLoginState{})
	h.DB.Where("expires_at < ?", now).Delete(&models.SAMLAssertionUse{})

	// Beide Aufrufe scheitern nur an ungültigen Eingaben, die hier nicht
	// vorkommen: Der SSO-URL ist durch parseIdPMetadata gesichert, Schlüssel
	// und Signaturverfahren passen zusammen (RSA, RSA-SHA256).
	req, _ := sp.MakeAuthenticationRequest(idpRedirectSSOURL(sp.IDPMetadata), saml.HTTPRedirectBinding, saml.HTTPPostBinding)
	st.State = randomToken(32)
	st.RequestID = req.ID
	st.StatusPageID = cfg.StatusPageID
	st.ExpiresAt = now.Add(oidcStateLifetime())
	target, _ := req.Redirect(st.State, sp)
	if err := h.DB.Create(&st).Error; err != nil {
		return "", errOIDCStartFailed
	}
	// SameSite=None: Der IdP schickt die Antwort per Cross-Site-POST, ein
	// Lax-Cookie (Browser-Standard) käme dabei nicht mit. Gegen Login-CSRF
	// schützt der Vergleich mit dem RelayState.
	c.SetSameSite(http.SameSiteNoneMode)
	c.SetCookie(samlStateCookieName, st.State, int(oidcStateLifetime().Seconds()), samlStateCookiePath, "", true, true)
	return target.String(), nil
}

// SAMLLoginHandler — GET /api/auth/saml/login?page=N (öffentlich,
// rate-limitiert). Browser-Navigation wie OIDCLoginHandler, Fehler als Code
// auf der Login-Seite.
func (h *Handler) SAMLLoginHandler(c *gin.Context) {
	target, e := h.beginSAML(c, 0)
	if e != nil {
		target = ssoErrorURL(false, 0, e)
	}
	c.Redirect(http.StatusTemporaryRedirect, target)
}

// SAMLLinkHandler — POST /api/auth/saml/link?page=N (angemeldet). Regeln und
// Antwort wie OIDCLinkHandler.
func (h *Handler) SAMLLinkHandler(c *gin.Context) {
	user, e := h.ssoLinkUser(c)
	if e != nil {
		writeOIDCError(c, e)
		return
	}
	target, e := h.beginSAML(c, user.ID)
	if e != nil {
		writeOIDCError(c, e)
		return
	}
	c.JSON(http.StatusOK, gin.H{"url": target})
}

// samlConfigComplete: Ohne IdP-Metadaten kann kein Ablauf starten. Schlüssel
// und Zertifikat legt das Speichern der Konfiguration immer an.
func samlConfigComplete(cfg models.StatusPageSSOConfig) bool {
	return cfg.SAMLIdPMetadataURL != "" || cfg.SAMLIdPMetadataXML != ""
}

// testSAMLConfig wie testOIDCConfig: kein saml_enabled nötig, dafür
// test_login_enabled.
func (h *Handler) testSAMLConfig(pageID uint64) (models.StatusPageSSOConfig, *oidcError) {
	var cfg models.StatusPageSSOConfig
	if err := h.DB.Where("status_page_id = ?", pageID).First(&cfg).Error; err != nil {
		return cfg, errOIDCNotConfigured
	}
	if !cfg.TestLoginEnabled {
		return cfg, errOIDCTestDisabled
	}
	if !samlConfigComplete(cfg) {
		return cfg, errSAMLTestIncomplete
	}
	return cfg, nil
}

// StartSAMLTestHandler — POST /api/admin/pages/:page_id/sso/saml-test
// (pages:write auf der Seite). Wie StartSSOTestHandler: prüft den Ablauf beim
// IdP samt allen Prüfungen des ACS, ohne Sitzung und ohne Verknüpfung.
func (h *Handler) StartSAMLTestHandler(c *gin.Context) {
	pageID, err := strconv.ParseUint(c.Param("page_id"), 10, 32)
	if err != nil {
		writeOIDCError(c, errOIDCBadPage)
		return
	}
	cfg, e := h.testSAMLConfig(pageID)
	if e != nil {
		writeOIDCError(c, e)
		return
	}
	target, e := h.startSAMLFlow(c, cfg, models.SAMLLoginState{TestLogin: true})
	if e != nil {
		writeOIDCError(c, e)
		return
	}
	c.JSON(http.StatusOK, gin.H{"url": target})
}

// consumeSAMLState prüft RelayState gegen das Cookie und löst den State atomar
// ein (DELETE … RETURNING), wie consumeOIDCState. Der State muss zur Seite im
// ACS-Pfad gehören.
func (h *Handler) consumeSAMLState(c *gin.Context) (models.SAMLLoginState, *oidcError) {
	var st models.SAMLLoginState
	state := c.PostForm("RelayState")
	cookie, _ := c.Cookie(samlStateCookieName)
	c.SetSameSite(http.SameSiteNoneMode)
	c.SetCookie(samlStateCookieName, "", -1, samlStateCookiePath, "", true, true)
	if state == "" || subtle.ConstantTimeCompare([]byte(cookie), []byte(state)) != 1 {
		return st, errOIDCStateInvalid
	}
	res := h.DB.Clauses(clause.Returning{}).Where("state = ?", state).Delete(&st)
	if res.Error != nil || res.RowsAffected == 0 || time.Now().After(st.ExpiresAt) ||
		strconv.FormatUint(uint64(st.StatusPageID), 10) != c.Param("page_id") {
		return st, errOIDCStateInvalid
	}
	return st, nil
}

// samlConfigForState wie oidcConfigForState.
func (h *Handler) samlConfigForState(st models.SAMLLoginState) (models.StatusPageSSOConfig, *oidcError) {
	if st.TestLogin {
		return h.testSAMLConfig(uint64(st.StatusPageID))
	}
	cfg, ok := h.activeSAMLConfig(uint64(st.StatusPageID))
	if !ok {
		return cfg, errOIDCNotConfigured
	}
	return cfg, nil
}

// SAMLACSHandler — POST /api/auth/saml/:page_id/acs (öffentlich,
// rate-limitiert). Jeder Fehler endet als Redirect mit festem Code.
func (h *Handler) SAMLACSHandler(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, samlMaxResponseBytes)
	st, e := h.consumeSAMLState(c)
	if e == nil {
		e = h.completeSAML(c, st)
	}
	if e != nil {
		ssoRedirect(c, ssoErrorURL(st.TestLogin, st.LinkUserID, e))
	}
}

func (h *Handler) completeSAML(c *gin.Context, st models.SAMLLoginState) *oidcError {
	cfg, e := h.samlConfigForState(st)
	if e != nil {
		return e
	}
	sp, e := samlServiceProviderFor(cfg)
	if e != nil {
		return e
	}
	assertion, e := verifySAMLResponse(sp, c.PostForm("SAMLResponse"), st, cfg.SAMLAllowUnspecifiedNameID)
	if e != nil {
		return e
	}
	if e := h.rememberSAMLAssertion(st, assertion); e != nil {
		return e
	}
	claims := oidcClaims{
		name:  samlAttribute(assertion, orDefault(cfg.SAMLAttributeName, models.SSODefaultAttributeName)),
		email: samlAttribute(assertion, orDefault(cfg.SAMLAttributeEmail, models.SSODefaultAttributeEmail)),
	}
	if st.TestLogin {
		ssoRedirect(c, samlTestResultURL(claims))
		return nil
	}
	key := models.UserSSOIdentity{
		StatusPageID: st.StatusPageID, Protocol: models.SSOProtocolSAML,
		Issuer: assertion.Issuer.Value, Subject: assertion.Subject.NameID.Value,
		SubjectFormat: samlNameIDFormat(assertion),
	}
	user, e := h.resolveSSOUser(st.LinkUserID, key, claims.email)
	if e != nil {
		return e
	}
	h.finishSSOLogin(c, user, claims.name, st.StatusPageID, st.LinkUserID, models.SSOProtocolSAML)
	return nil
}

// verifySAMLResponse prüft die Antwort des IdP. Bewusst ParseXMLResponse statt
// ParseResponse: Letzteres löste bei einem SAMLart-Parameter eine
// Artifact-Anfrage an den IdP aus. Als aktuelle URL gilt der ACS-URL aus
// BASE_URL, weil der Request-URL hinter dem Reverse-Proxy anders aussieht.
func verifySAMLResponse(sp *saml.ServiceProvider, encoded string, st models.SAMLLoginState, allowUnspecified bool) (*saml.Assertion, *oidcError) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(raw) == 0 {
		return nil, errSAMLResponseInvalid
	}
	if err := checkSAMLAssertionCount(raw); err != nil {
		slog.Warn("SAML response rejected", "page", st.StatusPageID, "error", err)
		return nil, errSAMLResponseInvalid
	}
	assertion, err := parseSAMLResponseSafely(sp, raw, st.RequestID)
	if err != nil {
		var bad saml.ErrBadStatus
		var ire *saml.InvalidResponseError
		if errors.As(err, &ire) && errors.As(ire.PrivateErr, &bad) {
			return nil, errOIDCProviderDenied
		}
		slog.Warn("SAML response rejected", "page", st.StatusPageID, "error", samlPrivateError(err))
		return nil, errSAMLResponseInvalid
	}
	return assertion, checkSAMLSubject(assertion, allowUnspecified)
}

// checkSAMLAssertionCount (Security-Review): Genau eine
// Assertion im ganzen Dokument und keine EncryptedAssertion. crewjam wertet
// mehrere Assertions aus und nimmt die erste gültige; eine zusätzliche Kopie
// (Signature Wrapping) oder eine verschlüsselte Assertion, deren Inhalt erst
// nach dem Entschlüsseln geprüft wird, hat hier keinen Nutzen und erweitert
// nur die Angriffsfläche. Gezählt wird nach lokalem Namen in jeder Tiefe, damit
// auch ein Versteck unter anderem Präfix oder in Extensions auffällt. Ungültiges
// XML lehnt ParseXMLResponse ohnehin ab; hier zählt es als null Assertions.
func checkSAMLAssertionCount(raw []byte) error {
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(raw); err != nil {
		return err
	}
	assertions := 0
	for _, el := range doc.FindElements("//*") {
		if el.Tag == "EncryptedAssertion" {
			return errors.New("EncryptedAssertion wird nicht angenommen")
		}
		if el.Tag == "Assertion" {
			assertions++
		}
	}
	if assertions != 1 {
		return fmt.Errorf("genau eine Assertion erwartet, gefunden: %d", assertions)
	}
	return nil
}

// parseSAMLResponseSafely fängt Paniken in crewjam ab: validateAssertion greift
// ohne Prüfung auf Conditions und SubjectConfirmationData zu. Erreichbar ist
// das nur mit gültiger Signatur, also vom IdP selbst, soll dann aber wie jede
// andere ungültige Antwort enden statt als 500.
func parseSAMLResponseSafely(sp *saml.ServiceProvider, raw []byte, requestID string) (a *saml.Assertion, err error) {
	defer func() {
		if r := recover(); r != nil {
			a, err = nil, fmt.Errorf("SAML-Antwort unvollständig: %v", r)
		}
	}()
	return sp.ParseXMLResponse(raw, []string{requestID}, sp.AcsURL)
}

// samlPrivateError holt den eigentlichen Grund aus crewjams
// InvalidResponseError (dessen Error() liefert absichtlich nur einen festen
// Text). Er landet nur im Log, nie in der Antwort.
func samlPrivateError(err error) error {
	var ire *saml.InvalidResponseError
	if errors.As(err, &ire) {
		return ire.PrivateErr
	}
	return err
}

// checkSAMLSubject: Zuordnung braucht eine stabile NameID. crewjam prüft
// InResponseTo, Recipient und Ablauf nur in vorhandenen SubjectConfirmations,
// ohne eine einzige wären diese Prüfungen übersprungen.
//
// Nur persistente NameIDs zählen. emailAddress nie:
// Eine Adresse vergibt der IdP-Admin neu (Konto-Übernahme nach Umbenennung).
// transient wechselt bei jeder Anmeldung. unspecified (auch ein fehlendes
// Format-Attribut, SAML-Core 8.3) nur mit allowUnspecified.
func checkSAMLSubject(a *saml.Assertion, allowUnspecified bool) *oidcError {
	if a.Subject == nil || a.Subject.NameID == nil || a.Subject.NameID.Value == "" ||
		len(a.Subject.SubjectConfirmations) == 0 || a.ID == "" {
		return errSAMLResponseInvalid
	}
	format := samlNameIDFormat(a)
	if format == string(saml.PersistentNameIDFormat) {
		return nil
	}
	if allowUnspecified && format == string(saml.UnspecifiedNameIDFormat) {
		return nil
	}
	return errSAMLNameIDFormat
}

// samlNameIDFormat: Ein fehlendes Format bedeutet unspecified.
func samlNameIDFormat(a *saml.Assertion) string {
	return orDefault(a.Subject.NameID.Format, string(saml.UnspecifiedNameIDFormat))
}

// rememberSAMLAssertion legt die Assertion-ID bis zum Ablauf der Assertion ab.
// Schlägt das INSERT am Unique-Index fehl, wurde sie schon einmal eingelöst.
// Auch jeder andere Fehler beim Speichern lehnt ab: Ohne Eintrag gäbe es
// keinen Replay-Schutz.
func (h *Handler) rememberSAMLAssertion(st models.SAMLLoginState, a *saml.Assertion) *oidcError {
	// SubjectConfirmationData ist hier gesetzt: crewjams validateAssertion
	// greift ohne Prüfung darauf zu (eine Panik dort fängt
	// parseSAMLResponseSafely ab).
	expires := a.Conditions.NotOnOrAfter
	for _, sc := range a.Subject.SubjectConfirmations {
		if sc.SubjectConfirmationData.NotOnOrAfter.After(expires) {
			expires = sc.SubjectConfirmationData.NotOnOrAfter
		}
	}
	use := models.SAMLAssertionUse{
		StatusPageID: st.StatusPageID,
		Issuer:       a.Issuer.Value,
		AssertionID:  a.ID,
		ExpiresAt:    expires.Add(saml.MaxClockSkew),
	}
	if err := h.DB.Create(&use).Error; err != nil {
		slog.Warn("SAML assertion replay rejected", "page", st.StatusPageID, "error", err)
		return errSAMLReplay
	}
	return nil
}

// samlAttribute liefert den ersten Wert des Attributs, dessen Name oder
// FriendlyName passt (Entra ID nennt die E-Mail etwa
// http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress, andere
// IdPs urn:oid:0.9.2342.19200300.100.1.3 mit FriendlyName mail).
func samlAttribute(a *saml.Assertion, name string) string {
	if name == "" {
		return ""
	}
	for _, stmt := range a.AttributeStatements {
		for _, attr := range stmt.Attributes {
			if (attr.Name == name || attr.FriendlyName == name) && len(attr.Values) > 0 {
				return attr.Values[0].Value
			}
		}
	}
	return ""
}

// samlTestResultURL wie oidcTestResultURL, dazu sso=saml, damit die SSO-Seite
// fehlende Attribute unter den SAML-Feldnamen anzeigt.
func samlTestResultURL(claims oidcClaims) string {
	target := oidcTestResultURL(claims)
	return strings.Replace(target, "oidc_test=ok", "oidc_test=ok&sso=saml", 1)
}
