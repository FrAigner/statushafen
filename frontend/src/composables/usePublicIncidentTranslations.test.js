import { describe, it, expect, vi } from 'vitest'
import { usePublicIncidentTranslations } from './usePublicIncidentTranslations'

describe('usePublicIncidentTranslations', () => {
  it('loads translations from public API', async () => {
    const translations = [
      { ID: 1, IncidentID: 1, LanguageCode: 'de', Title: 'Vorfall', Message: 'Vorfall-Nachricht' },
      { ID: 2, IncidentID: 1, LanguageCode: 'en', Title: 'Incident', Message: 'Incident message' }
    ]
    const apiFetch = vi.fn(async () => translations)

    const { loadTranslations, translations: trans } = usePublicIncidentTranslations()
    await loadTranslations(apiFetch, 1)

    expect(apiFetch).toHaveBeenCalledWith('/api/incidents/1/translations')
    expect(trans.value).toEqual(translations)
  })

  it('handles API errors gracefully', async () => {
    const apiFetch = vi.fn(async () => {
      throw new Error('API error')
    })

    const { loadTranslations, error, translations } = usePublicIncidentTranslations()
    await loadTranslations(apiFetch, 1)

    expect(error.value).toBe('API error')
    expect(translations.value).toEqual([])
  })

  it('provides translation lookup by language code', async () => {
    const trans = [
      { ID: 1, IncidentID: 1, LanguageCode: 'de', Title: 'Deutsch' },
      { ID: 2, IncidentID: 1, LanguageCode: 'en', Title: 'English' }
    ]
    const apiFetch = vi.fn(async () => trans)

    const { loadTranslations, getTranslation } = usePublicIncidentTranslations()
    await loadTranslations(apiFetch, 1)

    expect(getTranslation('de')).toEqual(trans[0])
    expect(getTranslation('en')).toEqual(trans[1])
    expect(getTranslation('fr')).toBeUndefined()
  })

  it('provides translations indexed by language code', async () => {
    const trans = [
      { ID: 1, IncidentID: 1, LanguageCode: 'de', Title: 'Deutsch' },
      { ID: 2, IncidentID: 1, LanguageCode: 'en', Title: 'English' }
    ]
    const apiFetch = vi.fn(async () => trans)

    const { loadTranslations, translationsByLanguage } = usePublicIncidentTranslations()
    await loadTranslations(apiFetch, 1)

    expect(translationsByLanguage.value.de).toEqual(trans[0])
    expect(translationsByLanguage.value.en).toEqual(trans[1])
    expect(translationsByLanguage.value.fr).toBeUndefined()
  })

  it('tracks loading state', async () => {
    const apiFetch = vi.fn(async () => [])
    const { loadTranslations, loading } = usePublicIncidentTranslations()

    expect(loading.value).toBe(false)
    const promise = loadTranslations(apiFetch, 1)
    expect(loading.value).toBe(true)
    await promise
    expect(loading.value).toBe(false)
  })
})
