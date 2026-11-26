// Decides for frontend/e2e/sso-oidc.spec.js whether the spec is skipped for lack
// of a mock OIDC. Locally an E2E run without the service should not turn red;
// in the nightly job (.github/workflows/e2e-sso-nachts.yml, E2E_REQUIRE_SSO_MOCK=1)
// a silent skip would instead be a green run without any check.
// Lives under src/ so that Vitest tests the decision (Vitest does not collect e2e/).

export function ssoMockRequired(env: Record<string, string | undefined>): boolean {
  return env.E2E_REQUIRE_SSO_MOCK === '1'
}

// Returns null (spec runs) or the reason to skip; throws when the mock is missing
// although it is required.
export function ssoMockSkipReason({ reachable, required, issuer }: { reachable: boolean; required: boolean; issuer: string }): string | null {
  if (reachable) return null
  const reason = `Mock-OIDC nicht erreichbar (${issuer}), siehe README "Lokale SSO-Tests mit Mock-OIDC"`
  if (required) {
    throw new Error(`${reason}; E2E_REQUIRE_SSO_MOCK=1 verlangt ihn, Überspringen ist hier ein Fehler`)
  }
  return reason
}
