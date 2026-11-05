package models

import (
	"time"

	"gorm.io/gorm"
)

// StatusPage repräsentiert eine eigene Statusseite (Mandant)
type StatusPage struct {
	gorm.Model
	Name string `gorm:"not null"`
	// Slug ist unter den aktiven Seiten eindeutig. Bewusst KEIN gorm-Tag
	// uniqueIndex: Ein regulärer Index sieht auch soft-gelöschte
	// Seiten und sperrt deren Slug für immer. Der partielle Index
	// idx_status_pages_slug_active entsteht per Raw-SQL in
	// database.CreateSoftDeleteUniqueIndexes.
	Slug string `gorm:"not null"`
	// IsSystem kennzeichnet die Systemseite. Der Seed
	// findet sie über dieses Kennzeichen statt über den Slug: Ein geänderter Slug
	// ließ sonst beim nächsten Start eine zweite Systemseite entstehen. Höchstens
	// eine aktive Seite trägt das Kennzeichen (partieller Index
	// idx_status_pages_system_active in database.CreateSoftDeleteUniqueIndexes).
	IsSystem        bool `gorm:"not null;default:false"`
	Description     string
	LogoURL         string
	CustomDomain    string
	IsPublicIndexed bool  `gorm:"default:true"`
	IsPrivate       bool  `gorm:"default:false"`
	DefaultRoleID   *uint `gorm:"default:null"`
	// AutoJoinEmailDomains: kommagetrennte, kleingeschriebene
	// Allowlist von E-Mail-Domains. Auto-Join per DefaultRoleID greift nur für
	// Konten mit passender Domain; leer = kein Auto-Join.
	AutoJoinEmailDomains string `gorm:"default:''"`

	// WebhookTokenHash authentifiziert den eingehenden Monitoring-Webhook,
	// der Component-Status ohne normale Session/JWT-Auth setzen darf. Gespeichert
	// wird nur der Hash (HashToken, Spalte webhook_token); der Klartext wird bei
	// der Erzeugung einmal ausgeliefert. Leer =
	// kein Token vergeben, Webhook-Endpunkt lehnt dann jede Anfrage ab. Bewusst
	// per json:"-" von der normalen StatusPage-JSON-Repräsentation ausgeschlossen,
	// damit der Token nie über die generischen Read-Endpunkte (GET /api/pages, ...)
	// oder ein unbedachtes PUT-Bind mitgeschickt/überschrieben werden kann - er wird
	// ausschließlich über die dedizierten Generate/Revoke-Endpunkte gesetzt bzw.
	// einmalig zurückgegeben.
	WebhookTokenHash string `gorm:"column:webhook_token;default:null" json:"-"`

	// Branding: rein additive Felder für seiten-spezifisches
	// Aussehen der öffentlichen Statusseite (Logo, Farben, Header-Layout).
	// Die Defaults entsprechen exakt den bisherigen globalen CSS-Werten
	// (--primary / --text-secondary aus frontend/src/style.css und
	// "classic" als bisher einzigem Header-Layout), damit bestehende Seiten
	// nach der Migration optisch unverändert bleiben, bis ein Admin die
	// Felder aktiv setzt.
	PrimaryColor   string `gorm:"default:'#4c6ef5'"`
	SecondaryColor string `gorm:"default:'#5e6c84'"`
	// HeaderStyle: "classic" (Standard, bisheriges Layout), "centered",
	// "minimal" - siehe frontend/src/views/public/StatusPage.vue.
	HeaderStyle string `gorm:"not null;default:'classic'"`
	// PageLayout: Seiten-Layout der öffentlichen Seite,
	// "standard" (Default, bisheriges Aussehen) oder "liste". Additiv und
	// unabhängig von HeaderStyle.
	PageLayout string `gorm:"not null;default:'standard'"`

	// CustomDomainStatus (TXT-Verifizierung seit):
	// Verifikations-Pipeline für CustomDomain.
	// "" (leer)   -> keine Custom Domain gesetzt.
	// "pending"   -> Domain eingetragen, DNS-TXT-Eintrag (Owner-Nachweis) noch
	//                nicht (erfolgreich) geprüft.
	// "verified"  -> TXT-Eintrag mit CustomDomainVerificationToken gefunden;
	//                Owner-Nachweis erbracht. Caddys On-Demand-TLS darf für
	//                diese Domain ein Zertifikat anfordern, der Kunde muss nun
	//                noch den CNAME auf den Edge-Server setzen.
	// "active"    -> zusätzlich mind. einmal echter öffentlicher Traffic über die
	//                Domain beobachtet (Beweis, dass DNS+TLS Ende-zu-Ende funktionieren).
	// "check_failed" -> die letzte DNS-Prüfung ist fehlgeschlagen;
	//                CustomDomainCheckedAt/CustomDomainCheckError nennen Zeitpunkt
	//                und Grund. Wie "pending" nicht auflösbar. Bestehende
	//                Datensätze bleiben "pending", bis geprüft wird.
	CustomDomainStatus     string     `gorm:"default:''"`
	CustomDomainVerifiedAt *time.Time `gorm:"default:null"`
	// CustomDomainCheckedAt/-CheckError: Zeitpunkt der letzten
	// DNS-Prüfung und bei Fehlschlag der Grund als stabiler Code
	// ("txt_not_found" | "dns_lookup_failed"; die Texte kommen aus den
	// Sprachdateien). Beides leer, solange nie geprüft wurde.
	CustomDomainCheckedAt  *time.Time `gorm:"default:null"`
	CustomDomainCheckError string     `gorm:"default:''"`

	// CustomDomainVerificationToken: zufälliger, pro Domain-
	// Eintrag neu generierter Wert, den der Kunde als TXT-Record bei seinem
	// DNS-Provider hinterlegen muss (Wert: "statushafen-verify=<token>"),
	// um den Besitz der Domain nachzuweisen - unabhängig vom CNAME, der erst
	// danach für das eigentliche Routing gesetzt wird (Standard-Verfahren wie
	// bei Google Search Console/Mailgun). json:"-" wie WebhookToken, da der
	// Wert nur über die dedizierten Set/Verify-Endpunkte ausgegeben werden
	// soll, nicht über generische Read-Endpunkte inkl. öffentlicher Seiten.
	CustomDomainVerificationToken string `gorm:"default:''" json:"-"`

	// NotificationFromAddress: optionaler, seiten-spezifischer
	// Absender für Incident-/Wartungsfenster-Benachrichtigungsmails, im selben
	// "Anzeigename <adresse>"-Format wie die globale SMTP_FROM-Env-Var (siehe
	// smtp.go). Leer = Fallback auf SMTP_FROM (bisheriges Verhalten,
	// unverändert für Seiten ohne eigene Konfiguration).
	NotificationFromAddress string `gorm:"default:''"`
}

