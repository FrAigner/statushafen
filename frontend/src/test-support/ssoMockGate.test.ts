import { describe, it, expect } from 'vitest'
import { ssoMockRequired, ssoMockSkipReason } from './ssoMockGate'

// The nightly job e2e-sso-nachts.yml sets E2E_REQUIRE_SSO_MOCK=1, so that
// frontend/e2e/sso-oidc.spec.ts is never silently skipped there.
describe('ssoMockRequired', () => {
  it('ist nur bei E2E_REQUIRE_SSO_MOCK=1 gesetzt', () => {
    expect(ssoMockRequired({ E2E_REQUIRE_SSO_MOCK: '1' })).toBe(true)
    expect(ssoMockRequired({ E2E_REQUIRE_SSO_MOCK: '0' })).toBe(false)
    expect(ssoMockRequired({ E2E_REQUIRE_SSO_MOCK: '' })).toBe(false)
    expect(ssoMockRequired({})).toBe(false)
  })
})

describe('ssoMockSkipReason', () => {
  const issuer = 'http://localhost:8090/statushafen'

  it('liefert null, wenn der Mock erreichbar ist (mit und ohne Pflicht)', () => {
    expect(ssoMockSkipReason({ reachable: true, required: false, issuer })).toBeNull()
    expect(ssoMockSkipReason({ reachable: true, required: true, issuer })).toBeNull()
  })

  it('liefert ohne Pflicht einen Grund zum Überspringen', () => {
    expect(ssoMockSkipReason({ reachable: false, required: false, issuer })).toBe(
      'Mock-OIDC nicht erreichbar (http://localhost:8090/statushafen), siehe README "Lokale SSO-Tests mit Mock-OIDC"',
    )
  })

  it('wirft mit Pflicht, statt zu überspringen', () => {
    expect(() => ssoMockSkipReason({ reachable: false, required: true, issuer })).toThrow(
      'Mock-OIDC nicht erreichbar (http://localhost:8090/statushafen), siehe README "Lokale SSO-Tests mit Mock-OIDC"; E2E_REQUIRE_SSO_MOCK=1 verlangt ihn, Überspringen ist hier ein Fehler',
    )
  })
})
