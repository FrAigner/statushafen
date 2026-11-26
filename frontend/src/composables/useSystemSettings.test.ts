import { describe, it, expect, beforeEach, vi } from 'vitest'
import { useAuth } from './useAuth'
import { useSystemSettings } from './useSystemSettings'
import { fetchMock } from '../test-support/fetchStub'

function jsonResponse(body: unknown, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

// Composable tests for the global system settings. Like the other
// composable tests, global.fetch is stubbed (useApi builds directly on it).
describe('useSystemSettings', () => {
  beforeEach(() => {
    useAuth().logout()
    vi.restoreAllMocks()
  })

  // Must come before the first load (module state): operator decision.
  it('startet mit ausgeschalteter Selbstüberwachung', () => {
    expect(useSystemSettings().settings.value.selfmonitor_enabled).toBe(false)
  })

  it('lädt die Systemeinstellungen', async () => {
    const body = { orders_enabled: false, email_enabled: true }
    global.fetch = fetchMock(async () => jsonResponse(body))
    const { settings, loadSettings } = useSystemSettings()

    const result = await loadSettings()

    expect(global.fetch).toHaveBeenCalledWith(expect.stringContaining('/api/settings'), expect.anything())
    expect(result).toEqual(body)
    expect(settings.value).toEqual(body)
  })

  it('schickt per PUT nur die übergebenen Felder und übernimmt den vollen Stand', async () => {
    const body = { orders_enabled: true, email_enabled: false }
    global.fetch = fetchMock(async () => jsonResponse(body))
    const { settings, saveSettings } = useSystemSettings()

    const result = await saveSettings({ email_enabled: false })

    expect(global.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/settings'),
      expect.objectContaining({ method: 'PUT', body: JSON.stringify({ email_enabled: false }) })
    )
    expect(result).toEqual(body)
    expect(settings.value).toEqual(body)
  })

  it('reicht Fehler beim Speichern weiter und behält den Stand', async () => {
    global.fetch = fetchMock(async () => jsonResponse({ orders_enabled: true, email_enabled: true }))
    const { settings, loadSettings, saveSettings } = useSystemSettings()
    await loadSettings()

    global.fetch = fetchMock(async () => jsonResponse({ error: 'kaputt' }, 500))
    await expect(saveSettings({ email_enabled: false })).rejects.toThrow()
    expect(settings.value).toEqual({ orders_enabled: true, email_enabled: true })
  })
})
