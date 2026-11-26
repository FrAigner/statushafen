import { describe, it, expect, vi, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { ref } from 'vue'
import { useComponentLocalization } from './useComponentLocalization'

const apiFetch = vi.fn()
vi.mock('./useApi', () => ({ useApi: () => ({ apiFetch }) }))
vi.mock('./useLanguage', () => ({ useLanguage: () => ({ currentLanguageCode: ref('en') }) }))

async function mountLocalization() {
  let result
  mount({
    setup() {
      result = useComponentLocalization({ ID: 4 }, (id) => `/api/components/${id}/translations`)
      return () => null
    },
  })
  await flushPromises()
  return result
}

describe('useComponentLocalization', () => {
  afterEach(() => { vi.restoreAllMocks(); apiFetch.mockReset() })

  it('loads the translations and picks the one of the current language', async () => {
    apiFetch.mockResolvedValueOnce([{ LanguageCode: 'de', Name: 'DB' }, { LanguageCode: 'en', Name: 'Database' }])
    const loc = await mountLocalization()

    expect(apiFetch).toHaveBeenCalledWith('/api/components/4/translations')
    expect(loc.currentTranslation.value).toEqual({ LanguageCode: 'en', Name: 'Database' })
    expect(loc.loading.value).toBe(false)
  })

  it('logs a failed load and keeps an empty list', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => {})
    apiFetch.mockRejectedValueOnce(new Error('offline'))
    const loc = await mountLocalization()

    expect(error).toHaveBeenCalledWith('Failed to load translations:', expect.any(Error))
    expect(loc.translations.value).toEqual([])
    expect(loc.currentTranslation.value).toBeUndefined()
    expect(loc.loading.value).toBe(false)
  })
})
