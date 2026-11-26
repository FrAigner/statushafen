// DocsPage rendert Markdown per v-html und verlässt sich dabei auf DOMPurify;
// happy-dom ist dafür zu unvollständig, deshalb jsdom.
// @vitest-environment jsdom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import i18n from '../../i18n'
import DocsPage from './DocsPage.vue'
import { SECTION_IDS, DOC_SOURCES, extractTitle } from '../../docs'
import { axe } from '../../test-support/axe'
import { useLandingChrome } from '../../composables/useLandingChrome'
import docsSource from './DocsPage.vue?raw'

function makeRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/docs/:section?', component: DocsPage },
      { path: '/admin/docs/:section?', component: DocsPage, meta: { docsBase: '/admin/docs' } },
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

  // The docs page shows the same header and footer as the landing page (isLanding), and the
  // order button there needs the real orders flag, not the optimistic default.
  describe('landing header', () => {
    beforeEach(() => {
      useLandingChrome().isLanding.value = false
      useLandingChrome().ordersEnabled.value = true
    })

    it('switches the layout to the landing header and back when leaving', async () => {
      global.fetch = vi.fn(async () => ({ ok: true, status: 200, json: async () => ({ enabled: true }) }))
      ;({ wrapper } = await mountDocs('/docs'))
      expect(useLandingChrome().isLanding.value).toBe(true)
      wrapper.unmount()
      wrapper = undefined
      expect(useLandingChrome().isLanding.value).toBe(false)
    })

    it('loads the orders flag for the order button in the header', async () => {
      global.fetch = vi.fn(async () => ({ ok: true, status: 200, json: async () => ({ enabled: false }) }))
      ;({ wrapper } = await mountDocs('/docs'))
      expect(global.fetch.mock.calls.some(([url]) => String(url).endsWith('/api/public/orders/enabled'))).toBe(true)
      expect(useLandingChrome().ordersEnabled.value).toBe(false)
    })

    it('keeps the content as wide as the landing header and clear of the sticky header', () => {
      const css = docsSource.split('<style scoped>')[1]
      const docs = css.match(/\.docs \{([^}]*)\}/)?.[1] ?? ''
      expect(docs).toMatch(/max-width:\s*var\(--lp-max\)/)
      expect(docs).toMatch(/margin:\s*0 auto/)
      const sidebar = css.match(/\.docs-sidebar \{([^}]*)\}/)?.[1] ?? ''
      expect(sidebar).toMatch(/top:\s*calc\(var\(--lp-header-height\)/)
    })

    it('keeps the admin frame when shown inside the admin area', async () => {
      global.fetch = vi.fn()
      ;({ wrapper } = await mountDocs('/admin/docs'))
      expect(useLandingChrome().isLanding.value).toBe(false)
      expect(global.fetch).not.toHaveBeenCalled()
      expect(wrapper.find('.docs').classes()).toContain('docs--embedded')
    })

    it('scales screenshots in the docs down to the content width', () => {
      const css = docsSource.split('<style scoped>')[1]
      const img = css.match(/\.docs-markdown :deep\(img\) \{([^}]*)\}/)?.[1] ?? ''
      expect(img).toMatch(/max-width:\s*100%/)
      expect(img).toMatch(/height:\s*auto/)
    })

    it('scrolls anchored headings below the sticky header', () => {
      const css = docsSource.split('<style scoped>')[1]
      const headings = css.match(/\.docs-markdown :deep\(h3\) \{\s*scroll-margin-top:([^;]*);/)?.[1] ?? ''
      expect(headings).toMatch(/var\(--docs-anchor-offset, var\(--lp-header-height\)\)/)
    })
  })

  // Inside the admin area every docs link stays below /admin/docs, so the admin frame stays.
  describe('inside the admin area', () => {
    it('points navigation and pager to /admin/docs', async () => {
      ;({ wrapper } = await mountDocs('/admin/docs'))
      expect(wrapper.findAll('.docs-nav a').map(a => a.attributes('href'))).toEqual(SECTION_IDS.map(id => `/admin/docs/${id}`))
      expect(wrapper.find('.docs-pager-next').attributes('href')).toBe(`/admin/docs/${SECTION_IDS[1]}`)
    })

    it('opens /docs links in the content below /admin/docs', async () => {
      let router
      ;({ wrapper, router } = await mountDocs('/admin/docs/faq'))
      await wrapper.find('.docs-markdown a[href="/docs/systemeinstellungen"]').trigger('click')
      await flushPromises()
      expect(router.currentRoute.value.path).toBe('/admin/docs/systemeinstellungen')
    })

    it('redirects a section anchor to the admin section page', async () => {
      let router
      ;({ wrapper, router } = await mountDocs('/admin/docs#vorfaelle'))
      await flushPromises()
      expect(router.currentRoute.value.path).toBe('/admin/docs/vorfaelle')
    })

    it('offsets anchors by the height of the sticky admin top bar', async () => {
      const bar = document.createElement('header')
      bar.className = 'admin-topbar'
      Object.defineProperty(bar, 'offsetHeight', { value: 125 })
      document.body.appendChild(bar)
      try {
        ;({ wrapper } = await mountDocs('/admin/docs/faq'))
        const style = wrapper.find('.docs').attributes('style')
        expect(style).toContain('--docs-anchor-offset: 125px')
        // The sticky sidebar refers to the admin content area (below the top bar), so it keeps no offset.
        expect(style).not.toContain('--lp-header-height')
      } finally {
        bar.remove()
      }
    })

    it('follows height changes of the admin top bar and stops watching on unmount', async () => {
      const bar = document.createElement('header')
      bar.className = 'admin-topbar'
      let height = 125
      Object.defineProperty(bar, 'offsetHeight', { get: () => height })
      document.body.appendChild(bar)
      const observers = []
      const original = globalThis.ResizeObserver
      globalThis.ResizeObserver = class {
        constructor(cb) { this.cb = cb; this.disconnect = vi.fn(); observers.push(this) }
        observe(el) { this.el = el }
      }
      try {
        ;({ wrapper } = await mountDocs('/admin/docs/faq'))
        expect(observers[0].el).toBe(bar)
        height = 180
        observers[0].cb()
        await flushPromises()
        expect(wrapper.find('.docs').attributes('style')).toContain('--docs-anchor-offset: 180px')
        wrapper.unmount()
        wrapper = undefined
        expect(observers[0].disconnect).toHaveBeenCalled()
      } finally {
        globalThis.ResizeObserver = original
        bar.remove()
      }
    })

    it('keeps links outside the docs unchanged', async () => {
      let router
      ;({ wrapper, router } = await mountDocs('/admin/docs/faq'))
      const md = wrapper.find('.docs-markdown')
      md.element.insertAdjacentHTML('beforeend', '<a id="ext-link" href="/admin/pages">x</a>')
      await md.find('#ext-link').trigger('click')
      await flushPromises()
      expect(router.currentRoute.value.path).toBe('/admin/pages')
    })

    it('maps the docs root link to the admin docs root', async () => {
      let router
      ;({ wrapper, router } = await mountDocs('/admin/docs/faq'))
      const md = wrapper.find('.docs-markdown')
      md.element.insertAdjacentHTML('beforeend', '<a id="docs-root" href="/docs">y</a>')
      await md.find('#docs-root').trigger('click')
      await flushPromises()
      expect(router.currentRoute.value.path).toBe('/admin/docs')
    })

    it('links the not-found page back to the admin docs', async () => {
      ;({ wrapper } = await mountDocs('/admin/docs/gibt-es-nicht'))
      expect(wrapper.find('.docs-not-found a').attributes('href')).toBe('/admin/docs')
    })
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
