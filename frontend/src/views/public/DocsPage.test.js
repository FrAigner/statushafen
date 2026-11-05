// DocsPage rendert Markdown per v-html und verlässt sich dabei auf DOMPurify;
// happy-dom ist dafür zu unvollständig, deshalb jsdom.
// @vitest-environment jsdom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import i18n from '../../i18n'
import DocsPage from './DocsPage.vue'
import { SECTION_IDS, DOC_SOURCES, extractTitle } from '../../docs'
import { axe } from '../../test-support/axe.js'

function makeRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/docs/:section?', component: DocsPage },
    ],
  })
}

async function mountDocs(path = '/docs') {
  const router = makeRouter()
  router.push(path)
  await router.isReady()
  const wrapper = mount({ template: '<router-view />' }, {
    global: { plugins: [i18n, router] },
    attachTo: document.body,
  })
  await flushPromises()
  return { wrapper, router }
}

const titleOf = (id, locale) => extractTitle(DOC_SOURCES[locale][id])

describe('DocsPage', () => {
  let wrapper
  beforeEach(() => {
    i18n.global.locale.value = 'de'
    window.scrollTo = vi.fn()
    Element.prototype.scrollIntoView = vi.fn()
  })
  afterEach(() => {
    wrapper?.unmount()
    wrapper = undefined
  })

  it('zeigt alle Abschnitte in der Navigation und auf /docs den ersten Abschnitt', async () => {
    ;({ wrapper } = await mountDocs('/docs'))

    const links = wrapper.findAll('.docs-nav a')
    expect(links.map(a => a.attributes('href'))).toEqual(SECTION_IDS.map(id => `/docs/${id}`))
    expect(links.map(a => a.text())).toEqual(SECTION_IDS.map(id => titleOf(id, 'de')))
    expect(wrapper.find('.docs-title').text()).toBe(titleOf(SECTION_IDS[0], 'de'))
    expect(links[0].attributes('aria-current')).toBe('page')
    expect(links[1].attributes('aria-current')).toBeUndefined()
  })

  it('rendert jeden Abschnitt mit Inhalt und Inhaltsverzeichnis', async () => {
    ;({ wrapper } = await mountDocs('/docs/vorfaelle'))

    expect(wrapper.find('.docs-title').text()).toBe('Vorfälle')
    const toc = wrapper.findAll('.docs-toc a')
    expect(toc.map(a => a.text())).toContain('Status-Ablauf')
    const statusLink = toc.find(a => a.text() === 'Status-Ablauf')
    expect(statusLink.attributes('href')).toBe('/docs/vorfaelle#status-ablauf')
    expect(wrapper.find('#status-ablauf').exists()).toBe(true)
  })

  it('Anker scrollen zur Überschrift, auch beim Aufruf per Deep-Link', async () => {
    ;({ wrapper } = await mountDocs('/docs/vorfaelle#updates'))
    const scrolled = vi.mocked(Element.prototype.scrollIntoView)
    expect(scrolled).toHaveBeenCalled()
    expect(scrolled.mock.contexts.at(-1).id).toBe('updates')

    scrolled.mockClear()
    await wrapper.findAll('.docs-toc a').find(a => a.text() === 'Status-Ablauf').trigger('click')
    await flushPromises()
    expect(scrolled.mock.contexts.at(-1).id).toBe('status-ablauf')
  })

  it('leitet /docs#abschnitt auf die Unterseite um', async () => {
    let router
    ;({ wrapper, router } = await mountDocs('/docs#abonnements'))
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/docs/abonnements')
    expect(wrapper.find('.docs-title').text()).toBe('Abonnements')

    // auch bei einer Navigation innerhalb der bereits offenen Doku
    await router.push('/docs#faq')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/docs/faq')
    expect(wrapper.find('.docs-title').text()).toBe('FAQ')
  })

  it('öffnet interne Links im Inhalt über den Router', async () => {
    let router
    ;({ wrapper, router } = await mountDocs('/docs/faq'))
    const link = wrapper.find('.docs-markdown a[href="/docs/systemeinstellungen"]')
    expect(link.exists()).toBe(true)
    await link.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/docs/systemeinstellungen')
  })

  it('Anker-Links im Inhalt bleiben auf der Seite und ändern nur den Hash', async () => {
    let router
    ;({ wrapper, router } = await mountDocs('/docs/faq'))
    const md = wrapper.find('.docs-markdown')
    md.element.insertAdjacentHTML('beforeend', '<a id="probe" href="#wie-beende-ich-ein-abo">x</a><a id="ext" href="https://example.com">e</a>')
    await md.find('#probe').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/docs/faq#wie-beende-ich-ein-abo')

    // Externe Links lässt die Seite in Ruhe. Der Listener am Dokument merkt
    // sich das und verhindert erst danach die (in jsdom nicht unterstützte)
    // Navigation.
    let preventedByPage
    const spy = (e) => { preventedByPage = e.defaultPrevented; e.preventDefault() }
    document.addEventListener('click', spy)
    md.find('#ext').element.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
    document.removeEventListener('click', spy)
    expect(preventedByPage).toBe(false)
  })

  it('lädt beim Sprachwechsel die englische Fassung', async () => {
    ;({ wrapper } = await mountDocs('/docs/vorfaelle'))
    expect(wrapper.find('.docs-title').text()).toBe('Vorfälle')

    i18n.global.locale.value = 'en'
    await flushPromises()
    expect(wrapper.find('.docs-title').text()).toBe('Incidents')
    expect(wrapper.find('.docs-nav a.active').text()).toBe('Incidents')
    expect(wrapper.find('.docs-fallback').exists()).toBe(false)
    expect(wrapper.find('.docs-back-to-top').text()).toBe('Back to top')
  })

  it('zeigt den Fallback-Hinweis, wenn eine englische Seite fehlt', async () => {
    const saved = DOC_SOURCES.en.vorlagen
    delete DOC_SOURCES.en.vorlagen
    try {
      i18n.global.locale.value = 'en'
      ;({ wrapper } = await mountDocs('/docs/vorlagen'))
      expect(wrapper.find('.docs-fallback').text()).toContain('not yet available in English')
      expect(wrapper.find('.docs-title').text()).toBe('Vorlagen')
    } finally {
      DOC_SOURCES.en.vorlagen = saved
    }
  })

  it('filtert die Navigation über das Suchfeld, auch im Text', async () => {
    ;({ wrapper } = await mountDocs('/docs'))
    const input = wrapper.find('#docs-search-input')

    await input.setValue('X-Webhook-Token')
    expect(wrapper.findAll('.docs-nav a').map(a => a.attributes('href'))).toContain('/docs/api-und-webhooks')
    expect(wrapper.findAll('.docs-nav a').length).toBeLessThan(SECTION_IDS.length)

    await input.setValue('Audit')
    expect(wrapper.findAll('.docs-nav a').map(a => a.text())).toContain('Audit-Log')

    await input.setValue('xyzgibtsnicht')
    expect(wrapper.findAll('.docs-nav a')).toHaveLength(0)
    expect(wrapper.find('.docs-nav-empty').text()).toBe('Kein Abschnitt gefunden.')

    await input.setValue('  ')
    expect(wrapper.findAll('.docs-nav a')).toHaveLength(SECTION_IDS.length)
  })

  it('Navigation lässt sich auf schmalen Bildschirmen auf- und zuklappen und schließt beim Wechsel', async () => {
    let router
    ;({ wrapper, router } = await mountDocs('/docs'))
    const toggle = wrapper.find('.docs-nav-toggle')
    expect(toggle.attributes('aria-expanded')).toBe('false')
    expect(toggle.attributes('aria-controls')).toBe('docs-sidebar')
    expect(toggle.text()).toBe('Abschnitte anzeigen')

    await toggle.trigger('click')
    expect(toggle.attributes('aria-expanded')).toBe('true')
    expect(wrapper.find('.docs').classes()).toContain('is-nav-open')
    expect(toggle.text()).toBe('Abschnitte ausblenden')

    await router.push('/docs/faq')
    await flushPromises()
    expect(toggle.attributes('aria-expanded')).toBe('false')
  })

  it('zeigt Vor- und Zurück-Links an den Rändern korrekt', async () => {
    ;({ wrapper } = await mountDocs('/docs'))
    expect(wrapper.find('.docs-pager-prev').exists()).toBe(false)
    expect(wrapper.find('.docs-pager-next').attributes('href')).toBe(`/docs/${SECTION_IDS[1]}`)
    wrapper.unmount()

    ;({ wrapper } = await mountDocs(`/docs/${SECTION_IDS.at(-1)}`))
    expect(wrapper.find('.docs-pager-next').exists()).toBe(false)
    expect(wrapper.find('.docs-pager-prev').attributes('href')).toBe(`/docs/${SECTION_IDS.at(-2)}`)
  })

  it('„Zurück nach oben“ scrollt an den Anfang und setzt den Fokus auf den Inhalt', async () => {
    ;({ wrapper } = await mountDocs('/docs/faq'))
    await wrapper.find('.docs-back-to-top').trigger('click')
    expect(window.scrollTo).toHaveBeenCalledWith({ top: 0, behavior: 'smooth' })
    expect(document.activeElement.id).toBe('docs-title')
  })

  it('lässt /docs#anker ohne passenden Abschnitt auf der Startseite und ignoriert unbekannte Anker', async () => {
    let router
    ;({ wrapper, router } = await mountDocs('/docs#gibt-es-nicht'))
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/docs')
    expect(Element.prototype.scrollIntoView).not.toHaveBeenCalled()
  })

  it('Klicks im Inhalt außerhalb von App-Links lässt die Seite in Ruhe', async () => {
    let router
    ;({ wrapper, router } = await mountDocs('/docs/faq'))
    const md = wrapper.find('.docs-markdown')
    md.element.insertAdjacentHTML('beforeend', '<span id="txt">t</span><a id="api" href="/api/public/pages">a</a><a id="proto" href="//example.com/x">p</a>')
    for (const sel of ['#txt', '#api', '#proto']) {
      let prevented
      const spy = (e) => { prevented = e.defaultPrevented; e.preventDefault() }
      document.addEventListener('click', spy)
      md.find(sel).element.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
      document.removeEventListener('click', spy)
      expect(prevented, sel).toBe(false)
    }
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/docs/faq')
  })

  it('nutzt die Abschnitts-ID als Titel, wenn ein Abschnitt in keiner Sprache vorliegt', async () => {
    const saved = { de: DOC_SOURCES.de.faq, en: DOC_SOURCES.en.faq }
    delete DOC_SOURCES.de.faq
    delete DOC_SOURCES.en.faq
    try {
      ;({ wrapper } = await mountDocs('/docs/faq'))
      expect(wrapper.findAll('.docs-nav a').at(-1).text()).toBe('faq')
      expect(wrapper.find('.docs-not-found').exists()).toBe(true)
    } finally {
      DOC_SOURCES.de.faq = saved.de
      DOC_SOURCES.en.faq = saved.en
    }
  })

  it('zeigt für unbekannte Abschnitte einen Hinweis mit Link zur Übersicht', async () => {
    ;({ wrapper } = await mountDocs('/docs/gibt-es-nicht'))
    const nf = wrapper.find('.docs-not-found')
    expect(nf.attributes('role')).toBe('alert')
    expect(nf.find('h1').text()).toBe('Abschnitt nicht gefunden')
    expect(nf.find('a').attributes('href')).toBe('/docs')
  })

  it('hat keine axe-Funde', async () => {
    ;({ wrapper } = await mountDocs('/docs/api-und-webhooks'))
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  // Jeder Abschnitt hat genau eine h1 (der Titel) und darunter
  // nur h2/h3 in lückenloser Reihenfolge; Tabellen und Listen dürfen axe nicht
  // verletzen. Geprüft für alle Abschnitte in beiden Sprachen.
  it.each(['de', 'en'])('hat in allen Abschnitten (%s) eine h1 und keine axe-Funde', async (loc) => {
    i18n.global.locale.value = loc
    for (const id of SECTION_IDS) {
      ;({ wrapper } = await mountDocs(`/docs/${id}`))
      expect(wrapper.findAll('h1'), `${loc}/${id}`).toHaveLength(1)
      const levels = wrapper.findAll('.docs-content h2, .docs-content h3').map(h => Number(h.element.tagName[1]))
      expect(levels.length, `${loc}/${id}: keine Überschriften`).toBeGreaterThan(0)
      expect(levels[0], `${loc}/${id}: erste Überschrift`).toBe(2)
      expect(await axe(wrapper), `${loc}/${id}`).toHaveNoViolations()
      wrapper.unmount()
      wrapper = null
    }
  }, 120000)
})
