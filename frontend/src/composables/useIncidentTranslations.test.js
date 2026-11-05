import { describe, it, expect, beforeEach, vi } from 'vitest'
import { useIncidentTranslations } from './useIncidentTranslations'

describe('useIncidentTranslations', () => {
  let apiFetchMock

  beforeEach(() => {
    apiFetchMock = vi.fn()
  })

  it('loads drafts and confirmed translations from the admin list for an incident', async () => {
    const translations = [
      { ID: 1, IncidentID: 1, LanguageCode: 'de', Title: 'Ausfall', Message: 'Datenbankausfall', Status: 'confirmed' },
      { ID: 2, IncidentID: 1, LanguageCode: 'en', Title: 'Outage', Message: 'Database outage', Status: 'confirmed' },
    ]
    apiFetchMock.mockResolvedValueOnce(translations)

    const { translations: result, loadTranslations } = useIncidentTranslations()
    await loadTranslations(apiFetchMock, 1)

    expect(apiFetchMock).toHaveBeenCalledWith('/api/incidents/1/translations/all')
    expect(result.value).toEqual(translations)
  })

  it('creates a new draft translation', async () => {
    const newTranslation = {
      ID: 3,
      IncidentID: 1,
      LanguageCode: 'fr',
      Title: 'Panne',
      Message: 'Panne de base de données',
      Status: 'draft',
      Source: 'manual'
    }
    apiFetchMock.mockResolvedValueOnce(newTranslation)

    const { createTranslation } = useIncidentTranslations()
    const result = await createTranslation(apiFetchMock, 1, {
      language_code: 'fr',
      title: 'Panne',
      message: 'Panne de base de données',
      source: 'manual'
    })

    expect(apiFetchMock).toHaveBeenCalledWith(
      '/api/incidents/1/translations',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({
          language_code: 'fr',
          title: 'Panne',
          message: 'Panne de base de données',
          source: 'manual'
        })
      })
    )
    expect(result).toEqual(newTranslation)
  })

  it('confirms a draft translation', async () => {
    const confirmed = {
      ID: 3,
      IncidentID: 1,
      LanguageCode: 'fr',
      Title: 'Panne',
      Message: 'Panne de base de données',
      Status: 'confirmed',
      Source: 'manual'
    }
    apiFetchMock.mockResolvedValueOnce(confirmed)

    const { confirmTranslation } = useIncidentTranslations()
    const result = await confirmTranslation(apiFetchMock, 3)

    expect(apiFetchMock).toHaveBeenCalledWith(
      '/api/incident-translations/3/confirm',
      expect.objectContaining({ method: 'PATCH' })
    )
    expect(result).toEqual(confirmed)
  })

  it('deletes a translation', async () => {
    apiFetchMock.mockResolvedValueOnce(null)

    const { deleteTranslation } = useIncidentTranslations()
    await deleteTranslation(apiFetchMock, 3)

    expect(apiFetchMock).toHaveBeenCalledWith(
      '/api/incident-translations/3',
      expect.objectContaining({ method: 'DELETE' })
    )
  })

  it('throws error on API failure', async () => {
    apiFetchMock.mockRejectedValueOnce(new Error('API Error'))

    const { loadTranslations } = useIncidentTranslations()
    await expect(loadTranslations(apiFetchMock, 999)).rejects.toThrow()
  })
})
