import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import i18n from '../../i18n'
import de from '../../i18n/locales/de.json'
import en from '../../i18n/locales/en.json'
import LandingPage from './LandingPage.vue'
import landingSource from './LandingPage.vue?raw'
import { axe } from '../../test-support/axe.js'
import { useLandingChrome } from '../../composables/useLandingChrome'

function pagesFixture() {
  return [
    { ID: 1, Name: 'Acme Corp', Slug: 'acme', Description: 'Alles rund um Acme.' },
    { ID: 2, Name: 'Beta', Slug: 'beta', Description: '' },
  ]
}

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

// LandingPage lädt zusätzlich das Flag /api/public/orders/enabled.
// Der Standard-Mock liefert die Seiten und ein aktives Flag; einzelne Tests
// schalten das Flag gezielt ab.
function routedFetch({ pages = pagesFixture(), enabled = true } = {}) {
  return vi.fn(async (url) => {
    if (String(url).includes('/api/public/orders/enabled')) {
      return jsonResponse({ enabled })
    }
    return jsonResponse(pages)
  })
}

function makeRouter() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: LandingPage },
      { path: '/order', component: { template: '<div/>' } },
      { path: '/status/:slug', component: { template: '<div/>' } },
    ],
  })
  router.push('/')
  return router
}

let activeWrapper = null

async function mountLanding(fetchImpl) {
  global.fetch = fetchImpl || routedFetch()
  const router = makeRouter()
  await router.isReady()

  const wrapper = mount(LandingPage, { global: { plugins: [i18n, router] } })
  await flushPromises()
  activeWrapper = wrapper
  return wrapper
}

