import { describe, it, expect, afterEach, vi } from 'vitest'
import { useTranslationEnabled } from './useTranslationEnabled'
import { fetchMock, mockedFetch } from '../test-support/fetchStub'

const json = (body: unknown, status = 200) => ({ ok: status < 400, status, json: async () => body })

describe('useTranslationEnabled', () => {
  afterEach(() => { vi.restoreAllMocks(); useTranslationEnabled().enabled.value = false })

  it('reads the public flag', async () => {
    global.fetch = fetchMock(async () => json({ enabled: true }))
    const { enabled, load } = useTranslationEnabled()
    expect(await load()).toBe(true)
    expect(enabled.value).toBe(true)
    expect(mockedFetch().mock.calls[0][0]).toBe('/api/public/translation/enabled')
  })

  it('treats anything but true and failed requests as off', async () => {
    const { enabled, load } = useTranslationEnabled()
    enabled.value = true
    global.fetch = fetchMock(async () => json({ enabled: 'yes' }))
    expect(await load()).toBe(false)
    enabled.value = true
    global.fetch = fetchMock(async () => json({ code: 'server_error' }, 500))
    vi.spyOn(console, 'error').mockImplementation(() => {})
    expect(await load()).toBe(false)
    expect(enabled.value).toBe(false)
  })
})
