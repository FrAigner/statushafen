import { describe, it, expect, beforeEach, vi } from 'vitest'
import { useComponentTranslations } from './useComponentTranslations'

describe('useComponentTranslations', () => {
  let apiFetchMock

  beforeEach(() => {
    apiFetchMock = vi.fn()
  })

  it('loads drafts and confirmed translations from the admin list for a component', async () => {
    const translations = [
      { ID: 1, ComponentID: 1, LanguageCode: 'de', Title: 'Datenbank', Description: 'Hauptdatenbank', Status: 'confirmed' },
      { ID: 2, ComponentID: 1, LanguageCode: 'en', Title: 'Database', Description: 'Primary database', Status: 'confirmed' },
    ]
    apiFetchMock.mockResolvedValueOnce(translations)

    const { translations: result, loadTranslations } = useComponentTranslations()
    await loadTranslations(apiFetchMock, 1)

    expect(apiFetchMock).toHaveBeenCalledWith('/api/components/1/translations/all')
    expect(result.value).toEqual(translations)
  })

  it('creates a new draft translation', async () => {
    const newTranslation = {
      ID: 3,
      ComponentID: 1,
      LanguageCode: 'fr',
      Title: 'Base de données',
      Description: 'Base de données principale',
      Status: 'draft',
      Source: 'manual'
    }
    apiFetchMock.mockResolvedValueOnce(newTranslation)

    const { createTranslation } = useComponentTranslations()
    const result = await createTranslation(apiFetchMock, 1, {
      language_code: 'fr',
      title: 'Base de données',
      description: 'Base de données principale',
      source: 'manual'
    })

    expect(apiFetchMock).toHaveBeenCalledWith(
      '/api/components/1/translations',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({
          language_code: 'fr',
          title: 'Base de données',
          description: 'Base de données principale',
          source: 'manual'
        })
      })
    )
    expect(result).toEqual(newTranslation)
  })

  it('confirms a draft translation', async () => {
    const confirmed = {
      ID: 3,
      ComponentID: 1,
      LanguageCode: 'fr',
      Title: 'Base de données',
      Description: 'Base de données principale',
      Status: 'confirmed',
      Source: 'manual'
    }
    apiFetchMock.mockResolvedValueOnce(confirmed)

    const { confirmTranslation } = useComponentTranslations()
    const result = await confirmTranslation(apiFetchMock, 3)

    expect(apiFetchMock).toHaveBeenCalledWith(
      '/api/component-translations/3/confirm',
      expect.objectContaining({ method: 'PATCH' })
    )
    expect(result).toEqual(confirmed)
  })

  it('deletes a translation', async () => {
    apiFetchMock.mockResolvedValueOnce(null)

    const { deleteTranslation } = useComponentTranslations()
    await deleteTranslation(apiFetchMock, 3)

    expect(apiFetchMock).toHaveBeenCalledWith(
      '/api/component-translations/3',
      expect.objectContaining({ method: 'DELETE' })
    )
  })

  it('throws error on API failure', async () => {
    apiFetchMock.mockRejectedValueOnce(new Error('API Error'))

    const { loadTranslations } = useComponentTranslations()
    await expect(loadTranslations(apiFetchMock, 999)).rejects.toThrow()
  })
})
