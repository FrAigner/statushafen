import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import InvitationPage from './InvitationPage.vue'
import { axe } from '../../test-support/axe.js'

// Einladung auf eine Statusseite annehmen oder ablehnen.

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

const preview = { page_name: 'Acme', role_name: 'Viewer', expires_at: '2099-01-01T00:00:00Z' }

function fetchRouter({ previewStatus = 200, previewBody = preview, actionStatus = 200, actionBody = {} } = {}) {
  return vi.fn(async (url) => {
    if (url.endsWith('/api/invitations/preview')) return jsonResponse(previewBody, previewStatus)
    if (url.endsWith('/api/invitations/accept') || url.endsWith('/api/invitations/decline')) return jsonResponse(actionBody, actionStatus)
    throw new Error(`Unerwarteter Fetch-Aufruf: ${url}`)
  })
}

async function mountPage({ token = 'tok-123', hashToken = null, loggedIn = true, fetchImpl = fetchRouter(), beforeMount = null } = {}) {
  if (loggedIn) useAuth().setAuth('jwt-1', { email: 'neu@x.de', is_global_admin: false })
  else useAuth().logout()
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/invitation', component: InvitationPage },
      { path: '/login', component: { template: '<div/>' } },
      { path: '/admin', component: { template: '<div/>' } },
    ],
  })
  if (hashToken !== null) router.push(`/invitation${token !== null ? `?token=${token}` : ''}#token=${encodeURIComponent(hashToken)}`)
  else router.push(token !== null ? `/invitation?token=${token}` : '/invitation')
  await router.isReady()
  global.fetch = fetchImpl
  if (beforeMount) beforeMount(router)
  const wrapper = mount(InvitationPage, { global: { plugins: [i18n, router] } })
  await flushPromises()
  return { wrapper, fetchImpl, router }
}

// Blockierter Speicher (privates Fenster, Cookies gesperrt): jeder Zugriff wirft.
function blockedStorage() {
  const boom = () => { throw new Error('blockiert') }
  return { getItem: boom, setItem: boom, removeItem: boom }
}

const bodyOf = (call) => JSON.parse(call[1].body)

