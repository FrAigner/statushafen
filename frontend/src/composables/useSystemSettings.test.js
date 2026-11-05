import { describe, it, expect, beforeEach, vi } from 'vitest'
import { useAuth } from './useAuth'
import { useSystemSettings } from './useSystemSettings'

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

// Composable-Tests für die globalen Systemeinstellungen. Wie
// die übrigen Composable-Tests wird global.fetch gestubbt (useApi baut direkt
// darauf auf).
describe('useSystemSettings', () => {
  beforeEach(() => {
    useAuth().logout()
    vi.restoreAllMocks()
  })

  // Muss vor dem ersten Laden stehen (Modulzustand): Betreiber-Entscheidung.
  it('startet mit ausgeschalteter Selbstüberwachung', () => {
    expect(useSystemSettings().settings.value.selfmonitor_enabled).toBe(false)
  })

  it('lädt die Systemeinstellungen', async () => {
    const body = { orders_enabled: false, email_enabled: true }
    global.fetch = vi.fn(async () => jsonResponse(body))
    const { settings, loadSettings } = useSystemSettings()

    const result = await loadSettings()

    expect(global.fetch).toHaveBeenCalledWith(expect.stringContaining('/api/settings'), expect.anything())
    expect(result).toEqual(body)
    expect(settings.value).toEqual(body)
  })

  it('schickt per PUT nur die übergebenen Felder und übernimmt den vollen Stand', async () => {
    const body = { orders_enabled: true, email_enabled: false }
    global.fetch = vi.fn(async () => jsonResponse(body))
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
    global.fetch = vi.fn(async () => jsonResponse({ orders_enabled: true, email_enabled: true }))
    const { settings, loadSettings, saveSettings } = useSystemSettings()
    await loadSettings()

    global.fetch = vi.fn(async () => jsonResponse({ error: 'kaputt' }, 500))
    await expect(saveSettings({ email_enabled: false })).rejects.toThrow()
    expect(settings.value).toEqual({ orders_enabled: true, email_enabled: true })
  })
})
