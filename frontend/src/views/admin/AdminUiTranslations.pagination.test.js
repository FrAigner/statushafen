import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import { setAuthAsPageRole } from '../../test-support/pageRole'
import { useTenant } from '../../composables/useTenant'
import AdminUiTranslations from './AdminUiTranslations.vue'
import de from '../../i18n/locales/de.json'
import { flatten } from '../../i18n/overrides'
import { axe } from '../../test-support/axe.js'

// Die Liste rendert die rund 1.700 App-Texte seitenweise.
// Die Daten sind die echten Katalog-Keys (kein Mock), damit die Größenordnung
// stimmt.
const TOTAL = Object.keys(flatten(de)).length

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

let activeWrapper = null
async function mountView(fetchImpl) {
  global.fetch = fetchImpl || vi.fn().mockResolvedValue(jsonResponse({}))
  const wrapper = mount(AdminUiTranslations, { global: { plugins: [i18n] }, attachTo: document.body })
  await flushPromises()
  activeWrapper = wrapper
  return wrapper
}
const info = (w) => w.find('.pager-info').text()
const pagerButtons = (w) => w.findAll('.pager-btn')
const nextPage = async (w) => { await pagerButtons(w)[1].trigger('click'); await flushPromises() }
const prevPage = async (w) => { await pagerButtons(w)[0].trigger('click'); await flushPromises() }