type Role struct {
	gorm.Model
	// Name ist unter den aktiven Rollen eindeutig. Bewusst KEIN gorm-Tag
	// uniqueIndex: Der partielle Index idx_roles_name_active
	// entsteht per Raw-SQL in database.CreateSoftDeleteUniqueIndexes, damit der
	// Name einer gelöschten Rolle wieder frei ist.
	Name        string `gorm:"not null"`
	Permissions string `gorm:"not null"`
}

// GlobalAdminRoleName: Name der Rolle, die Seed und SSO-Bootstrap
// als globale Rolle (User.RoleID) eines Global Admins setzen. Global Admin ist
// eine globale Eigenschaft (User.IsGlobalAdmin); als Seitenrolle vergeben,
// suggerierte sie Plattformrechte, gab aber nur die Permissions der Rolle auf
// dieser einen Seite. Sie ist deshalb nicht als Seitenrolle vergebbar.
const GlobalAdminRoleName = "Global Admin"

// GlobalOnly: true für die Rolle, die nicht als Seitenrolle
// (UserStatusPageRole, StatusPage.DefaultRoleID) vergeben werden darf.
func (r Role) GlobalOnly() bool {
	return r.Name == GlobalAdminRoleName
}

// SystemSetting: generische globale Key/Value-Systemeinstellungen.
// Erste Einstellung ist orders.enabled (Self-Service-Bestellung an/aus). Bewusst
// als erweiterbare Key/Value-Tabelle statt als einzelne Spalte, damit weitere
// globale Schalter ohne Schemaänderung folgen können. Konvention:
// ein fehlender Key bedeutet den jeweiligen Default (für orders.enabled "an"),
// damit bestehende Deployments unverändert bleiben.
type SystemSetting struct {
	gorm.Model
	Key   string `gorm:"uniqueIndex;not null"`
	Value string `gorm:"not null"`
}

type User struct {
	gorm.Model
	// Email ist unter den aktiven Benutzern eindeutig (partieller
	// Index idx_users_email_active statt gorm-uniqueIndex, s. Role.Name).
	Email        string `gorm:"not null"`
	PasswordHash string `json:"-"`
	RoleID       uint   `gorm:"default:null"`
	Role         Role   `gorm:"foreignKey:RoleID"`
	// AllowedCountries wurde durch UserTag ersetzt (Country → Tags
	// Schritt 4/4). Die alte DB-Spalte bleibt vorerst bestehen (Rollback-
	// Sicherheitsnetz), wird aber nicht mehr gelesen/geschrieben.
	SSOProvider   string `gorm:"default:null"`
	SSOSubject    string `gorm:"default:null"`
	IsGlobalAdmin bool   `gorm:"default:false"`
	// PreferredLanguage speichert die vom eingeloggten User gewählte UI-Sprache
	// (i18n-Infrastruktur). Rein additiv - AutoMigrate legt die Spalte
	// automatisch an, bestehende Nutzer erhalten den leeren Default (Frontend
	// fällt dann auf den Browser-/Fallback-Wert zurück). Anonyme Besucher
	// speichern ihre Sprachwahl stattdessen im localStorage des Browsers.
	PreferredLanguage string `gorm:"default:''"`
	// HideHelpTooltips: Hilfe-Tooltips im Admin-Bereich für
	// diesen User ausgeschaltet. Bewusst negiert gespeichert: der Default
	// "an" ist so der Nullwert false. Ein bool mit gorm-Default true würde
	// GORM beim Create mit false stillschweigend durch den Default ersetzen,
	// und AutoMigrate legt die Spalte für bestehende Nutzer mit false an.
	HideHelpTooltips bool `gorm:"default:false"`
	// TokenVersion wandert als Claim in jedes Login-Token und wird bei jeder
	// Anfrage mit dem Konto verglichen. Eine Erhöhung (Passwortwechsel,
	// Entzug von Rechten, Löschung) macht alle bis dahin ausgestellten Tokens
	// sofort ungültig, ohne dass dafür eine Sperrliste nötig ist.
	TokenVersion uint `gorm:"not null;default:0"`

	// Die verknüpften Rollen pro Statusseite
	UserStatusPageRoles []UserStatusPageRole `gorm:"foreignKey:UserID"`
}

// UserStatusPageRole: granulare Rechte pro Statusseite und User
type UserStatusPageRole struct {
	gorm.Model
	UserID       uint       `gorm:"not null;index"`
	User         User       `gorm:"foreignKey:UserID"`
	StatusPageID uint       `gorm:"not null;index"`
	StatusPage   StatusPage `gorm:"foreignKey:StatusPageID"`
	RoleID       uint       `gorm:"not null"`
	Role         Role       `gorm:"foreignKey:RoleID"`
}

