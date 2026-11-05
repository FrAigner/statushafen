import { describe, it, expect } from 'vitest'
import { ssoMockRequired, ssoMockSkipReason } from './ssoMockGate.js'

// Der Nachtjob e2e-sso-nachts.yml setzt E2E_REQUIRE_SSO_MOCK=1, damit
// frontend/e2e/sso-oidc.spec.js dort nie still übersprungen wird.
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

// derselbe Schalter gilt für frontend/e2e/sso-saml.spec.js; der Grund
// nennt dann den Mock-SAML-IdP und seinen README-Abschnitt.
describe('ssoMockSkipReason für den SAML-Mock', () => {
  const mock = { name: 'Mock-SAML', readme: 'Lokale SSO-Tests mit Mock-SAML' }

  it('nennt den übergebenen Mock', () => {
    expect(ssoMockSkipReason({ reachable: false, required: false, issuer: 'http://localhost:8091/metadata', mock })).toBe(
      'Mock-SAML nicht erreichbar (http://localhost:8091/metadata), siehe README "Lokale SSO-Tests mit Mock-SAML"',
    )
    expect(ssoMockSkipReason({ reachable: true, required: true, issuer: 'x', mock })).toBeNull()
  })
})
