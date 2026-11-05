import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { useApi, isForbidden, isSessionExpired } from './useApi'
import { useAuth } from './useAuth'
import i18n from '../i18n'

function res(status, body = {}) {
  return { ok: status < 400, status, json: async () => body }
}

describe('useApi', () => {
  beforeEach(() => {
    i18n.global.locale.value = 'de'
    useAuth().logout()
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })
  afterEach(() => vi.restoreAllMocks())

  it('wirft bei 403 einen als Berechtigungsfehler erkennbaren Fehler mit bisheriger Meldung', async () => {
    global.fetch = vi.fn().mockResolvedValue(res(403))
    const err = await useApi().apiFetch('/api/x').catch(e => e)
    expect(err.message).toBe('Zugriff verweigert (Fehlende Berechtigung)')
    expect(err.status).toBe(403)
    expect(err.forbidden).toBe(true)
    expect(isForbidden(err)).toBe(true)
  })

  // Der Body der 403 (Fehlercode) darf nicht verworfen werden.
  it('hängt den JSON-Body einer 403 als data an und behält die feste Meldung', async () => {
    const body = { error: 'Bestätigung fehlgeschlagen', code: 'confirmation_invalid' }
    global.fetch = vi.fn().mockResolvedValue(res(403, body))
    const err = await useApi().apiFetch('/api/x').catch(e => e)
    expect(err.data).toEqual(body)
    expect(err.message).toBe('Zugriff verweigert (Fehlende Berechtigung)')
    expect(isForbidden(err)).toBe(true)
  })

  it('403 mit unlesbarem Body liefert leeres data statt eines Parserfehlers', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: false, status: 403, json: async () => { throw new SyntaxError('x') } })
    const err = await useApi().apiFetch('/api/x').catch(e => e)
    expect(err.data).toEqual({})
    expect(err.forbidden).toBe(true)
  })

  // erwartete Statuscodes sind kein Konsolenfehler, der Fehler wird trotzdem geworfen.
  it('loggt einen erwarteten Status nicht, andere Status aber schon', async () => {
    global.fetch = vi.fn().mockResolvedValue(res(404, { error: 'nix' }))
    const err = await useApi().apiFetch('/api/x', { expectedStatuses: [404] }).catch(e => e)
    expect(err.status).toBe(404)
    expect(console.error).not.toHaveBeenCalled()

    await useApi().apiFetch('/api/x', { expectedStatuses: [404] }).catch(() => {})
    global.fetch = vi.fn().mockResolvedValue(res(500, { error: 'boom' }))
    await useApi().apiFetch('/api/x', { expectedStatuses: [404] }).catch(() => {})
    expect(console.error).toHaveBeenCalledTimes(1)
  })

  it('reicht expectedStatuses nicht an fetch weiter', async () => {
    global.fetch = vi.fn().mockResolvedValue(res(200, {}))
    await useApi().apiFetch('/api/x', { expectedStatuses: [404], method: 'POST' })
    expect(global.fetch.mock.calls[0][1]).not.toHaveProperty('expectedStatuses')
  })

  // Fehlercodes des Backends werden übersetzt statt den englischen
  // oder technischen Servertext zu zeigen.
  it.each([
    ['de', 'invalid_request', 'Die Eingabe ist unvollständig oder ungültig. Bitte prüfe die Felder.'],
    ['en', 'invalid_request', 'The input is incomplete or invalid. Please check the fields.'],
    ['de', 'server_error', 'Auf dem Server ist ein Fehler aufgetreten. Bitte versuche es später erneut.'],
    ['de', 'tag_not_found', 'Der Tag wurde nicht gefunden.'],
  ])('übersetzt den Fehlercode (%s, %s)', async (locale, code, text) => {
    i18n.global.locale.value = locale
    global.fetch = vi.fn().mockResolvedValue(res(400, { error: "Key: 'X.Name' Error:Field validation for 'Name' failed on the 'required' tag", code }))
    const err = await useApi().apiFetch('/api/x').catch(e => e)
    expect(err.message).toBe(text)
    expect(err.data.code).toBe(code)
  })

  // Webhook-Ziel drosselt/lehnt ab/ist nicht erreichbar - verständliche
  // Meldung statt rohem Servertext, retry_after landet als {seconds} in der Meldung.
  it.each([
    ['de', 429, 'webhook_rate_limited', { retry_after: 12 }, 'Google drosselt gerade – bitte in 12 Sekunden erneut versuchen.'],
    ['en', 429, 'webhook_rate_limited', { retry_after: 3 }, 'Google is rate limiting right now – please try again in 3 seconds.'],
    ['de', 502, 'webhook_rejected', {}, 'Das Webhook-Ziel hat die Anfrage abgelehnt. Bitte prüfe die Webhook-URL und den Schlüssel.'],
    ['en', 502, 'webhook_unreachable', {}, 'The webhook target is currently unreachable. Please try again later.'],
  ])('übersetzt die Webhook-Fehlercodes (%s, %s, %s)', async (locale, status, code, extra, text) => {
    i18n.global.locale.value = locale
    global.fetch = vi.fn().mockResolvedValue(res(status, { error: 'Server antwortete mit Status: 429', code, ...extra }))
    const err = await useApi().apiFetch('/api/x').catch(e => e)
    expect(err.message).toBe(text)
  })

  // Löschen einer Sprache scheitert mit count/pages, beide landen in der Meldung.
  it.each([
    ['de', 'language_is_default', { count: 2, pages: ['Acme', 'Beta'] }, 'Die Sprache ist Standardsprache von 2 Statusseite(n) (Acme, Beta). Stelle dort zuerst eine andere Standardsprache ein.'],
    ['en', 'language_is_default', { count: 1, pages: ['Acme'] }, 'The language is the default language of 1 status page(s) (Acme). Set a different default language there first.'],
    ['de', 'language_in_use', { count: 3 }, 'Zu dieser Sprache gibt es noch 3 Mail-Vorlage(n). Lösche diese zuerst.'],
    ['en', 'language_in_use', { count: 3 }, '3 email template(s) still exist for this language. Delete them first.'],
  ])('setzt count und pages in die Sprach-Löschmeldung ein (%s, %s)', async (locale, code, extra, text) => {
    i18n.global.locale.value = locale
    global.fetch = vi.fn().mockResolvedValue(res(409, { error: 'x', code, ...extra }))
    const err = await useApi().apiFetch('/api/languages/1', { method: 'DELETE' }).catch(e => e)
    expect(err.message).toBe(text)
  })

  it('behält den Servertext bei unbekanntem oder fehlendem Code', async () => {
    global.fetch = vi.fn().mockResolvedValue(res(409, { error: 'Eigener Text', code: 'code_ohne_uebersetzung' }))
    expect((await useApi().apiFetch('/api/x').catch(e => e)).message).toBe('Eigener Text')
    global.fetch = vi.fn().mockResolvedValue(res(400, { error: 'Nur Text' }))
    expect((await useApi().apiFetch('/api/x').catch(e => e)).message).toBe('Nur Text')
    global.fetch = vi.fn().mockResolvedValue(res(502, {}))
    expect((await useApi().apiFetch('/api/x').catch(e => e)).message).toBe('HTTP Fehler 502')
  })

  it.each([404, 500])('markiert Status %i nicht als Berechtigungsfehler', async (status) => {
    global.fetch = vi.fn().mockResolvedValue(res(status, { error: 'boom' }))
    const err = await useApi().apiFetch('/api/x').catch(e => e)
    expect(err.message).toBe('boom')
    expect(err.status).toBe(status)
    expect(err.forbidden).toBeUndefined()
    expect(isForbidden(err)).toBe(false)
  })

  // Aufrufer brauchen den Fehler-Body, z. B. die Liste der
  // betroffenen Seiten aus der 409-Antwort von DELETE /api/me.
  it('hängt den JSON-Body des Fehlers als data an', async () => {
    const body = { error: 'konflikt', code: 'sole_admin_pages', pages: [{ id: 1 }] }
    global.fetch = vi.fn().mockResolvedValue(res(409, body))
    const err = await useApi().apiFetch('/api/x').catch(e => e)
    expect(err.status).toBe(409)
    expect(err.data).toEqual(body)
  })

  it('401 auf eine Anfrage mit Token meldet ab und ist kein Berechtigungsfehler', async () => {
    useAuth().setAuth('jwt-1', { email: 'a@x.de' })
    global.fetch = vi.fn().mockResolvedValue(res(401))
    const err = await useApi().apiFetch('/api/x').catch(e => e)
    expect(err.message).toBe('Sitzung abgelaufen. Bitte neu einloggen.')
    expect(err.status).toBe(401)
    expect(isSessionExpired(err)).toBe(true)
    expect(isForbidden(err)).toBe(false)
    expect(useAuth().token.value).toBeFalsy()
  })

  // Ein falsches Passwort (401 von /api/login, ohne Token) ist kein
  // Sitzungsende. Bisher wurde jede 401 zu „Sitzung abgelaufen“.
  it('401 ohne gesendetes Token ist kein Sitzungsende und trägt die Server-Meldung', async () => {
    global.fetch = vi.fn().mockResolvedValue(res(401, { error: 'Invalid email or password' }))
    const err = await useApi().apiFetch('/api/login', { method: 'POST', auth: false }).catch(e => e)
    expect(err.message).toBe('Invalid email or password')
    expect(err.status).toBe(401)
    expect(isSessionExpired(err)).toBe(false)
  })

  // Der Login schickt ein evtl. noch gespeichertes, veraltetes
  // Token nicht mit, damit sein 401 nie als Sitzungsende gilt.
  it('sendet bei auth: false kein Token und meldet bei 401 nicht ab', async () => {
    useAuth().setAuth('jwt-alt', { email: 'a@x.de' })
    global.fetch = vi.fn().mockResolvedValue(res(401, { error: 'Invalid email or password' }))
    const err = await useApi().apiFetch('/api/login', { method: 'POST', auth: false }).catch(e => e)
    expect(global.fetch.mock.calls[0][1].headers['X-Auth-Token']).toBeUndefined()
    expect(global.fetch.mock.calls[0][1].auth).toBeUndefined()
    expect(isSessionExpired(err)).toBe(false)
    expect(useAuth().token.value).toBe('jwt-alt')
  })

  // Eine 401 von Caddys Basic Auth (WWW-Authenticate: Basic)
  // betrifft den vorgeschalteten Zugang, nicht die App-Sitzung.
  it.each(['Basic realm="restricted"', 'basic'])('401 mit WWW-Authenticate %s meldet nicht ab', async (challenge) => {
    useAuth().setAuth('jwt-1', { email: 'a@x.de' })
    global.fetch = vi.fn().mockResolvedValue({ ...res(401), headers: new Headers({ 'WWW-Authenticate': challenge }) })
    const err = await useApi().apiFetch('/api/x').catch(e => e)
    expect(err.status).toBe(401)
    expect(err.basicAuth).toBe(true)
    expect(err.message).toBe('Zugang zum Server verweigert (vorgeschaltete Anmeldung).')
    expect(isSessionExpired(err)).toBe(false)
    expect(useAuth().token.value).toBe('jwt-1')
  })

  it('401 mit anderem WWW-Authenticate-Schema gilt bei gesendetem Token als Sitzungsende', async () => {
    useAuth().setAuth('jwt-1', { email: 'a@x.de' })
    global.fetch = vi.fn().mockResolvedValue({ ...res(401), headers: new Headers({ 'WWW-Authenticate': 'Bearer' }) })
    const err = await useApi().apiFetch('/api/x').catch(e => e)
    expect(err.basicAuth).toBeUndefined()
    expect(isSessionExpired(err)).toBe(true)
  })

  // Die Meldungen kamen bisher fest auf Deutsch, auch in der englischen Oberfläche.
  it('übersetzt die eigenen Fehlermeldungen in die aktive Sprache', async () => {
    i18n.global.locale.value = 'en'
    try {
      useAuth().setAuth('jwt-1', { email: 'a@x.de' })
      global.fetch = vi.fn()
        .mockResolvedValueOnce(res(403))
        .mockResolvedValueOnce({ ok: false, status: 502, json: async () => { throw new Error('kein json') } })
        .mockResolvedValueOnce(res(401))
      expect((await useApi().apiFetch('/api/x').catch(e => e)).message).toBe('Access denied (missing permission)')
      expect((await useApi().apiFetch('/api/x').catch(e => e)).message).toBe('HTTP error 502')
      expect((await useApi().apiFetch('/api/x').catch(e => e)).message).toBe('Session expired. Please sign in again.')
    } finally {
      i18n.global.locale.value = 'de'
    }
  })

  it('isSessionExpired ist null-sicher', () => {
    expect(isSessionExpired(null)).toBe(false)
    expect(isSessionExpired(undefined)).toBe(false)
  })

  // Das Login-Token geht über X-Auth-Token; Authorization bleibt
  // frei, damit der Browser Caddys Basic-Auth-Zugangsdaten selbst mitschickt.
  it.each([
    ['eingeloggt', 'jwt-1', 'jwt-1'],
    ['ausgeloggt', '', undefined],
  ])('sendet das Token als X-Auth-Token, nie als Authorization (%s)', async (_name, tok, expected) => {
    if (tok) useAuth().setAuth(tok, { email: 'a@x.de' })
    global.fetch = vi.fn().mockResolvedValue(res(200, { ok: true }))
    await useApi().apiFetch('/api/x')
    const headers = global.fetch.mock.calls[0][1].headers
    expect(headers['X-Auth-Token']).toBe(expected)
    expect(headers.Authorization).toBeUndefined()
  })

  it('isForbidden ist null-sicher', () => {
    expect(isForbidden(null)).toBe(false)
    expect(isForbidden(undefined)).toBe(false)
  })

  it('nutzt "HTTP Fehler <status>", wenn der Body kein JSON ist', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: false, status: 502, json: async () => { throw new Error('kein json') } })
    const err = await useApi().apiFetch('/api/x').catch(e => e)
    expect(err.message).toBe('HTTP Fehler 502')
    expect(err.status).toBe(502)
  })

  it('liefert JSON bei Erfolg und null bei 204', async () => {
    global.fetch = vi.fn().mockResolvedValueOnce(res(200, { a: 1 })).mockResolvedValueOnce(res(204))
    expect(await useApi().apiFetch('/api/x')).toEqual({ a: 1 })
    expect(await useApi().apiFetch('/api/x')).toBeNull()
  })

  it('Netzwerkfehler haben keinen Status', async () => {
    global.fetch = vi.fn().mockRejectedValue(new TypeError('Failed to fetch'))
    const err = await useApi().apiFetch('/api/x').catch(e => e)
    expect(err.status).toBeUndefined()
  })

  // Der Browser-TypeError ist englisch und je Browser verschieden.
  it.each(['Failed to fetch', 'Load failed', 'NetworkError when attempting to fetch resource.'])(
    'zeigt bei Netzwerkfehler (%s) eine übersetzte Meldung statt des Browsertexts', async (browserText) => {
      global.fetch = vi.fn().mockRejectedValue(new TypeError(browserText))
      const err = await useApi().apiFetch('/api/x').catch(e => e)
      expect(err.message).toBe(i18n.global.t('api.networkError'))
      expect(err.message).not.toContain(browserText)
      expect(err.network).toBe(true)
    })

  // 200 mit HTML-Body (Proxy) zeigte "Unexpected token '<' ...".
  it('zeigt bei unlesbarem Erfolgs-Body keinen rohen Parserfehler', async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: true, status: 200, headers: new Headers(), json: async () => { throw new SyntaxError("Unexpected token '<'") },
    })
    const err = await useApi().apiFetch('/api/x').catch(e => e)
    expect(err.message).toBe(i18n.global.t('api.errors.server_error'))
    expect(err.message).not.toContain('Unexpected')
  })
})