// PageOrder: Self-Service-Bestellung einer neuen Statusseite über die
// öffentliche Startseite ("Eingangstor"-Homepage). Trackt den
// kompletten Lauf vom Bestellformular bis zur fertig provisionierten Seite,
// ohne vor der Email-Bestätigung schon einen einloggbaren User anzulegen -
// sonst wäre das öffentliche Formular selbst schon ein Vektor für beliebig
// viele leere User-Zeilen. Status-Übergänge: "pending_confirmation" (Formular
// abgeschickt, Bestätigungsmail raus) -> "confirmed" (Link geklickt,
// StatusPage/User/Rolle angelegt, Passwort noch nicht gesetzt) -> "active"
// (Passwort gesetzt, Login möglich). Abgelaufene, nie bestätigte Bestellungen
// zählen in den Prüfungen von CreateOrderHandler (Slug, Kontingent) sofort als
// abgelaufen; der Wartungs-Tick setzt sie auf "expired" und löscht sie später.
type PageOrder struct {
	gorm.Model
	CompanyName string `gorm:"not null"`
	// Email ist bewusst NICHT unique - ein Kontakt darf mehrere (historische)
	// Bestellungen haben. Der Missbrauchsschutz gegen Massen-Bestellungen
	// erfolgt stattdessen über den Pending-Cap in CreateOrderHandler (max. 3
	// gleichzeitig offene Bestellungen je Email) plus IP-Rate-Limiting.
	Email string `gorm:"not null;index"`
	// Slug: optionaler Wunsch-Slug aus dem Formular. Leer = wird beim
	// Bestätigen automatisch aus CompanyName generiert (wie bei
	// CreateStatusPageHandler ohne Slug-Angabe).
	Slug string `gorm:"default:null"`

	Status string `gorm:"not null;default:'pending_confirmation';index"`

	// ConfirmTokenHash/SetPasswordTokenHash: gleiches Muster wie
	// StatusPage.WebhookTokenHash. Die Token sind 32 Byte Zufall (hex-kodiert);
	// die Spalten confirm_token und set_password_token halten nur den Hash, der
	// Klartext steht in der Mail beziehungsweise in der Antwort. Bewusst nur
	// "index" statt "uniqueIndex": nach Verbrauch wird der Token auf ""
	// zurückgesetzt (siehe ConfirmOrderHandler/SetOrderPasswordHandler) - bei
	// mehreren bereits verbrauchten Bestellungen würde ein uniqueIndex auf
	// leeren Strings kollidieren (GORM/Postgres/SQLite behandeln "" als
	// normalen Wert, nicht wie NULL). Die faktische Eindeutigkeit ist über die
	// kryptografische Zufälligkeit der 256-Bit-Tokens gegeben, ein
	// DB-Constraint ist hier nicht nötig.
	ConfirmTokenHash   string     `gorm:"column:confirm_token;index;default:null" json:"-"`
	ConfirmTokenExpiry *time.Time `gorm:"default:null" json:"-"`

	SetPasswordTokenHash   string     `gorm:"column:set_password_token;index;default:null" json:"-"`
	SetPasswordTokenExpiry *time.Time `gorm:"default:null" json:"-"`

	StatusPageID *uint `gorm:"default:null"`
	UserID       *uint `gorm:"default:null"`

	// RequestIP: c.ClientIP() zum Bestellzeitpunkt, ausschließlich für die
	// Missbrauchs-Analyse im Audit-Log - keine funktionale Verwendung.
	RequestIP string `gorm:"default:null" json:"-"`
}

// Defaults der SSO-Config: gleiche Werte wie die default-Tags
// unten, damit der Handler leere Eingaben beim Anlegen und Aktualisieren
// gleich behandelt und GET ohne Config dieselben Werte meldet.
const (
	SSODefaultScopes         = "openid email profile"
	SSODefaultAttributeEmail = "email"
	SSODefaultAttributeName  = "name"
)

// StatusPageSSOConfig: Pro-Statusseite OIDC-Konfiguration.
// Leere Config = SSO deaktiviert. SAML wird später hinzugefügt (Phase 2).
type StatusPageSSOConfig struct {
	gorm.Model
	StatusPageID uint       `gorm:"not null;uniqueIndex" json:"status_page_id"`
	StatusPage   StatusPage `gorm:"foreignKey:StatusPageID" json:"-"`

	// OIDC-Konfiguration (OpenID Connect Discovery)
	OIDCEnabled        bool   `gorm:"default:false" json:"oidc_enabled"`
	OIDCDiscoveryURL   string `gorm:"default:''" json:"oidc_discovery_url"`
	OIDCClientID       string `gorm:"default:''" json:"oidc_client_id"`
	OIDCClientSecret   string `gorm:"default:''" json:"-"`
	OIDCScopes         string `gorm:"default:'openid email profile'" json:"oidc_scopes"`
	OIDCAttributeEmail string `gorm:"default:'email'" json:"oidc_attribute_email"`
	OIDCAttributeName  string `gorm:"default:'name'" json:"oidc_attribute_name"`
	// OIDCAttributeRole: nicht unterstützt. Eine Rolle aus einem
	// Claim des Seiten-IdP wäre eine Rechteausweitung an der Rollenverwaltung
	// vorbei; die Spalte bleibt nur für bestehende Daten und die API-Form.
	OIDCAttributeRole string `gorm:"default:''" json:"oidc_attribute_role"`
	// Rückruf-URL (vom System generiert, schreibgeschützt)
	OIDCACSUrl string `gorm:"column:oidc_acs_url;default:''" json:"oidc_acs_url"`
	// TestLogin deaktivieren wenn Live
	TestLoginEnabled bool `gorm:"default:true" json:"test_login_enabled"`

	// SAML-Konfiguration. Die IdP-Metadaten kommen entweder per
	// URL (nur https, SSRF-Schutz wie OIDC-Discovery) oder als eingefügtes XML,
	// nie beides. Schlüssel und Zertifikat des Service Providers erzeugt das
	// Backend je Seite selbst (handlers/saml_keys.go). Der private Schlüssel wird
	// wie das OIDC-Client-Secret behandelt: nur in der DB, nie in einer
	// API-Antwort (json:"-"). Explizite Spaltennamen, weil GORM aus "SAML"
	// sonst s_a_m_l macht.
	SAMLEnabled        bool   `gorm:"column:saml_enabled;default:false" json:"saml_enabled"`
	SAMLIdPMetadataURL string `gorm:"column:saml_idp_metadata_url;default:''" json:"saml_idp_metadata_url"`
	SAMLIdPMetadataXML string `gorm:"column:saml_idp_metadata_xml;type:text;default:''" json:"saml_idp_metadata_xml"`
	SAMLAttributeEmail string `gorm:"column:saml_attribute_email;default:'email'" json:"saml_attribute_email"`
	SAMLAttributeName  string `gorm:"column:saml_attribute_name;default:'name'" json:"saml_attribute_name"`
	SAMLSPKey          string `gorm:"column:saml_sp_key;type:text;default:''" json:"-"`
	SAMLSPCertificate  string `gorm:"column:saml_sp_certificate;type:text;default:''" json:"saml_sp_certificate"`
	// SAMLAllowUnspecifiedNameID): Standard ist nur
	// eine persistente NameID. Mit dem Schalter nimmt der ACS auch
	// "unspecified" an; das kann je nach IdP eine änderbare Kennung sein
	// (Benutzername), deshalb nur auf ausdrücklichen Wunsch des Seiten-Admins.
	SAMLAllowUnspecifiedNameID bool `gorm:"column:saml_allow_unspecified_nameid;default:false" json:"saml_allow_unspecified_nameid"`
}

