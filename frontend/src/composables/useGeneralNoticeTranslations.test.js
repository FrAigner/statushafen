import { describe, it, expect, beforeEach, vi } from 'vitest'
import { useGeneralNoticeTranslations } from './useGeneralNoticeTranslations'

describe('useGeneralNoticeTranslations', () => {
  let apiFetchMock

  beforeEach(() => {
    apiFetchMock = vi.fn()
  })

  it('loads drafts and confirmed translations from the admin list for a notice', async () => {
    const translations = [
      { ID: 1, GeneralNoticeID: 1, LanguageCode: 'de', Title: 'Wartung', Message: 'Geplante Wartung', Status: 'confirmed' },
      { ID: 2, GeneralNoticeID: 1, LanguageCode: 'en', Title: 'Maintenance', Message: 'Scheduled maintenance', Status: 'confirmed' },
    ]
    apiFetchMock.mockResolvedValueOnce(translations)

    const { translations: result, loadTranslations } = useGeneralNoticeTranslations()
    await loadTranslations(apiFetchMock, 1)

    expect(apiFetchMock).toHaveBeenCalledWith('/api/notices/1/translations/all')
    expect(result.value).toEqual(translations)
  })

  it('creates a new draft translation', async () => {
    const newTranslation = {
      ID: 3,
      GeneralNoticeID: 1,
      LanguageCode: 'fr',
      Title: 'Maintenance',
      Message: 'Maintenance planifiée',
      Status: 'draft',
      Source: 'manual'
    }
    apiFetchMock.mockResolvedValueOnce(newTranslation)

    const { createTranslation } = useGeneralNoticeTranslations()
    const result = await createTranslation(apiFetchMock, 1, {
      language_code: 'fr',
      title: 'Maintenance',
      message: 'Maintenance planifiée',
      source: 'manual'
    })

    expect(apiFetchMock).toHaveBeenCalledWith(
      '/api/notices/1/translations',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({
          language_code: 'fr',
          title: 'Maintenance',
          message: 'Maintenance planifiée',
          source: 'manual'
        })
      })
    )
    expect(result).toEqual(newTranslation)
  })

  it('confirms a draft translation', async () => {
    const confirmed = {
      ID: 3,
      GeneralNoticeID: 1,
      LanguageCode: 'fr',
      Title: 'Maintenance',
      Message: 'Maintenance planifiée',
      Status: 'confirmed',
      Source: 'manual'
    }
    apiFetchMock.mockResolvedValueOnce(confirmed)

    const { confirmTranslation } = useGeneralNoticeTranslations()
    const result = await confirmTranslation(apiFetchMock, 3)

    expect(apiFetchMock).toHaveBeenCalledWith(
      '/api/notice-translations/3/confirm',
      expect.objectContaining({ method: 'PATCH' })
    )
    expect(result).toEqual(confirmed)
  })

  it('deletes a translation', async () => {
    apiFetchMock.mockResolvedValueOnce(null)

    const { deleteTranslation } = useGeneralNoticeTranslations()
    await deleteTranslation(apiFetchMock, 3)

    expect(apiFetchMock).toHaveBeenCalledWith(
      '/api/notice-translations/3',
      expect.objectContaining({ method: 'DELETE' })
    )
  })

  it('throws error on API failure', async () => {
    apiFetchMock.mockRejectedValueOnce(new Error('API Error'))

    const { loadTranslations } = useGeneralNoticeTranslations()
    await expect(loadTranslations(apiFetchMock, 999)).rejects.toThrow()
  })
})
