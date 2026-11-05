import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import i18n from './index'
import { flatten, unflatten, applyGlobalOverrides, applyPageOverrides, clearPageOverrides } from './overrides'

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

describe('flatten/unflatten', () => {
  it('flatten wandelt verschachtelte Objekte in "dotted" Key-Value-Paare um', () => {
    const nested = { nav: { items: { sso: 'SSO Konfiguration' } }, common: { save: 'Speichern' } }
    expect(flatten(nested)).toEqual({ 'nav.items.sso': 'SSO Konfiguration', 'common.save': 'Speichern' })
  })

  it('unflatten ist die Umkehrung von flatten (Rundtrip)', () => {
    const nested = { nav: { items: { sso: 'SSO Konfiguration', users: 'Benutzer' } }, common: { save: 'Speichern' } }
    expect(unflatten(flatten(nested))).toEqual(nested)
  })

  it('unflatten verschmutzt Object.prototype nicht', () => {
    const result = unflatten({ '__proto__.polluted': 'yes', 'a.constructor.prototype.x': 'yes', 'nav.sso': 'SSO' })
    expect({}.polluted).toBeUndefined()
    expect({}.x).toBeUndefined()
    expect(Object.getPrototypeOf(result)).toBe(Object.prototype)
    expect(result).toEqual({ nav: { sso: 'SSO' } })
  })

  it('unflatten verwirft Schlüssel mit constructor-/prototype-Segment komplett', () => {
    expect(unflatten({ constructor: 'x', prototype: 'y', 'a.prototype': 'z', 'ok.key': 'v' })).toEqual({ ok: { key: 'v' } })
  })

  it('unflatten verwirft einen Präfix-Schlüssel, statt den Teilbaum zu überschreiben (Review-Befund 1)', () => {
    expect(unflatten({ 'nav.items.sso': 'SSO', 'nav.items.users': 'U', nav: 'x' })).toEqual({
      nav: { items: { sso: 'SSO', users: 'U' } },
    })
    expect(unflatten({ 'nav.items.sso': 'SSO', 'nav.items': 'x' })).toEqual({ nav: { items: { sso: 'SSO' } } })
  })

  it('unflatten ist unabhängig von der Reihenfolge der Einträge', () => {
    expect(unflatten({ 'nav.items': 'x', 'nav.items.sso': 'SSO' })).toEqual({ nav: { items: { sso: 'SSO' } } })
    expect(unflatten({ nav: 'x', 'nav.items.sso': 'SSO' })).toEqual({ nav: { items: { sso: 'SSO' } } })
  })

  it('unflatten behält Schlüssel mit gemeinsamem Textpräfix, der kein Pfadpräfix ist', () => {
    expect(unflatten({ nav: 'x', navigation: 'y', 'nav2.a': 'z' })).toEqual({ nav: 'x', navigation: 'y', nav2: { a: 'z' } })
  })
})

describe('applyGlobalOverrides / applyPageOverrides / clearPageOverrides', () => {
  const originalNavSso = i18n.global.getLocaleMessage('de').adminNav.items.sso

  beforeEach(() => {
    i18n.global.locale.value = 'de'
  })

  afterEach(async () => {
    // Layer zurücksetzen, damit Tests einander nicht beeinflussen.
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({}))
    await applyGlobalOverrides('de')
    clearPageOverrides('de')
    vi.restoreAllMocks()
  })

  it('wendet einen globalen Override auf die i18n-Nachrichten an, ohne andere Keys zu verändern', async () => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ 'adminNav.items.sso': 'Mein SSO' }))

    await applyGlobalOverrides('de')

    expect(i18n.global.t('adminNav.items.sso')).toBe('Mein SSO')
    // Unberührter Key bleibt beim Bundle-Default.
    expect(i18n.global.t('common.save')).not.toBe('')
  })

  it('fällt bei Netzwerkfehler auf den Bundle-Default zurück, statt zu werfen', async () => {
    global.fetch = vi.fn().mockRejectedValue(new Error('Netzwerkfehler'))

    await expect(applyGlobalOverrides('de')).resolves.not.toThrow()
    expect(i18n.global.t('adminNav.items.sso')).toBe(originalNavSso)
  })

  it('Seiten-Override gewinnt gegen einen globalen Override für denselben Key (Präzedenz Bundle < global < Seite)', async () => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ 'adminNav.items.sso': 'Globaler Text' }))
    await applyGlobalOverrides('de')
    expect(i18n.global.t('adminNav.items.sso')).toBe('Globaler Text')

    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ 'adminNav.items.sso': 'Seiten-Text' }))
    await applyPageOverrides(1, 'de')

    expect(i18n.global.t('adminNav.items.sso')).toBe('Seiten-Text')
  })

  it('clearPageOverrides entfernt nur die Seiten-Ebene, globaler Override bleibt bestehen', async () => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ 'adminNav.items.sso': 'Globaler Text' }))
    await applyGlobalOverrides('de')

    global.fetch = vi.fn().mockResolvedValue(jsonResponse({ 'adminNav.items.sso': 'Seiten-Text' }))
    await applyPageOverrides(1, 'de')
    expect(i18n.global.t('adminNav.items.sso')).toBe('Seiten-Text')

    clearPageOverrides('de')

    expect(i18n.global.t('adminNav.items.sso')).toBe('Globaler Text')
  })
})