// OIDCLoginState hält State, Nonce und PKCE-Verifier eines
// laufenden OIDC-Logins einer Statusseite bis zum Callback. Bewusst in der DB
// statt im Prozessspeicher (wie ssoCodeStore), weil zwischen Start und
// Callback der IdP-Login liegt und ein Neustart des Backends sonst jeden
// offenen Login abbräche. Ohne gorm.Model (kein Soft-Delete): Der Callback
// löscht die Zeile hart, damit ein State genau einmal einlösbar ist.
// LinkUserID != 0 markiert einen Verknüpfungs-Login aus einem bestehenden,
// angemeldeten Konto. TestLogin markiert die Testanmeldung eines
// Seiten-Admins aus der SSO-Seite: Sie prüft nur den Ablauf beim Anbieter,
// ohne Sitzung und ohne Verknüpfung.
type OIDCLoginState struct {
	ID           uint      `gorm:"primaryKey"`
	State        string    `gorm:"not null;uniqueIndex"`
	Nonce        string    `gorm:"not null"`
	CodeVerifier string    `gorm:"not null"`
	StatusPageID uint      `gorm:"not null"`
	LinkUserID   uint      `gorm:"not null;default:0"`
	TestLogin    bool      `gorm:"not null;default:false"`
	ExpiresAt    time.Time `gorm:"not null;index"`
	CreatedAt    time.Time
}

// SAMLLoginState ist das SAML-Gegenstück zu OIDCLoginState: Es
// hält die ID des AuthnRequests bis zur Antwort des IdP am ACS-Endpunkt.
// State steckt als RelayState im Request und zusätzlich im Cookie saml_state
// (gegen Login-CSRF). Der ACS löscht die Zeile hart (DELETE … RETURNING),
// damit jede Request-ID genau einmal einlösbar ist; nur Antworten mit
// passendem InResponseTo werden angenommen (kein IdP-initiierter Login).
type SAMLLoginState struct {
	ID           uint      `gorm:"primaryKey"`
	State        string    `gorm:"not null;uniqueIndex"`
	RequestID    string    `gorm:"not null"`
	StatusPageID uint      `gorm:"not null"`
	LinkUserID   uint      `gorm:"not null;default:0"`
	TestLogin    bool      `gorm:"not null;default:false"`
	ExpiresAt    time.Time `gorm:"not null;index"`
	CreatedAt    time.Time
}

// SAMLAssertionUse merkt sich die ID jeder angenommenen Assertion
// bis zu ihrem Ablauf (Replay-Schutz zusätzlich zur einmaligen Request-ID).
// Der Unique-Index auf (Issuer, AssertionID) lässt ein zweites Einlösen am
// INSERT scheitern, auch bei zwei gleichzeitigen Anfragen.
type SAMLAssertionUse struct {
	ID           uint      `gorm:"primaryKey"`
	StatusPageID uint      `gorm:"not null"`
	Issuer       string    `gorm:"not null;uniqueIndex:idx_saml_assertion_use"`
	AssertionID  string    `gorm:"not null;uniqueIndex:idx_saml_assertion_use"`
	ExpiresAt    time.Time `gorm:"not null;index"`
	CreatedAt    time.Time
}

// SSO-Protokolle einer UserSSOIdentity.
const (
	SSOProtocolOIDC = "oidc"
	SSOProtocolSAML = "saml"
)

// UserSSOIdentity verknüpft ein Konto mit einer Identität beim
// OIDC-Anbieter einer Statusseite. Ein Login über das Seiten-SSO findet das
// Konto nur über (Seite, Issuer, Subject), nie über die E-Mail:
// Den IdP einer Seite pflegt deren Admin, eine E-Mail-Zuordnung erlaubte ihm
// sonst die Übernahme fremder Konten. Kein gorm.Model, damit eine gelöschte
// Verknüpfung den Unique-Index nicht weiter belegt. Email hält den
// Wert des konfigurierten E-Mail-Claims (oidc_attribute_email), nur zur Anzeige
// im Profil, nie für die Zuordnung.
//
// Protocol trennt OIDC- und SAML-Identitäten (Bestandszeilen
// bekommen über den Spalten-Default "oidc"). Es gehört zum Unique-Index, weil
// manche IdPs (z.B. Keycloak) für OIDC-Issuer und SAML-Entity-ID denselben
// String verwenden; für SAML sind Issuer die Entity-ID des IdP und Subject die
// NameID. Der frühere Index idx_user_sso_identity ohne Protokoll wird in
// database.dropLegacySSOIdentityIndex entfernt.
//
// SubjectFormat ist das NameID-Format einer
// SAML-Identität (persistent oder, wenn freigegeben, unspecified) und gehört
// zum Schlüssel: Derselbe Wert in einem anderen Format ist eine andere
// Kennung. Bei OIDC leer. Der Index heißt deshalb idx_user_sso_identity_key.
type UserSSOIdentity struct {
	ID            uint   `gorm:"primaryKey"`
	UserID        uint   `gorm:"not null;index"`
	StatusPageID  uint   `gorm:"not null;uniqueIndex:idx_user_sso_identity_key"`
	Protocol      string `gorm:"not null;default:'oidc';uniqueIndex:idx_user_sso_identity_key"`
	Issuer        string `gorm:"not null;uniqueIndex:idx_user_sso_identity_key"`
	Subject       string `gorm:"not null;uniqueIndex:idx_user_sso_identity_key"`
	SubjectFormat string `gorm:"not null;default:'';uniqueIndex:idx_user_sso_identity_key"`
	Email         string `gorm:"not null;default:''"`
	CreatedAt     time.Time
}