describe('LandingPage', () => {
  beforeEach(() => {
    i18n.global.locale.value = 'de'
  })

  afterEach(() => {
    activeWrapper?.unmount()
    activeWrapper = null
    vi.restoreAllMocks()
  })

  it('zeigt einen Ladezustand, bevor die öffentlichen Seiten geladen sind', () => {
    const router = makeRouter()
    global.fetch = vi.fn(() => new Promise(() => {}))
    const wrapper = mount(LandingPage, { global: { plugins: [i18n, router] } })
    activeWrapper = wrapper

    expect(wrapper.find('.page-grid').exists()).toBe(false)
  })

  it('rendert eine Karte je öffentlicher Statusseite mit Link zu /status/:slug', async () => {
    const wrapper = await mountLanding()

    const cards = wrapper.findAll('.page-card')
    expect(cards).toHaveLength(2)
    expect(cards[0].text()).toContain('Acme Corp')
    expect(cards[0].text()).toContain('Alles rund um Acme.')
    expect(cards[0].attributes('href')).toBe('/status/acme')
  })

  it('zeigt eine Default-Beschreibung, wenn die Seite keine eigene hat', async () => {
    const wrapper = await mountLanding()

    expect(wrapper.findAll('.page-card')[1].text()).not.toContain('undefined')
  })

  it('zeigt den Directory-Bereich leer, wenn keine öffentlichen Seiten existieren', async () => {
    const wrapper = await mountLanding(vi.fn().mockResolvedValue(jsonResponse([])))

    expect(wrapper.findAll('.page-card')).toHaveLength(0)
    expect(wrapper.find('.page-grid').exists()).toBe(true)
  })

  it('bleibt funktionsfähig, wenn das Laden der öffentlichen Seiten fehlschlägt', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const wrapper = await mountLanding(vi.fn().mockRejectedValue(new Error('Netzwerkfehler')))

    expect(wrapper.find('.page-grid').exists()).toBe(true)
    expect(wrapper.findAll('.page-card')).toHaveLength(0)
  })

  it('verlinkt den primären CTA auf /order', async () => {
    const wrapper = await mountLanding()

    expect(wrapper.find('.lp-cta .btn-primary').attributes('href')).toBe('/order')
  })

  // ist die Self-Service-Bestellung deaktiviert, verschwindet der
  // Order-CTA (der Directory-Link bleibt).
  it('blendet den Order-CTA aus, wenn die Bestellung deaktiviert ist', async () => {
    const wrapper = await mountLanding(routedFetch({ enabled: false }))

    expect(wrapper.find('.btn-primary').exists()).toBe(false)
    expect(wrapper.find('.lp-cta .btn-secondary').exists()).toBe(true)
  })

  // Regressionen vom 2026-10-29 (Startseite „nicht stimmig“).
  it('zeigt keinen Hilfe-Block mit Platzhalter-Adresse', async () => {
    const wrapper = await mountLanding()

    expect(wrapper.find('.support-section').exists()).toBe(false)
    expect(wrapper.html()).not.toContain('statushafen.local')
    expect(wrapper.find('a[href^="mailto:"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('rund um die Uhr')
  })

  it('nennt die Liste „Aktuelle Statusseiten“, nicht Partner oder Kunden', async () => {
    const wrapper = await mountLanding()
    expect(wrapper.find('#directory h2').text()).toBe('Aktuelle Statusseiten')
    expect(wrapper.find('#directory').text()).not.toMatch(/Partner|Kunden/)

    i18n.global.locale.value = 'en'
    await flushPromises()
    expect(wrapper.find('#directory h2').text()).toBe('Current status pages')
    expect(wrapper.find('#directory').text()).not.toMatch(/partners|customers/i)
  })

  // Die Inhaltsbreite der Startseite ist --lp-max (1160 px, Vorlage
  // Richtung A), nicht mehr die 960 px des Standard-Containers. Die Raster müssen
  // in die Inhaltsbreite abzüglich 2 x 24 px Rand passen. happy-dom rechnet kein
  // Layout, deshalb prüft der Test die CSS-Werte.
  it('Raster bieten Platz für drei Funktionskarten bzw. mehrere Statusseiten nebeneinander', () => {
    const inhalt = 1160 - 2 * 24
    expect(readFileSync(resolve(__dirname, '../../assets/tokens.css'), 'utf-8')).toMatch(/--lp-max:\s*1160px/)
    const feats = landingSource.match(/\.feats \{([^}]*)\}/)?.[1] ?? ''
    expect(feats).toMatch(/repeat\(3,\s*minmax\(0,\s*1fr\)\)/)
    const grid = landingSource.match(/\.page-grid \{([^}]*)\}/)?.[1] ?? ''
    const min = Number(grid.match(/minmax\((\d+)px/)?.[1])
    expect(min).toBeGreaterThan(0)
    expect(3 * min + 2 * 16).toBeLessThanOrEqual(inhalt)
  })

  it('Inhalt sitzt zentriert in .lp-in (--lp-max), die Abschnitte selbst laufen randlos', () => {
    const inner = landingSource.match(/\.lp-in \{([^}]*)\}/)?.[1] ?? ''
    expect(inner).toMatch(/max-width:\s*var\(--lp-max\)/)
    expect(inner).toMatch(/margin:\s*0 auto/)
    for (const sel of ['lp-hero', 'lp-sec']) {
      const block = landingSource.match(new RegExp(`\\.${sel} \\{([^}]*)\\}`))?.[1]
      expect(block, sel).toBeTruthy()
      expect(block, sel).toMatch(/padding:\s*var\(--sp-\d\) 0;/)
      expect(block, sel).not.toMatch(/max-width/)
    }
  })

  // Richtung A: Hero zweigeteilt. Gesucht wird über Rollen und
  // Namen statt über Klassen, damit ein Umbau des Markups die Tests nicht bricht.
  describe('Hero (Richtung A)', () => {
    const t = (k, p) => i18n.global.t(k, p)

    it('ist ein benannter Bereich mit der h1 als Name und trägt Eyebrow, Lead und beide CTAs', async () => {
      const wrapper = await mountLanding()
      const hero = wrapper.find('section[aria-labelledby="hero-title"]')
      expect(hero.find('h1#hero-title').text()).toBe(t('landing.heroTitle'))
      expect(hero.find('.eyebrow').text()).toBe(t('landing.heroEyebrow'))
      expect(hero.find('.lp-lead').text()).toBe(t('landing.heroSubtitle'))
      const links = hero.findAll('.lp-cta a').map(a => [a.text(), a.attributes('href')])
      expect(links).toEqual([
        [t('landing.ctaOrder'), '/order'],
        [t('landing.ctaDirectory'), '#directory'],
      ])
      expect(hero.find('.lp-cta a[href="/order"]').classes()).toContain('btn-primary')
      expect(hero.find('.lp-cta a[href="#directory"]').classes()).toContain('btn-secondary')
    })

    it('ist eine einzige h1, der Funktionsabschnitt eine h2 mit sechs Karten als h3', async () => {
      const wrapper = await mountLanding()
      expect(wrapper.findAll('h1')).toHaveLength(1)
      const sec = wrapper.find('section#features')
      expect(sec.find('h2').text()).toBe('Alles für eine gute Störungskommunikation')
      expect(sec.find('.lp-sec-sub').text()).toBe('Ohne Einrichtungsaufwand: Seite bestellen, Komponenten anlegen, fertig.')
      expect(sec.findAll('.feat h3').map(h => h.text())).toEqual([
        'Komponenten & Gruppen', 'Vorfälle mit Verlauf', 'Wartungsfenster',
        'Abonnements', 'Mehrsprachig', 'Mandanten & SSO',
      ])
      expect(sec.findAll('.feat-ic svg')).toHaveLength(6)
    })

    it('Hero-Texte entsprechen der Vorlage (de und en)', async () => {
      const wrapper = await mountLanding()
      const hero = wrapper.find('.lp-hero')
      expect(hero.find('.eyebrow').text()).toBe('Gehostet in Deutschland')
      expect(hero.find('.eyebrow svg.status-icon--operational').exists()).toBe(true)
      expect(hero.find('h1').text()).toBe('Wenn etwas ausfällt, wissen eure Kunden es zuerst von euch.')
      expect(hero.find('.lp-lead').text()).toContain('Vorfälle melden, Wartungen ankündigen und Abonnenten automatisch informieren.')
      expect(hero.find('.btn-primary').text()).toBe('Statusseite bestellen')
      expect(hero.find('.btn-secondary').text()).toBe('Beispiel ansehen')
      i18n.global.locale.value = 'en'
      await flushPromises()
      expect(hero.find('h1').text()).toBe('When something goes down, your customers hear it from you first.')
    })

    // Der Titel brach mitten im Wort („ausfäl-lt“): overflow-wrap/hyphens sind verboten;
    // der Umbruch kommt aus max-width 13ch (längstes Wort kürzer).
    it('trennt den Hero-Titel nie mitten im Wort', () => {
      const title = landingSource.match(/\.lp-title \{([^}]*)\}/)?.[1] ?? ''
      expect(title).not.toMatch(/overflow-wrap|word-break|hyphens/)
      expect(title).toMatch(/max-width:\s*13ch/)
      for (const lang of [de, en]) {
        const longest = Math.max(...lang.landing.heroTitle.split(/\s+/).map(w => w.length))
        expect(longest).toBeLessThanOrEqual(13)
      }
    })

    it('meldet der Kopfzeile die Startseite und räumt beim Verlassen auf', async () => {
      const { isLanding, ordersEnabled } = useLandingChrome()
      const wrapper = await mountLanding(routedFetch({ enabled: false }))
      expect(isLanding.value).toBe(true)
      expect(ordersEnabled.value).toBe(false)
      wrapper.unmount()
      activeWrapper = null
      expect(isLanding.value).toBe(false)
      expect(ordersEnabled.value).toBe(true)
    })

    it('bietet die Anker #features und #directory an (Ziele der Kopfnavigation)', async () => {
      const wrapper = await mountLanding()
      expect(wrapper.find('section#features').exists()).toBe(true)
      expect(wrapper.find('section#directory').exists()).toBe(true)
      expect(landingSource).toMatch(/scroll-margin-top/)
    })

    it('Verzeichniskarten zeigen Logo-Kachel, Adresse und „Statusseite öffnen“', async () => {
      const wrapper = await mountLanding()
      const card = wrapper.findAll('.page-card')[0]
      expect(card.find('.cust-logo').text()).toBe('A')
      expect(card.find('.page-slug').text()).toBe('/status/acme')
      expect(card.find('.page-go').text()).toBe('Statusseite öffnen')
    })

    // Der Slug brach neben dem Live-Abzeichen mitten im Wort um
    // („/status/techstar“ + „t“) und machte die Karte höher. Jetzt eine Zeile mit
    // Auslassung und vollem Text im title; das Abzeichen schrumpft nicht.
    it('Slug der Verzeichniskarte: voller Text als title, eine Zeile mit Auslassung statt Umbruch im Wort', async () => {
      const wrapper = await mountLanding()
      const slug = wrapper.findAll('.page-card')[0].find('.page-slug')
      expect(slug.attributes('title')).toBe('/status/acme')
      const rule = landingSource.match(/\n\.page-slug \{([^}]*)\}/)?.[1] ?? ''
      expect(rule).toMatch(/white-space:\s*nowrap/)
      expect(rule).toMatch(/overflow:\s*hidden/)
      expect(rule).toMatch(/text-overflow:\s*ellipsis/)
      expect(rule).not.toMatch(/overflow-wrap/)
      expect(landingSource.match(/\n\.badge \{([^}]*)\}/)?.[1]).toMatch(/flex-shrink:\s*0/)
      expect(landingSource.match(/\n\.page-id \{([^}]*)\}/)?.[1]).toMatch(/min-width:\s*0/)
    })

    it('der pulsierende Live-Punkt respektiert prefers-reduced-motion', () => {
      expect(landingSource).toMatch(/@media \(prefers-reduced-motion: reduce\)\s*\{\s*\.live-dot \{ animation: none; \}/)
    })

    it('der Teilausfall ist schraffiert (Form + Farbe)', () => {
      expect(landingSource).toMatch(/\.bar--partial \{[^}]*repeating-linear-gradient[^}]*var\(--part\)/)
    })

    // Kein Testhinweis, kein Preisbezug, keine unbelegte Behauptung.
    it('enthält keinen Testhinweis, Preisbezug oder die Aussage „DSGVO-konform“', async () => {
      for (const lang of ['de', 'en']) {
        i18n.global.locale.value = lang
        const wrapper = await mountLanding()
        expect(wrapper.text()).not.toMatch(/14 Tage|kostenlos|Kreditkarte|Preis|Preise|free trial|pricing|DSGVO|GDPR/i)
        wrapper.unmount()
        activeWrapper = null
      }
    })

    it('die Beispielkarte ist reine Zierde: aria-hidden, 30 grobe Segmente je Komponente, Status mit Form', async () => {
      const wrapper = await mountLanding()
      const mock = wrapper.find('.lp-hero-visual')
      expect(mock.attributes('aria-hidden')).toBe('true')
      expect(mock.find('.cust-logo').text()).toBe('T')
      expect(mock.find('strong').text()).toBe('TechStart GmbH')
      expect(mock.find('.live-dot').exists()).toBe(true)
      expect(mock.findAll('.mock-row').map(r => r.find('.mock-name').text())).toEqual(['API', 'Website', 'Zahlungs-Gateway'])
      for (const row of mock.findAll('.mock-row')) {
        expect(row.findAll('.bar')).toHaveLength(30)
        expect(row.find('svg.status-icon').exists()).toBe(true)
      }
      expect(mock.find('.mock-banner').text()).toBe(t('landing.mockBanner'))
      expect(mock.find('.mock-banner svg.status-icon--operational').exists()).toBe(true)
      // Beispieldaten sind fest: je ein Teilausfall bei Website und Zahlungs-Gateway
      const rows = mock.findAll('.mock-row')
      expect(rows[0].findAll('.bar--partial')).toHaveLength(0)
      expect(rows[1].findAll('.bar--partial')).toHaveLength(1)
      expect(rows[2].findAll('.bar--partial')).toHaveLength(1)
    })

    it('ohne Bestellung bleibt nur der Verzeichnis-Link im Hero', async () => {
      const wrapper = await mountLanding(routedFetch({ enabled: false }))
      expect(wrapper.findAll('.lp-cta a')).toHaveLength(1)
      expect(wrapper.find('.lp-cta a').attributes('href')).toBe('#directory')
    })

    it('Farben kommen aus Tokens: keine Hex-/rgb-Werte, kein Inline-Style, keine Richtung-B-Tokens', () => {
      const code = landingSource.replace(/\/\*[\s\S]*?\*\//g, '').replace(/<!--[\s\S]*?-->/g, '').replace(/(^|\s)\/\/.*$/gm, '$1')
      expect(code).not.toMatch(/#[0-9a-fA-F]{3,8}\b/)
      expect(code).not.toMatch(/rgba?\(/)
      expect(code).not.toMatch(/\sstyle=|:style=/)
      expect(code).not.toMatch(/--hero-|--beacon|--on-beacon/)
    })

    // Regression: Die Navy-Hero-Tokens aus Richtung B sind entfernt und dürfen
    // nicht zurückkehren; die Startseite folgt Richtung A.
    it('tokens.css enthält keine Hero-/Beacon-Tokens mehr', () => {
      // `tokens.css?raw` kam in vitest leer an (der Test war wirkungslos);
      // deshalb per readFileSync lesen und sicherstellen, dass die Datei nicht leer ist.
      const tokensSource = readFileSync(resolve(__dirname, '../../assets/tokens.css'), 'utf-8')
      expect(tokensSource).toContain('--primary')
      expect(tokensSource).not.toMatch(/--hero-|--beacon|--on-beacon/)
    })

    it('die Knöpfe sind mindestens 44 px hoch (Zielgröße)', () => {
      const btn = landingSource.match(/\.btn \{([^}]*)\}/)?.[1] ?? ''
      expect(Number(btn.match(/min-height:\s*(\d+)px/)?.[1])).toBeGreaterThanOrEqual(44)
    })

    // Die Beispielkarte ragte rechts über den Seitenrand, weil 1.1fr/1fr den
    // min-content der Karte nicht schrumpfen lässt; minmax(0, …) verhindert das.
    it('lässt die Hero-Spalten schrumpfen (Karte ragt nicht über den Rand)', () => {
      const hero = landingSource.match(/\.lp-hero-in \{([^}]*)\}/)?.[1] ?? ''
      expect(hero).toMatch(/grid-template-columns:\s*minmax\(0,\s*1\.1fr\)\s+minmax\(0,\s*1fr\)/)
    })

    it('zeichnet das Punkteraster der Richtung A im Hero', () => {
      const hero = landingSource.match(/\.lp-hero \{([^}]*)\}/)?.[1] ?? ''
      expect(hero).toMatch(/radial-gradient/)
    })

    it('stapelt auf Mobil einspaltig', () => {
      const media = landingSource.match(/@media \(max-width: 640px\) \{([\s\S]*?)\n\}/)?.[1] ?? ''
      expect(media).toMatch(/\.lp-hero-in \{[^}]*grid-template-columns:\s*1fr/)
      expect(media).toMatch(/\.feats \{[^}]*grid-template-columns:\s*1fr/)
    })
  })

  it('hat keine axe-Funde', async () => {
    const wrapper = await mountLanding()
    expect(await axe(wrapper)).toHaveNoViolations()
  })
})
