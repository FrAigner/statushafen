// Entscheidet für frontend/e2e/sso-oidc.spec.js, ob der Spec mangels
// Mock-OIDC übersprungen wird. Lokal soll ein E2E-Lauf ohne den Dienst nicht rot
// werden; im Nachtjob (.github/workflows/e2e-sso-nachts.yml, E2E_REQUIRE_SSO_MOCK=1)
// wäre ein stilles Überspringen dagegen ein grüner Lauf ohne jede Prüfung.
// Liegt unter src/, damit Vitest die Entscheidung testet (e2e/ sammelt Vitest nicht ein).

export function ssoMockRequired(env) {
  return env.E2E_REQUIRE_SSO_MOCK === '1'
}

// Returns null (spec runs) or the reason to skip; throws when the mock is missing
// although it is required.
export function ssoMockSkipReason({ reachable, required, issuer }) {
  if (reachable) return null
  const reason = `Mock-OIDC nicht erreichbar (${issuer}), siehe README "Lokale SSO-Tests mit Mock-OIDC"`
  if (required) {
    throw new Error(`${reason}; E2E_REQUIRE_SSO_MOCK=1 verlangt ihn, Überspringen ist hier ein Fehler`)
  }
  return reason
}