// Ensure StatusPageSSOConfig scopes json fields per page config
func (sso *StatusPageSSOConfig) ToPublicResponse() map[string]interface{} {
	return map[string]interface{}{
		"status_page_id":       sso.StatusPageID,
		"oidc_enabled":         sso.OIDCEnabled,
		"oidc_discovery_url":   sso.OIDCDiscoveryURL,
		"oidc_client_id":       sso.OIDCClientID,
		"oidc_scopes":          sso.OIDCScopes,
		"oidc_attribute_email": sso.OIDCAttributeEmail,
		"oidc_attribute_name":  sso.OIDCAttributeName,
		"oidc_attribute_role":  sso.OIDCAttributeRole,
		"oidc_acs_url":         sso.OIDCACSUrl,
		"test_login_enabled":   sso.TestLoginEnabled,
		// Das Secret selbst bleibt geheim; das Admin-Formular
		// muss nur wissen, ob eins hinterlegt ist.
		"oidc_client_secret_set": sso.OIDCClientSecret != "",
		// SAML. Der SP-Schlüssel fehlt bewusst, das Zertifikat ist
		// öffentlich (steht ohnehin in den SP-Metadaten).
		"saml_enabled":                  sso.SAMLEnabled,
		"saml_idp_metadata_url":         sso.SAMLIdPMetadataURL,
		"saml_idp_metadata_xml":         sso.SAMLIdPMetadataXML,
		"saml_attribute_email":          sso.SAMLAttributeEmail,
		"saml_attribute_name":           sso.SAMLAttributeName,
		"saml_sp_certificate":           sso.SAMLSPCertificate,
		"saml_allow_unspecified_nameid": sso.SAMLAllowUnspecifiedNameID,
	}
}

// Component unterstützt beliebig tiefe Verschachtelung über ParentID (Ordnerstruktur)
// Wenn ParentID == nil → Top-Level Gruppe oder Einzelkomponente
// Wenn Status == "group" → reine Gruppe (kein eigener Status, ergibt sich aus Kindern)
type Component struct {
	gorm.Model
	StatusPageID uint `gorm:"not null;index"`
	// Pointer + omitempty, damit die API kein leeres, nie
	// geladenes StatusPage-Objekt pro Komponente ausliefert.
	StatusPage *StatusPage `gorm:"foreignKey:StatusPageID" json:",omitempty"`

	// Selbstreferenz: nil = Root-Element
	ParentID *uint      `gorm:"index;default:null"`
	Parent   *Component `gorm:"foreignKey:ParentID"`
	// Children werden per API-Handler befüllt (nicht direkt in GORM geladen, um N+1 zu vermeiden)
	Children []Component `gorm:"-"` // wird manuell befüllt
	// Tags (Ersatz für das frühere Country-Feld) - wie Children
	// bewusst nicht als GORM-Relation, sondern manuell im Handler befüllt
	// (siehe handlers/tag_access.go, tagsForComponent), um die vorhandene
	// ComponentTag-Join-Tabelle ohne implizites many2many-Tag anzusprechen.
	Tags []Tag `gorm:"-"`

	Name string `gorm:"not null"`
	// Slug: Kennung für API und Integrationen, je Statusseite
	// unter aktiven Zeilen eindeutig (partieller Unique-Index per Raw-SQL in
	// database/db.go, bewusst kein uniqueIndex-Tag) und nach dem Anlegen
	// unveränderlich. Leer nur bei Altzeilen vor dem Backfill.
	Slug        string `gorm:"not null;default:''"`
	Description string
	// Visible: false blendet die Komponente (bei einer Gruppe samt
	// Mitgliedern) auf der öffentlichen Statusseite, in der Verfügbarkeit, im
	// Abonnement-Assistenten und im öffentlichen Gesamtstatus aus. Im Admin
	// bleibt sie sichtbar, Webhook und Statusänderung funktionieren weiter.
	// Default true, damit Bestandszeilen beim AutoMigrate (ADD COLUMN ... DEFAULT
	// true) sichtbar bleiben. Achtung GORM: false ist der Nullwert und wird beim
	// Create durch den Default ersetzt; ein unsichtbar anzulegender Eintrag
	// braucht danach ein explizites Update (siehe CreateComponentHandler).
	Visible bool `gorm:"not null;default:true"`
	// ArchivedAt: nicht nil = archiviert. Anders als der
	// GORM-Soft-Delete (DeletedAt, nicht abrufbar) bleibt die Komponente samt
	// Verlauf, Verfügbarkeit, Vorfall-Zuordnungen und Übersetzungen
	// erhalten und ist wiederherstellbar. Archivierte Komponenten verhalten sich
	// öffentlich wie unsichtbare (Statusseite, Verfügbarkeit, Gesamtstatus) und
	// fehlen im Admin in den Auswahlen; die Liste zeigt sie nur im Segment
	// „Archiviert“. Eine Gruppe archiviert ihre Mitglieder mit (gleicher Zeitpunkt).
	ArchivedAt *time.Time `gorm:"index"`
	// Status: "operational","degraded_performance","partial_outage","major_outage","group"
	// "group" = reine Ordner-Ebene, Status wird aus Kindern aggregiert
	Status string `gorm:"not null;default:'operational'"`
	// IsGroup kennzeichnet in API-Antworten eine Gruppe. Nötig,
	// weil buildTree Status bei Gruppen durch den schlechtesten Kindstatus
	// ersetzt; der Admin erkannte die Gruppe sonst nicht und machte sie beim
	// Speichern zur normalen Komponente. Nicht in der DB, im Handler gesetzt
	// wie Children/Tags.
	IsGroup bool `gorm:"-"`
	// Country wurde durch ComponentTag ersetzt (Country → Tags
	// Schritt 4/4). Die alte DB-Spalte bleibt vorerst bestehen (Rollback-
	// Sicherheitsnetz), wird aber nicht mehr gelesen/geschrieben.
	// Sortierung innerhalb einer Gruppe
	SortOrder int `gorm:"default:0"`

	// effektiver Status (schlimmster aus eigenem Status, offenen
	// Vorfällen und gekoppelten externen Komponenten) samt Herkunft. Nicht
	// persistiert, nur in den Listen-Antworten (Komponentenbaum, Dashboard)
	// befüllt; Status selbst bleibt der gespeicherte Wert, damit beim
	// Entkoppeln nichts verloren geht und bestehende Clients unverändert bleiben.
	EffectiveStatus string              `gorm:"-" json:"effective_status,omitempty"`
	ExternalSources []ExternalStatusRef `gorm:"-" json:"external_sources,omitempty"`
}

