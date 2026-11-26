import { describe, it, expect, vi, type Mock } from 'vitest'
import type { ApiFetch } from './useApi'
import { usePublicComponentTranslations } from './usePublicComponentTranslations'

// A vi.fn with a fixed answer, callable as the generic ApiFetch.
const fakeApiFetch = (impl: () => Promise<unknown>) => vi.fn(impl) as Mock<() => Promise<unknown>> & ApiFetch

describe('usePublicComponentTranslations', () => {
  it('loads translations from public API', async () => {
    const translations = [
      { ID: 1, ComponentID: 1, LanguageCode: 'de', Title: 'Datenbank', Description: 'Hauptdatenbank' },
      { ID: 2, ComponentID: 1, LanguageCode: 'en', Title: 'Database', Description: 'Main database' }
    ]
    const apiFetch = fakeApiFetch(async () => translations)

    const { loadTranslations, translations: trans } = usePublicComponentTranslations()
    await loadTranslations(apiFetch, 1)

    expect(apiFetch).toHaveBeenCalledWith('/api/components/1/translations')
    expect(trans.value).toEqual(translations)
  })

  it('handles API errors gracefully', async () => {
    const apiFetch = fakeApiFetch(async () => {
      throw new Error('API error')
    })

    const { loadTranslations, error, translations } = usePublicComponentTranslations()
    await loadTranslations(apiFetch, 1)

    expect(error.value).toBe('API error')
    expect(translations.value).toEqual([])
  })

  it('provides translation lookup by language code', async () => {
    const trans = [
      { ID: 1, ComponentID: 1, LanguageCode: 'de', Title: 'Deutsch', Description: 'Datenbank' },
      { ID: 2, ComponentID: 1, LanguageCode: 'en', Title: 'English', Description: 'Database' }
    ]
    const apiFetch = fakeApiFetch(async () => trans)

    const { loadTranslations, getTranslation } = usePublicComponentTranslations()
    await loadTranslations(apiFetch, 1)

    expect(getTranslation('de')).toEqual(trans[0])
    expect(getTranslation('en')).toEqual(trans[1])
    expect(getTranslation('fr')).toBeUndefined()
  })

  it('provides translations indexed by language code', async () => {
    const trans = [
      { ID: 1, ComponentID: 1, LanguageCode: 'de', Title: 'Deutsch', Description: 'Datenbank' },
      { ID: 2, ComponentID: 1, LanguageCode: 'en', Title: 'English', Description: 'Database' }
    ]
    const apiFetch = fakeApiFetch(async () => trans)

    const { loadTranslations, translationsByLanguage } = usePublicComponentTranslations()
    await loadTranslations(apiFetch, 1)

    expect(translationsByLanguage.value.de).toEqual(trans[0])
    expect(translationsByLanguage.value.en).toEqual(trans[1])
    expect(translationsByLanguage.value.fr).toBeUndefined()
  })

  it('tracks loading state', async () => {
    const apiFetch = fakeApiFetch(async () => [])
    const { loadTranslations, loading } = usePublicComponentTranslations()

    expect(loading.value).toBe(false)
    const promise = loadTranslations(apiFetch, 1)
    expect(loading.value).toBe(true)
    await promise
    expect(loading.value).toBe(false)
  })
})
