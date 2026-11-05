// Entscheidet für frontend/e2e/sso-oidc.spec.js, ob der Spec mangels
// Mock-OIDC übersprungen wird. Lokal soll ein E2E-Lauf ohne den Dienst nicht rot
// werden; im Nachtjob (.github/workflows/e2e-sso-nachts.yml, E2E_REQUIRE_SSO_MOCK=1)
// wäre ein stilles Überspringen dagegen ein grüner Lauf ohne jede Prüfung.
// Liegt unter src/, damit Vitest die Entscheidung testet (e2e/ sammelt Vitest nicht ein).

export function ssoMockRequired(env) {
  return env.E2E_REQUIRE_SSO_MOCK === '1'
}

// Liefert null (Spec läuft) oder den Grund zum Überspringen; wirft, wenn der Mock
// fehlt, obwohl er verlangt ist. mock benennt den Dienst und seinen
// README-Abschnitt (Standard: Mock-OIDC; frontend/e2e/sso-saml.spec.js übergibt
// den Mock-SAML-IdP).
const OIDC_MOCK = { name: 'Mock-OIDC', readme: 'Lokale SSO-Tests mit Mock-OIDC' }

export function ssoMockSkipReason({ reachable, required, issuer, mock = OIDC_MOCK }) {
  if (reachable) return null
  const reason = `${mock.name} nicht erreichbar (${issuer}), siehe README "${mock.readme}"`
  if (required) {
    throw new Error(`${reason}; E2E_REQUIRE_SSO_MOCK=1 verlangt ihn, Überspringen ist hier ein Fehler`)
  }
  return reason
}