type Incident struct {
	gorm.Model
	StatusPageID uint `gorm:"not null;index"`
	// Pointer + omitempty wie Component.StatusPage
	// - die nie geladene Relation lieferte sonst in jeder
	// (auch öffentlichen) Vorfall-Antwort ein leeres StatusPage-Objekt mit
	// dem kompletten internen Konfigurations-Schema der Seite.
	StatusPage *StatusPage `gorm:"foreignKey:StatusPageID" json:",omitempty"`
	Title      string      `gorm:"not null"`
	Message    string      `gorm:"not null"`
	Status     string      `gorm:"not null;default:'investigating'"`
	// Severity: Schweregrad des Vorfalls für die gewichtete
	// Verfügbarkeit (degraded_performance | partial_outage | major_outage).
	// Altdaten und Wartungen tragen den Default.
	Severity string `gorm:"not null;default:'degraded_performance'"`
	// Country wurde durch IncidentTag ersetzt (Country → Tags
	// Schritt 4/4). Die alte DB-Spalte bleibt vorerst bestehen (Rollback-
	// Sicherheitsnetz), wird aber nicht mehr gelesen/geschrieben.
	// Mehrfachzuordnung zu Komponenten (ersetzt die frühere 1:1-FK
	// ComponentID/Component). Bewusst als explizites Join-Modell
	// (IncidentComponent) statt GORM many2many-Tag, analog zu
	// UserStatusPageRole - vermeidet Tabellen-Kollisionen und lässt sich um
	// weitere Felder (SortOrder) erweitern.
	IncidentComponents []IncidentComponent `gorm:"foreignKey:IncidentID" json:"-"`

	// Wartungsfenster: erweitert Incident statt eines eigenen
	// Modells zu sein, damit Timeline/Notifications/Komponenten-Zuordnung des
	// Incidents direkt mitgenutzt werden. Status-Lebenszyklus für Wartungsfenster:
	// "scheduled" -> "in_progress" -> "resolved" (bewusst derselbe Terminal-
	// Status wie bei Incidents, damit der bestehende "aktive Incidents"-Filter
	// (models.IsResolvedIncidentStatus: resolved) auf der öffentlichen Statusseite unverändert
	// funktioniert). Wird vom Scheduler in backend/scheduler/maintenance.go
	// automatisch weitergeschaltet.
	IsMaintenance       bool       `gorm:"not null;default:false"`
	ScheduledStartAt    *time.Time `gorm:""`
	ScheduledEndAt      *time.Time `gorm:""`
	ReminderLeadMinutes int        `gorm:"not null;default:0"` // 0 = keine Erinnerung
	ReminderSentAt      *time.Time `gorm:""`

	// Ereigniszeiten normaler Vorfälle: optionaler Beginn/Ende,
	// damit sich Historie nachtragen lässt. CreatedAt bleibt der technische
	// Zeitstempel; für Wartungen gelten ScheduledStartAt/ScheduledEndAt.
	StartedAt  *time.Time `gorm:""`
	ResolvedAt *time.Time `gorm:""`
}

// IncidentComponent: explizite Join-Tabelle Incident <-> Component (many-to-many).
// Ersetzt die frühere 1:1-FK Incident.ComponentID/Component.
type IncidentComponent struct {
	gorm.Model
	IncidentID  uint      `gorm:"not null;index"`
	Incident    Incident  `gorm:"foreignKey:IncidentID;constraint:OnDelete:CASCADE"`
	ComponentID uint      `gorm:"not null;index"`
	Component   Component `gorm:"foreignKey:ComponentID"`
	SortOrder   int       `gorm:"default:0"`
}

// Tag ist das neue, frei definierbare Pendant zu den bisherigen
// Country-Strings (Schritt 1/4 - Country → Tags). Rein
// additiv: läuft zunächst nur parallel zu Component.Country, Incident.Country
// und User.AllowedCountries mit, ohne dass die bestehende Zugriffsprüfung
// (UserHasAccessToCountry) davon Gebrauch macht - das folgt erst in Schritt
// 2/4.
//
// StatusPageID == nil markiert einen globalen (repo-/instanzweiten) Tag,
// andernfalls ist der Tag an genau eine Statusseite gebunden. Da Postgres
// NULL-Werte in einem normalen Unique-Index als paarweise verschieden
// behandelt, reicht ein Unique-Index auf (Name, StatusPageID) NICHT aus, um
// doppelte globale Tags zu verhindern - siehe die beiden partiellen
// Unique-Indizes idx_tags_global_name_active / idx_tags_scoped_name_active (nur aktive Zeilen), die
// CreateSoftDeleteUniqueIndexes() in backend/database/db.go per Raw-SQL anlegt
// (GORM AutoMigrate erzeugt keine partiellen Indizes).
type Tag struct {
	gorm.Model
	Name         string      `gorm:"not null"`
	StatusPageID *uint       `gorm:"index"`
	StatusPage   *StatusPage `gorm:"foreignKey:StatusPageID"`
}

// ComponentTag: explizite Join-Tabelle Component <-> Tag (many-to-many),
// analog zu IncidentComponent - bewusst kein GORM many2many-Tag.
type ComponentTag struct {
	gorm.Model
	ComponentID uint      `gorm:"not null;index"`
	Component   Component `gorm:"foreignKey:ComponentID;constraint:OnDelete:CASCADE"`
	TagID       uint      `gorm:"not null;index"`
	Tag         Tag       `gorm:"foreignKey:TagID;constraint:OnDelete:CASCADE"`
}