describe('AdminUiTranslations Paginierung', () => {
  beforeEach(() => {
    useAuth().logout()
    useTenant().setActiveTenant(null)
    useTenant().setMyPages([])
    localStorage.clear()
    vi.spyOn(console, 'error').mockImplementation(() => {})
    i18n.global.locale.value = 'de'
    setAuthAsPageRole('jwt-1', { is_global_admin: true, permissions: '' })
  })
  afterEach(() => {
    activeWrapper?.unmount()
    activeWrapper = null
    vi.restoreAllMocks()
  })

  it('rendert bei über 1.000 Texten nur eine Seite mit 50 Zeilen im DOM', async () => {
    expect(TOTAL).toBeGreaterThan(1000)
    const wrapper = await mountView()
    expect(wrapper.findAll('.override-row')).toHaveLength(50)
    expect(wrapper.findAll('.override-input')).toHaveLength(50)
    expect(info(wrapper)).toBe(`Zeilen 1–50 von ${TOTAL}`)
  })

  it('Seitengröße 25/50/100 ändert die Zeilenzahl und springt auf Seite 1', async () => {
    const wrapper = await mountView()
    await nextPage(wrapper)
    await wrapper.find('.pager select').setValue('100')
    expect(wrapper.findAll('.override-row')).toHaveLength(100)
    expect(info(wrapper)).toBe(`Zeilen 1–100 von ${TOTAL}`)
    await wrapper.find('.pager select').setValue('25')
    expect(wrapper.findAll('.override-row')).toHaveLength(25)
  })

  it('Ungespeicherte Änderungen bleiben über Seitenwechsel erhalten und werden gezählt', async () => {
    const wrapper = await mountView()
    const firstKey = wrapper.find('.override-row .ent-mono').text()
    await wrapper.find('.override-input').setValue('Erste Seite')
    expect(wrapper.find('.count-note').text()).toContain('1 ungespeichert')

    await nextPage(wrapper)
    expect(info(wrapper)).toBe(`Zeilen 51–100 von ${TOTAL}`)
    expect(wrapper.find('.override-row .ent-mono').text()).not.toBe(firstKey)
    await wrapper.find('.override-input').setValue('Zweite Seite')
    expect(wrapper.find('.count-note').text()).toContain('2 ungespeichert')

    await prevPage(wrapper)
    expect(wrapper.find('.override-row .ent-mono').text()).toBe(firstKey)
    expect(wrapper.find('.override-input').element.value).toBe('Erste Seite')
  })

  it('Änderungen überleben auch Filter und Suche', async () => {
    const wrapper = await mountView()
    const firstKey = wrapper.find('.override-row .ent-mono').text()
    await wrapper.find('.override-input').setValue('Bleibt')
    await wrapper.find('.comp-search input').setValue(firstKey)
    expect(wrapper.findAll('.override-row').length).toBeGreaterThan(0)
    await wrapper.find('.comp-search input').setValue('')
    expect(wrapper.find('.override-input').element.value).toBe('Bleibt')
    expect(wrapper.find('.count-note').text()).toContain('1 ungespeichert')
  })

  it('Alle speichern sendet die Änderungen von mehreren Seiten', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({}))
    const wrapper = await mountView(fetchMock)
    const key1 = wrapper.find('.override-row .ent-mono').text()
    await wrapper.find('.override-input').setValue('Text 1')
    await nextPage(wrapper)
    const key2 = wrapper.find('.override-row .ent-mono').text()
    await wrapper.find('.override-input').setValue('Text 2')

    const saveAll = wrapper.findAll('.comp-card__tools .action-btn').find(b => b.text().includes('Alle speichern'))
    expect(saveAll.text()).toBe('Alle speichern (2)')
    await saveAll.trigger('click')
    await flushPromises()

    const puts = fetchMock.mock.calls.filter(([, o]) => o?.method === 'PUT').map(([, o]) => JSON.parse(o.body))
    expect(puts).toEqual([
      { language_code: 'de', key: key1, value: 'Text 1' },
      { language_code: 'de', key: key2, value: 'Text 2' },
    ])
    expect(wrapper.find('.count-note').text()).toBe('2 angepasst')
  })

  it('Alle speichern meldet Fehler und behält den Entwurf', async () => {
    const fetchMock = vi.fn().mockImplementation((url, opts) => Promise.resolve(opts?.method === 'PUT' ? jsonResponse({ error: 'kaputt' }, 500) : jsonResponse({})))
    const wrapper = await mountView(fetchMock)
    await wrapper.find('.override-input').setValue('X')
    await wrapper.findAll('.comp-card__tools .action-btn').find(b => b.text().includes('Alle speichern')).trigger('click')
    await flushPromises()
    expect(wrapper.find('.alert-error').text()).toContain('1 Einträge')
    expect(wrapper.find('.count-note').text()).toContain('1 ungespeichert')
  })

  it('Entwürfe sind je Sprache getrennt (kein Übersprechen de -> en)', async () => {
    const wrapper = await mountView()
    await wrapper.find('.override-input').setValue('nur deutsch')
    await wrapper.find('select[aria-label="Sprache"]').setValue('en')
    await flushPromises()
    expect(wrapper.find('.override-input').element.value).toBe('')
    await wrapper.find('select[aria-label="Sprache"]').setValue('de')
    await flushPromises()
    expect(wrapper.find('.override-input').element.value).toBe('nur deutsch')
  })

  it('zählt angepasste Texte über alle Seiten und springt bei Filterwechsel auf Seite 1', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ 'adminNav.items.sso': 'a', 'common.save': 'b' }))
    const wrapper = await mountView(fetchMock)
    expect(wrapper.find('.count-note').text()).toBe('2 angepasst')
    await nextPage(wrapper)
    await wrapper.find('select[aria-label="Bereich"]').setValue('adminNav')
    expect(info(wrapper).startsWith('Zeilen 1–')).toBe(true)
  })

  it('zählt eine Eingabe nur als ungespeichert, wenn sie vom gespeicherten Override abweicht', async () => {
    const wrapper0 = await mountView()
    const firstKey = wrapper0.find('.override-row .ent-mono').text()
    wrapper0.unmount()
    const wrapper = await mountView(vi.fn().mockResolvedValue(jsonResponse({ [firstKey]: 'Gespeichert' })))
    expect(wrapper.find('.override-input').element.value).toBe('Gespeichert')
    await wrapper.find('.override-input').setValue('Geändert')
    expect(wrapper.find('.count-note').text()).toBe('1 angepasst · 1 ungespeichert')
    await wrapper.find('.override-input').setValue('Gespeichert')
    expect(wrapper.find('.count-note').text()).toBe('1 angepasst')
  })

  it('setzt den Fokus nach dem Seitenwechsel in die Liste', async () => {
    const wrapper = await mountView()
    await nextPage(wrapper)
    await flushPromises()
    expect(document.activeElement).toBe(wrapper.find('[role="region"]').element)
  })

  it('zeigt ohne Treffer und ohne aktive Seite keine Paginierung', async () => {
    const wrapper = await mountView()
    await wrapper.find('.comp-search input').setValue('gibt-es-garantiert-nicht-xyz')
    expect(wrapper.find('.pager').exists()).toBe(false)
  })

  it('liest ohne Schreibrecht nur und bietet kein Alle speichern an', async () => {
    useAuth().logout()
    setAuthAsPageRole('jwt-1', { is_global_admin: false, permissions: '' })
    useTenant().setActiveTenant('7')
    const wrapper = await mountView()
    expect(wrapper.findAll('.override-row')).toHaveLength(50)
    expect(wrapper.text()).not.toContain('Alle speichern')
  })

  it('hat keine axe-Funde auf der paginierten Liste', async () => {
    const wrapper = await mountView()
    expect(await axe(wrapper)).toHaveNoViolations()
  })
})
