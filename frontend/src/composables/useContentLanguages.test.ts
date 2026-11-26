import { describe, it, expect, beforeEach, vi } from 'vitest'
import { useAuth } from './useAuth'
import { useContentLanguages } from './useContentLanguages'
import { fetchMock as createFetchMock } from '../test-support/fetchStub'

function jsonResponse(body: unknown, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

// Stage 1 of 5 of multilingualism: composable tests for the global language
// catalog + the languages activated per status page. Like the other
// composable tests, global.fetch is stubbed, since useApi builds directly on
// it rather than on axios or the like.
describe('useContentLanguages', () => {
  beforeEach(() => {
    useAuth().logout()
    vi.restoreAllMocks()
  })

  it('lädt den globalen Sprachkatalog', async () => {
    global.fetch = createFetchMock(async () => jsonResponse([{ ID: 1, Code: 'de', Name: 'Deutsch', IsActive: true }]))
    const { catalog, loadCatalog } = useContentLanguages()

    await loadCatalog()

    expect(global.fetch).toHaveBeenCalledWith(expect.stringContaining('/api/languages'), expect.anything())
    expect(catalog.value).toEqual([{ ID: 1, Code: 'de', Name: 'Deutsch', IsActive: true }])
  })

  it('lädt die aktivierten Sprachen einer Statusseite', async () => {
    global.fetch = createFetchMock(async () => jsonResponse([{ ID: 1, LanguageCode: 'de', IsDefault: true }]))
    const { pageLanguages, loadPageLanguages } = useContentLanguages()

    await loadPageLanguages(7)

    expect(global.fetch).toHaveBeenCalledWith(expect.stringContaining('/api/pages/7/languages'), expect.anything())
    expect(pageLanguages.value).toEqual([{ ID: 1, LanguageCode: 'de', IsDefault: true }])
  })

  it('leert pageLanguages ohne echten Seiten-Aufruf, wenn keine pageId übergeben wird', async () => {
    global.fetch = vi.fn()
    const { pageLanguages, loadPageLanguages } = useContentLanguages()
    pageLanguages.value = [{ ID: 1, LanguageCode: 'de', IsDefault: true }]

    await loadPageLanguages(null)

    expect(pageLanguages.value).toEqual([])
    expect(global.fetch).not.toHaveBeenCalled()
  })

  it('speichert die Sprachauswahl einer Statusseite per PUT', async () => {
    global.fetch = createFetchMock(async () => jsonResponse([{ ID: 2, LanguageCode: 'en', IsDefault: false }]))
    const { savePageLanguages } = useContentLanguages()

    const result = await savePageLanguages(7, ['de', 'en'], 'de')

    expect(global.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/pages/7/languages'),
      expect.objectContaining({ method: 'PUT', body: JSON.stringify({ codes: ['de', 'en'], default: 'de' }) })
    )
    expect(result).toEqual([{ ID: 2, LanguageCode: 'en', IsDefault: false }])
  })

  it('legt eine neue Sprache an und lädt den Katalog danach neu', async () => {
    const fetchMock = createFetchMock(async (url, options = {}) => {
      const method = options.method || 'GET'
      if (method === 'POST') return jsonResponse({ ID: 3, Code: 'fr', Name: 'Français', IsActive: true })
      return jsonResponse([{ ID: 3, Code: 'fr', Name: 'Français', IsActive: true }])
    })
    global.fetch = fetchMock
    const { catalog, createLanguage } = useContentLanguages()

    const created = await createLanguage('fr', 'Français')

    expect(created).toEqual({ ID: 3, Code: 'fr', Name: 'Français', IsActive: true })
    expect(catalog.value).toEqual([{ ID: 3, Code: 'fr', Name: 'Français', IsActive: true }])
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('aktualisiert eine Sprache und lädt den Katalog danach neu', async () => {
    const fetchMock = createFetchMock(async (url, options = {}) => {
      const method = options.method || 'GET'
      if (method === 'PUT') return jsonResponse({ ID: 3, Code: 'fr', Name: 'Francais (updated)', IsActive: false })
      return jsonResponse([{ ID: 3, Code: 'fr', Name: 'Francais (updated)', IsActive: false }])
    })
    global.fetch = fetchMock
    const { catalog, updateLanguage } = useContentLanguages()

    const updated = await updateLanguage(3, { name: 'Francais (updated)', is_active: false })

    expect(updated.IsActive).toBe(false)
    expect(catalog.value[0].Name).toBe('Francais (updated)')
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/languages/3'),
      expect.objectContaining({ method: 'PUT' })
    )
  })
})