// IncidentTag: explizite Join-Tabelle Incident <-> Tag (many-to-many),
// analog zu IncidentComponent - bewusst kein GORM many2many-Tag.
type IncidentTag struct {
	gorm.Model
	IncidentID uint     `gorm:"not null;index"`
	Incident   Incident `gorm:"foreignKey:IncidentID;constraint:OnDelete:CASCADE"`
	TagID      uint     `gorm:"not null;index"`
	Tag        Tag      `gorm:"foreignKey:TagID;constraint:OnDelete:CASCADE"`
}

// UserTag: explizite Join-Tabelle User <-> Tag (many-to-many), analog zu
// UserStatusPageRole - bewusst kein GORM many2many-Tag.
type UserTag struct {
	gorm.Model
	UserID uint `gorm:"not null;index"`
	User   User `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE"`
	TagID  uint `gorm:"not null;index"`
	Tag    Tag  `gorm:"foreignKey:TagID;constraint:OnDelete:CASCADE"`
}

// GeneralNotice: seitenweite Ankündigungen, die nicht an einen
// konkreten Incident/Wartungsfenster gebunden sind (z.B. Richtlinien-
// änderungen, organisatorische Hinweise). Bewusst ein eigenes, schlankes
// Modell statt einer Erweiterung von Incident (anders als bei Wartungs-
// fenstern): Notices haben keinen Status-Lebenszyklus und keine
// Komponenten-Zuordnung, sondern nur einen Sichtbarkeits-Schalter (IsActive)
// und ein optionales Zeitfenster (StartAt/EndAt).
type GeneralNotice struct {
	gorm.Model
	StatusPageID uint `gorm:"not null;index"`
	// siehe Incident.StatusPage.
	StatusPage *StatusPage `gorm:"foreignKey:StatusPageID" json:",omitempty"`
	Title      string      `gorm:"not null"`
	Message    string      `gorm:"not null"` // Markdown-fähig, wird als reiner String gespeichert
	// Level: "info" | "warning" | "critical"
	Level string `gorm:"not null;default:'info'"`
	// IsActive: bewusst KEIN gorm "default:true" - GORM behandelt bool-
	// ZeroValues (false) bei "default"-Tags als "nicht gesetzt" und würde sie
	// beim Insert stillschweigend auf den DB-Default zurücksetzen, sodass sich
	// eine explizit inaktive Notice nie anlegen ließe. Der Default "aktiv" wird
	// stattdessen anwendungsseitig in CreateNoticeHandler erzwungen.
	IsActive bool `gorm:"not null"`
	// Collapsed: reduziert Platz auf der Statusseite - Notice erscheint
	// hinter einem aufklappbaren Header statt direkt ausgeschrieben.
	Collapsed bool       `gorm:"not null;default:false"`
	StartAt   *time.Time `gorm:""`
	EndAt     *time.Time `gorm:""`

	// NotifySubscribers: steuert, ob das Anlegen/Aktivieren
	// dieser Notice eine Mail-Benachrichtigung an alle passenden Subscriber
	// auslöst. Bewusst opt-in (Default false) - rein kosmetische Notices
	// (z.B. "wir haben ein neues Logo") sollen niemanden ungefragt
	// zuspammen. Existierende Notices behalten das bisherige Verhalten
	// (kein Mailversand), da AutoMigrate die neue Spalte auf false setzt.
	NotifySubscribers bool `gorm:"not null;default:false"`
}

// IncidentTranslation (Stage 2): Übersetzungen von Incident-Title/Message
// mit Draft/Confirmed-Status. Nur bestätigte werden auf der öffentlichen Seite gezeigt.
type IncidentTranslation struct {
	gorm.Model
	IncidentID   uint     `gorm:"not null"`
	Incident     Incident `gorm:"foreignKey:IncidentID;constraint:OnDelete:CASCADE"`
	LanguageCode string   `gorm:"not null"`
	Title        string   `gorm:"not null"`
	Message      string   `gorm:"not null"`
	Status       string   `gorm:"not null;default:'draft'"`
	Source       string   `gorm:"not null;default:'manual'"`
}

// ComponentTranslation (Stage 3): Übersetzungen von Component-Name/Description
// mit Draft/Confirmed-Status. Nur bestätigte werden auf der öffentlichen Seite gezeigt.
type ComponentTranslation struct {
	gorm.Model
	ComponentID  uint      `gorm:"not null"`
	Component    Component `gorm:"foreignKey:ComponentID;constraint:OnDelete:CASCADE"`
	LanguageCode string    `gorm:"not null"`
	Title        string    `gorm:"not null"`
	Description  string    `gorm:"not null"`
	Status       string    `gorm:"not null;default:'draft'"`
	Source       string    `gorm:"not null;default:'manual'"`
}

// GeneralNoticeTranslation (Stage 4): Übersetzungen von Notice-Title/Message
// mit Draft/Confirmed-Status. Nur bestätigte werden auf der öffentlichen Seite gezeigt.
type GeneralNoticeTranslation struct {
	gorm.Model
	GeneralNoticeID uint          `gorm:"not null"`
	GeneralNotice   GeneralNotice `gorm:"foreignKey:GeneralNoticeID;constraint:OnDelete:CASCADE"`
	LanguageCode    string        `gorm:"not null"`
	Title           string        `gorm:"not null"`
	Message         string        `gorm:"not null"`
	Status          string        `gorm:"not null;default:'draft'"`
	Source          string        `gorm:"not null;default:'manual'"`
}

