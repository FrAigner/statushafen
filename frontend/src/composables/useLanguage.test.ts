import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import i18n from '../i18n'
import { useAuth } from './useAuth'
import { useLanguage } from './useLanguage'

function jsonResponse(body: unknown, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

// useI18n() (used internally by useLanguage) requires an active component
// instance ("Must be called at the top of a setup function"); a host stub
// makes setLanguage callable in the test.
function mountUseLanguage() {
  let result!: ReturnType<typeof useLanguage>
  const wrapper = mount({
    setup() {
      result = useLanguage()
      return () => null
    },
  }, { global: { plugins: [i18n] } })
  return { wrapper, ...result }
}

describe('useLanguage', () => {
  beforeEach(() => {
    i18n.global.locale.value = 'de'
    localStorage.clear()
    useAuth().logout()
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('setzt die Locale, persistiert sie und lädt die globalen UI-Overrides der neuen Sprache neu', async () => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({}))
    const { setLanguage } = mountUseLanguage()

    await setLanguage('en')

    expect(i18n.global.locale.value).toBe('en')
    expect(localStorage.getItem('preferred_language')).toBe('en')
    expect(global.fetch).toHaveBeenCalledWith(expect.stringContaining('/api/public/ui-overrides?language_code=en'))
  })

  it('ignoriert nicht unterstützte Sprachen', async () => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({}))
    const { setLanguage } = mountUseLanguage()

    await setLanguage('fr')

    expect(i18n.global.locale.value).toBe('de')
    expect(global.fetch).not.toHaveBeenCalled()
  })

  it('persistiert die Sprache zusätzlich serverseitig, wenn eingeloggt', async () => {
    useAuth().setAuth('jwt-1', { email: 'a@x.de' })
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({}))
    const { setLanguage } = mountUseLanguage()

    await setLanguage('en')

    expect(global.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/me'),
      expect.objectContaining({ method: 'PATCH' })
    )
  })

  it('Sprachumschaltung funktioniert auch, wenn die serverseitige Persistenz fehlschlägt', async () => {
    useAuth().setAuth('jwt-1', { email: 'a@x.de' })
    global.fetch = vi.fn()
      .mockResolvedValueOnce(jsonResponse({})) // applyGlobalOverrides
      .mockRejectedValueOnce(new Error('Netzwerkfehler')) // PATCH /api/me
    const { setLanguage } = mountUseLanguage()

    await expect(setLanguage('en')).resolves.not.toThrow()
    expect(i18n.global.locale.value).toBe('en')
  })

  // An error while saving must be reported visibly to the user.
  it('meldet einen fehlgeschlagenen PATCH mit verständlicher Meldung und behält die lokale Umschaltung', async () => {
    useAuth().setAuth('jwt-1', { email: 'a@x.de' })
    global.fetch = vi.fn()
      .mockResolvedValueOnce(jsonResponse({})) // applyGlobalOverrides
      .mockResolvedValueOnce(jsonResponse({ error: 'Could not save preferred language' }, 500)) // PATCH
    const { setLanguage, saveError } = mountUseLanguage()

    await setLanguage('en')

    expect(saveError.value).toBe(i18n.global.t('language.saveError'))
    expect(saveError.value).not.toBe('language.saveError')
    expect(i18n.global.locale.value).toBe('en')
    expect(localStorage.getItem('preferred_language')).toBe('en')
  })

  it('zeigt bei Erfolg keine Meldung und löscht eine frühere', async () => {
    useAuth().setAuth('jwt-1', { email: 'a@x.de' })
    global.fetch = vi.fn()
      .mockResolvedValueOnce(jsonResponse({}))
      .mockRejectedValueOnce(new Error('Netzwerkfehler'))
      .mockResolvedValueOnce(jsonResponse({}))
      .mockResolvedValueOnce(jsonResponse({ preferred_language: 'de' }))
    const { setLanguage, saveError } = mountUseLanguage()

    await setLanguage('en')
    expect(saveError.value).not.toBe('')
    await setLanguage('de')
    expect(saveError.value).toBe('')
  })

  it('anonyme Besucher lösen keinen Backend-Call und keine Fehlermeldung aus', async () => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse({}))
    const { setLanguage, saveError } = mountUseLanguage()

    await setLanguage('en')

    expect(saveError.value).toBe('')
    expect(global.fetch).not.toHaveBeenCalledWith(expect.stringContaining('/api/me'), expect.anything())
  })
})
