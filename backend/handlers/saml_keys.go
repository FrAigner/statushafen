package handlers

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"strconv"
	"time"
)

// Schlüssel und Zertifikat des SAML-Service-Providers je
// Statusseite. Der IdP bekommt das Zertifikat über die SP-Metadaten; damit
// prüft er die Signatur der AuthnRequests (verschlüsselte Assertions nimmt
// der ACS nicht an, siehe checkSAMLAssertionCount). Ein eigenes Paar je Seite
// statt eines globalen, damit ein IdP keine signierten Requests einer anderen
// Seite annimmt und ein Wechsel nur eine Seite trifft.
//
// Ablage wie beim OIDC-Client-Secret (Spalte o_id_c_client_secret): im
// Klartext in der Datenbank, nie in einer API-Antwort. Das Repo kennt bisher
// keine Verschlüsselung ruhender Secrets (kein Schlüsselmaterial außerhalb
// der DB, das man dafür bräuchte); das gilt für alle Secrets gemeinsam und
// gehört in eine eigene Änderung.

// samlCertLifetime: Selbstsigniertes Zertifikat, lange gültig, weil ein
// Ablauf jede Seite still vom IdP trennen würde (viele IdPs prüfen das
// Ablaufdatum des SP-Zertifikats). Als Funktion wegen gremlins, siehe
// oidcStateLifetime.
func samlCertLifetime() time.Duration { return 10 * 365 * 24 * time.Hour }

// generateSAMLKeyPair erzeugt RSA-2048-Schlüssel und Zertifikat im PEM-Format.
// Fehler sind hier nicht möglich: crypto/rand bricht seit Go 1.24 bei einem
// Fehler selbst ab, und Vorlage samt Schlüssel sind fest und gültig.
func generateSAMLKeyPair(pageID uint) (keyPEM, certPEM string) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "statuspage SAML SP page " + strconv.FormatUint(uint64(pageID), 10)},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(samlCertLifetime()),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		BasicConstraintsValid: true,
	}
	der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	keyDER, _ := x509.MarshalPKCS8PrivateKey(key)
	keyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
	certPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	return keyPEM, certPEM
}

var errSAMLKeyInvalid = errors.New("SAML-SP-Schlüssel oder -Zertifikat ungültig")

func parseSAMLCert(certPEM string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return nil, errSAMLKeyInvalid
	}
	return x509.ParseCertificate(block.Bytes)
}

// parseSAMLKeyPair liest das gespeicherte Paar. Ein Fehler heißt: die Zeile
// wurde von Hand verändert; das Speichern im Admin-UI erzeugt kein neues Paar,
// solange eins gesetzt ist.
func parseSAMLKeyPair(keyPEM, certPEM string) (*rsa.PrivateKey, *x509.Certificate, error) {
	cert, err := parseSAMLCert(certPEM)
	if err != nil {
		return nil, nil, err
	}
	block, _ := pem.Decode([]byte(keyPEM))
	if block == nil {
		return nil, nil, errSAMLKeyInvalid
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, nil, err
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, nil, errSAMLKeyInvalid
	}
	return key, cert, nil
}