// IncidentTemplate: Vorlage für wiederkehrende Vorfälle/
// Wartungsfenster, damit Title/Message/Status/Komponenten nicht jedes Mal neu
// eingetippt werden müssen. Bewusst KEINE FK-Beziehung von Incident zurück
// zur Vorlage - beim Anlegen werden die Feldwerte nur kopiert
// (siehe CreateIncidentHandler/TemplateID-Handling), damit das Löschen einer
// Vorlage nie bereits erstellte Incidents/Wartungsfenster beeinflusst.
type IncidentTemplate struct {
	gorm.Model
	StatusPageID  uint       `gorm:"not null;index"`
	StatusPage    StatusPage `gorm:"foreignKey:StatusPageID"`
	Name          string     `gorm:"not null"`                    // interner Vorlagenname, z.B. "DB-Wartung"
	Kind          string     `gorm:"not null;default:'incident'"` // "incident" | "maintenance"
	Title         string     `gorm:"not null"`
	Message       string     `gorm:"not null"`
	DefaultStatus string     `gorm:""`
	// ComponentIDs: vorausgewählte Komponenten, komma-getrennt (analog zu
	// Subscriber.SubscribedComponentIDs) statt eigener Join-Tabelle, da hier
	// keine referenzielle Integrität benötigt wird - die IDs werden beim
	// Anlegen eines Incidents nur einmalig als Vorbefüllung ausgelesen.
	ComponentIDs string `gorm:""`
}

// IncidentTemplateTranslation: Übersetzung von Titel und Nachricht
// einer Vorlage in eine weitere Sprache der Seite. Die Standardsprache ist die
// Vorlage selbst. Beim „Vorlage verwenden“ werden die Texte mit ins Formular
// übernommen. Status wie bei den anderen Übersetzungen: draft | confirmed.
type IncidentTemplateTranslation struct {
	gorm.Model
	IncidentTemplateID uint             `gorm:"not null"`
	IncidentTemplate   IncidentTemplate `gorm:"foreignKey:IncidentTemplateID;constraint:OnDelete:CASCADE"`
	LanguageCode       string           `gorm:"not null"`
	Title              string           `gorm:"not null"`
	Message            string           `gorm:"not null"`
	Status             string           `gorm:"not null;default:'draft'"`
}

// NotificationTemplate: pro Statusseite und Benachrichtigungstyp
// hinterlegbare Vorlage für Subject und HTML-Body von Benachrichtigungs-Mails,
// gerendert per Go html/template. Fehlt für einen (StatusPageID, Kind,
// Audience, Language)-Tupel ein Eintrag, greift der hartkodierte
// Default-Renderer (buildNotificationEmailHTML) - bestehendes Verhalten
// bleibt so unverändert, solange kein Admin ein eigenes Template hinterlegt.
//
// Kind identifiziert den Anlass (Incident-/Wartungsfenster-/Notice-
// Benachrichtigungen, siehe die NotificationKind*-Konstanten in
// handlers/notification_template.go).
//
// Audience (Stage 5 der Mehrsprachigkeit): "staff" (interne
// User) | "subscriber" (öffentliche Abonnenten). Vorher steckte diese
// Unterscheidung implizit im Language-Feld selbst (Language=en meinte
// "interne Kopie", Language=de meinte "Subscriber-Kopie" - siehe
// handlers/incident.go buildNotificationDispatchContext) und kollidierte
// dadurch mit jeder weiteren echten Sprache, die nicht zufällig "en"/"de"
// hieß. Mit Audience als eigenem Feld ist Language jetzt ein echter,
// gegen den Sprachkatalog (models.Language/StatusPageLanguage)
// validierter Locale-Code statt einer verkappten Zielgruppen-Angabe.
//
// (StatusPageID, Kind, Audience, Language) ist unter den aktiven Zeilen unique
// (partieller Index idx_notification_template_active in
// database.CreateSoftDeleteUniqueIndexes, kein gorm-Tag) - ein Tenant hat pro
// Kombination höchstens ein Custom-Template.
type NotificationTemplate struct {
	gorm.Model
	StatusPageID uint       `gorm:"not null"`
	StatusPage   StatusPage `gorm:"foreignKey:StatusPageID"`
	Kind         string     `gorm:"not null"`
	Audience     string     `gorm:"not null;default:'staff'"`
	Language     string     `gorm:"not null"`
	Subject      string     `gorm:"not null"`
	BodyHTML     string     `gorm:"not null"`
}

type Subscriber struct {
	gorm.Model
	StatusPageID           uint       `gorm:"not null;index"`
	StatusPage             StatusPage `gorm:"foreignKey:StatusPageID"`
	Email                  string     `gorm:"default:null"`             // leer bei Google Chat
	WebhookURL             string     `gorm:"default:null"`             // leer bei E-Mail
	Channel                string     `gorm:"not null;default:'email'"` // "email" | "google_chat"
	SubscribedComponentIDs string     `gorm:"not null;default:'all'"`
	// Language: gewünschte Sprache der Benachrichtigungsmails, ein
	// bei der Seite aktiver Sprachcode. Leer = Standardsprache der Statusseite.
	Language string `gorm:"not null;default:''"`
	// CreatedByUserID verknüpft das Abo mit dem anlegenden User (falls angemeldet),
	// damit dieser es später über /me/subscription wiederfinden und bearbeiten kann
	// – vorher gab es dafür nur den fragilen Email-String-Abgleich, der bei
	// admin-angelegten Webhook-Abos (ohne Email) komplett ins Leere lief.
	CreatedByUserID *uint `gorm:"default:null;index"`
	CreatedBy       *User `gorm:"foreignKey:CreatedByUserID"`

	// Double-Opt-in: Status "pending" = E-Mail-Adresse noch nicht
	// per Link bestätigt, "active" = bestätigt bzw. ohne Bestätigung gültig
	// (eigene Konto-Adresse, Webhook-Kanäle). Benachrichtigungen gehen nur an
	// "active". Der DB-Default "active" sorgt dafür, dass AutoMigrate alle
	// Bestandsabos beim Anlegen der Spalte als bestätigt übernimmt (zusätzlich
	// abgesichert per database.backfillSubscriberStatus).
	Status string `gorm:"not null;default:'active';index"`
	// ConfirmTokenHash: SHA-256 (hex) des Bestätigungstokens. Der Klartext
	// steht nur im Mail-Link, damit ein DB-Leak keine offenen Abos bestätigen
	// kann. Nach der Bestätigung leer, wie PageOrder.ConfirmToken.
	ConfirmTokenHash   string     `gorm:"index;default:null" json:"-"`
	ConfirmTokenExpiry *time.Time `gorm:"default:null" json:"-"`
}

// Subscriber-Status-Werte.
const (
	SubscriberStatusPending = "pending"
	SubscriberStatusActive  = "active"
)