describe('InvitationPage', () => {
  beforeEach(() => {
    sessionStorage.clear()
    i18n.global.locale.value = 'de'
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })
  afterEach(() => {
    useAuth().logout()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('zeigt ohne Token einen Fehler und ruft nichts auf', async () => {
    const { wrapper, fetchImpl } = await mountPage({ token: null })
    expect(fetchImpl).not.toHaveBeenCalled()
    expect(wrapper.find('.invitation-error').text()).toContain('Kein Einladungs-Token in diesem Link gefunden.')
  })

  it('verlangt die Anmeldung und springt danach mit Token zurück', async () => {
    const { wrapper, fetchImpl } = await mountPage({ loggedIn: false, token: 'ABC 1' })
    expect(fetchImpl).not.toHaveBeenCalled()
    expect(wrapper.find('.invitation-login').text()).toContain('Melde dich mit der E-Mail-Adresse an')
    // Der Token wandert weder in returnTo (Query) noch in ein Fragment der Login-Adresse.
    const href = wrapper.find('.invitation-login a').attributes('href')
    expect(href).toBe('/login?returnTo=' + encodeURIComponent('/invitation'))
    expect(href).not.toContain('ABC')
  })

  it('liest den Token aus dem Fragment (#token=...) und nutzt ihn für die Vorschau', async () => {
    const { fetchImpl } = await mountPage({ token: null, hashToken: 'frag ment' })
    expect(bodyOf(fetchImpl.mock.calls[0])).toEqual({ token: 'frag ment' })
  })

  it('bevorzugt das Fragment vor der alten Query (beide gesetzt)', async () => {
    const { fetchImpl, router } = await mountPage({ token: 'alt', hashToken: 'neu' })
    expect(bodyOf(fetchImpl.mock.calls[0])).toEqual({ token: 'neu' })
    expect(router.currentRoute.value.fullPath).toBe('/invitation')
  })

  it('entfernt den Token aus Query (Altlink) und Adresszeile, sobald er gelesen ist', async () => {
    const { router } = await mountPage({ token: 'alt-1' })
    expect(router.currentRoute.value.fullPath).toBe('/invitation')
    expect(router.currentRoute.value.query).toEqual({})
    expect(router.currentRoute.value.hash).toBe('')
  })

  it('speichert den gelesenen Token unter invitationToken in sessionStorage', async () => {
    await mountPage({ token: null, hashToken: 'tok-key' })
    expect(sessionStorage.getItem('invitationToken')).toBe('tok-key')
  })

  it('liest nur das führende # ab (ein zweites # gehört zum Wert)', async () => {
    const { fetchImpl } = await mountPage({ token: null, hashToken: 'a#b' })
    expect(bodyOf(fetchImpl.mock.calls[0])).toEqual({ token: 'a#b' })
  })

  it('ersetzt die Adresse nicht, wenn der Token aus sessionStorage kommt', async () => {
    sessionStorage.setItem('invitationToken', 'tok-ss')
    let spy
    const { fetchImpl } = await mountPage({ token: null, beforeMount: (r) => { spy = vi.spyOn(r, 'replace') } })
    expect(bodyOf(fetchImpl.mock.calls[0])).toEqual({ token: 'tok-ss' })
    expect(spy).not.toHaveBeenCalled()
  })

  it('behält den Token bei vorübergehenden Fehlern (500, Netzwerk); Neuladen klappt danach', async () => {
    const err500 = fetchRouter({ previewStatus: 500, previewBody: { error: 'boom' } })
    await mountPage({ token: null, hashToken: 'tok-keep', fetchImpl: err500 })
    expect(sessionStorage.getItem('invitationToken')).toBe('tok-keep')
    const net = vi.fn(async () => { throw new TypeError('Failed to fetch') })
    await mountPage({ token: null, fetchImpl: net })
    expect(sessionStorage.getItem('invitationToken')).toBe('tok-keep')
    const { wrapper } = await mountPage({ token: null })
    expect(wrapper.find('.invitation-ready').exists()).toBe(true)
  })

  it.each(['invitation_not_found', 'invitation_expired', 'invitation_already_member'])('löscht den Token bei endgültigem Fehler %s', async (code) => {
    await mountPage({ token: null, hashToken: 'tok-final', fetchImpl: fetchRouter({ previewStatus: 404, previewBody: { error: 'x', code } }) })
    expect(sessionStorage.getItem('invitationToken')).toBeNull()
  })

  it('behält den Token bei Kontokonflikt (richtiges Konto kann ihn noch nutzen)', async () => {
    await mountPage({ token: null, hashToken: 'tok-conf', fetchImpl: fetchRouter({ previewStatus: 409, previewBody: { error: 'x', code: 'invitation_account_conflict' } }) })
    expect(sessionStorage.getItem('invitationToken')).toBe('tok-conf')
  })

  it('entfernt den Token auch aus dem Fragment', async () => {
    const { router } = await mountPage({ token: null, hashToken: 'tok-h' })
    expect(router.currentRoute.value.hash).toBe('')
    expect(router.currentRoute.value.fullPath).toBe('/invitation')
  })

  it('Neuladen ohne Token in der Adresse nutzt den Token der Tab-Sitzung (sessionStorage)', async () => {
    await mountPage({ token: null, hashToken: 'tok-reload' })
    const { fetchImpl, wrapper } = await mountPage({ token: null })
    expect(bodyOf(fetchImpl.mock.calls[0])).toEqual({ token: 'tok-reload' })
    expect(wrapper.find('.invitation-ready').exists()).toBe(true)
    expect(localStorage.getItem('invitationToken')).toBeNull()
  })

  it('Login-Rückkehr: Token überlebt den Weg über /login ohne Query', async () => {
    const first = await mountPage({ loggedIn: false, token: null, hashToken: 'tok-login' })
    expect(first.wrapper.find('.invitation-login').exists()).toBe(true)
    // nach dem Login führt returnTo auf die nackte Adresse
    const { fetchImpl, router } = await mountPage({ token: null })
    expect(bodyOf(fetchImpl.mock.calls[0])).toEqual({ token: 'tok-login' })
    expect(router.currentRoute.value.fullPath).toBe('/invitation')
  })

  it('vergisst den Token nach Annehmen und Ablehnen', async () => {
    const a = await mountPage({ token: null, hashToken: 't1' })
    await a.wrapper.find('.btn-accept').trigger('click')
    await flushPromises()
    expect(sessionStorage.getItem('invitationToken')).toBeNull()
    const d = await mountPage({ token: null, hashToken: 't2' })
    await d.wrapper.find('.btn-decline').trigger('click')
    await flushPromises()
    expect(sessionStorage.getItem('invitationToken')).toBeNull()
  })

  it('vergisst den Token bei einem Fehler der Vorschau', async () => {
    await mountPage({ token: null, hashToken: 't3', fetchImpl: fetchRouter({ previewStatus: 404, previewBody: { error: 'x', code: 'invitation_not_found' } }) })
    expect(sessionStorage.getItem('invitationToken')).toBeNull()
  })

  it('zeigt ohne Token in der Adresse und bei blockiertem sessionStorage den Fehler', async () => {
    vi.stubGlobal('sessionStorage', blockedStorage())
    const { wrapper, fetchImpl } = await mountPage({ token: null })
    expect(fetchImpl).not.toHaveBeenCalled()
    expect(wrapper.find('.invitation-error').text()).toContain('Kein Einladungs-Token')
  })

  it('funktioniert, wenn sessionStorage nicht verfügbar ist', async () => {
    vi.stubGlobal('sessionStorage', blockedStorage())
    const { fetchImpl } = await mountPage({ token: null, hashToken: 'tok-ss' })
    expect(bodyOf(fetchImpl.mock.calls[0])).toEqual({ token: 'tok-ss' })
  })

  it('zeigt Seite und Rolle aus der Vorschau, ohne den Token zu verbrauchen', async () => {
    const { wrapper, fetchImpl } = await mountPage()
    expect(fetchImpl).toHaveBeenCalledTimes(1)
    expect(fetchImpl.mock.calls[0][0]).toContain('/api/invitations/preview')
    expect(bodyOf(fetchImpl.mock.calls[0])).toEqual({ token: 'tok-123' })
    expect(fetchImpl.mock.calls[0][1].method).toBe('POST')
    const text = wrapper.find('.invitation-ready').text()
    expect(text).toContain('„Acme“')
    expect(text).toContain('„Viewer“')
    expect(wrapper.find('.btn-accept').attributes('disabled')).toBeUndefined()
  })

  it('nimmt an und zeigt den Erfolg mit Link zur Verwaltung', async () => {
    const { wrapper, fetchImpl } = await mountPage()
    await wrapper.find('.btn-accept').trigger('click')
    await flushPromises()
    const call = fetchImpl.mock.calls[1]
    expect(call[0]).toContain('/api/invitations/accept')
    expect(bodyOf(call)).toEqual({ token: 'tok-123' })
    expect(wrapper.find('.invitation-accepted').text()).toContain('Du bist jetzt Mitglied der Statusseite „Acme“.')
    expect(wrapper.find('.invitation-accepted a').attributes('href')).toBe('/admin')
  })

  it('lehnt ab und meldet das', async () => {
    const { wrapper, fetchImpl } = await mountPage()
    await wrapper.find('.btn-decline').trigger('click')
    await flushPromises()
    expect(fetchImpl.mock.calls[1][0]).toContain('/api/invitations/decline')
    expect(wrapper.find('.invitation-declined').text()).toContain('Einladung abgelehnt')
  })

  it('sperrt beide Knöpfe, solange die Antwort läuft', async () => {
    const base = fetchRouter()
    const fetchImpl = vi.fn(async (url, o) => (url.endsWith('/accept') ? new Promise(() => {}) : base(url, o)))
    const { wrapper } = await mountPage({ fetchImpl })
    await wrapper.find('.btn-accept').trigger('click')
    await flushPromises()
    expect(wrapper.find('.btn-accept').attributes('disabled')).toBeDefined()
    expect(wrapper.find('.btn-decline').attributes('disabled')).toBeDefined()
  })

  it('übersetzt den Fehlercode der Vorschau (fremdes Konto / unbekannter Token)', async () => {
    const { wrapper } = await mountPage({ fetchImpl: fetchRouter({ previewStatus: 404, previewBody: { error: 'x', code: 'invitation_not_found' } }) })
    expect(wrapper.find('.invitation-error [role="alert"]').text()).toBe(i18n.global.t('api.errors.invitation_not_found'))
  })

  it('übersetzt den Fehlercode beim Annehmen (Kontokonflikt)', async () => {
    const { wrapper } = await mountPage({ fetchImpl: fetchRouter({ actionStatus: 409, actionBody: { error: 'x', code: 'invitation_account_conflict' } }) })
    await wrapper.find('.btn-accept').trigger('click')
    await flushPromises()
    expect(wrapper.find('.invitation-error').text()).toContain(i18n.global.t('api.errors.invitation_account_conflict'))
  })

  it('englische Texte', async () => {
    i18n.global.locale.value = 'en'
    const { wrapper } = await mountPage()
    expect(wrapper.find('.invitation-ready').text()).toContain('You have been invited to the status page "Acme" with the role "Viewer".')
    i18n.global.locale.value = 'de'
  })

  it('hat keine axe-Funde (Vorschau und Anmeldung)', async () => {
    const ready = await mountPage()
    expect(await axe(ready.wrapper)).toHaveNoViolations()
    const login = await mountPage({ loggedIn: false })
    expect(await axe(login.wrapper)).toHaveNoViolations()
  })
})
