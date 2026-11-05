import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import i18n from '../i18n'
import de from '../i18n/locales/de.json'
import en from '../i18n/locales/en.json'
import { useAuth } from '../composables/useAuth'
import { useTenant } from '../composables/useTenant'
import { HELP_PARTS } from '../composables/useHelpText'
import { onyxFormStubs } from '../test-support/onyxStubs'
import { vueSources, lookup } from '../test-support/templateScan.js'
import AdminComponents from './admin/AdminComponents.vue'
import AdminExternalSources from './admin/AdminExternalSources.vue'

// Abdeckungs-Test für Hilfetexte (Was / Auswirkung / Beispiel).
//
// Für jede Seite in HELP_COMPLETE_VIEWS gilt:
//   1. Im gemounteten DOM hat JEDES Formularfeld (input, select, textarea)
//      und JEDER Button eine Hilfe-Zuordnung: aria-describedby zeigt auf ein
//      role="tooltip"-Element der Tooltip-Ebene (ID "help-…"). v-help setzt
//      das, solange die Hilfe eingeschaltet ist (Default). Bei Feldgruppen
//      (z. B. Tag-Checkboxen) darf das aria-describedby an der umgebenden
//      Gruppe (role="group") sitzen.
//      Ausgenommen sind Elemente mit data-help-exempt="<Begründung>" (z. B.
//      das ✕ einer Meldung).
//   2. Jeder im Quelltext verwendete Hilfe-Schlüssel (v-help="'…'") existiert
//      in de UND en mit what, effect und example.
//   3. Es gibt keine ⓘ-Hilfesymbole mehr (Rückmeldung zum ersten Pilot: zu
//      viele Symbole auf der Seite).
// Zusätzlich muss der ganze help-Katalog in beiden Sprachen vollständig sein.
//
// Geprüft wird der gemountete DOM statt des Quelltexts, weil nur dort
// sichtbar ist, was wirklich als Feld/Button ankommt (v-if-Zustände,
// Unterkomponenten wie TagCheckboxList). Deshalb fährt jeder Eintrag seine
// Seite durch alle Zustände (Liste, Neu, Bearbeiten …).
//
// Seite hinzufügen (Stufe 2):
//   1. In der View `import { vHelp } from '../../directives/vHelp'` und an
//      jedes Feld bzw. jeden Button v-help="'bereich.feld'" schreiben (Muster:
//      AdminComponents.vue). Bei sit-onyx-Komponenten direkt an OnyxInput,
//      OnyxSelect, OnyxButton …; die Direktive beschreibt das Feld darin.
//      Feldgruppen: v-help an das Element mit role="group". Funktionen, die
//      sich in jeder Zeile wiederholen, teilen sich einen Schlüssel.
//   2. Texte unter help.<bereich>.<feld>.{what,effect,example} in de.json und
//      en.json eintragen, aus dem tatsächlichen Verhalten (Handler, docs/).
//   3. Hier einen Eintrag in HELP_COMPLETE_VIEWS ergänzen: file (Pfad ab
//      src/) und states, eine async Funktion, die die View mountet, durch
//      ihre Zustände klickt und für jeden Zustand `await check(label)` ruft.

const wrappers = []

function jsonResponse(body) {
  return { ok: true, status: 200, json: async () => body }
}

async function mountAdminView(component, routes) {
  useAuth().setAuth('jwt-1', { permissions: '*', is_global_admin: true })
  useTenant().setActiveTenant('1')
  global.fetch = vi.fn(async (url) => {
    const hit = Object.entries(routes).find(([pattern]) => new RegExp(pattern).test(url))
    return jsonResponse(hit ? hit[1] : [])
  })
  const wrapper = mount(component, { attachTo: document.body, global: { plugins: [i18n], stubs: onyxFormStubs } })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}

export const HELP_COMPLETE_VIEWS = [
  {
    file: 'views/admin/AdminComponents.vue',
    async states(check) {
      const wrapper = await mountAdminView(AdminComponents, {
        '/components$': [{ ID: 1, Name: 'API', Description: 'Kern-API', Status: 'operational', Tags: [] }],
        '/api/tags': [{ ID: 5, Name: 'EU' }],
      })
      await check('Liste')
      await wrapper.find('[role="menuitem"]').trigger('click')
      await check('Bearbeiten')
      await wrapper.findAll('button').find(b => b.text() === i18n.global.t('common.cancel')).trigger('click')
      await wrapper.findAll('button').find(b => b.text().includes(i18n.global.t('components.newButton'))).trigger('click')
      await check('Neu')
    },
  },
  {
    file: 'views/admin/AdminExternalSources.vue',
    async states(check) {
      const wrapper = await mountAdminView(AdminExternalSources, {
        '/external-sources/1/components$': { groups: [], components: [{ ID: 7, external_id: 'vm', name: 'VM', group_name: 'Compute', is_group: false, linked_component_ids: [] }] },
        '/external-sources$': [{ ID: 1, id: 1, name: 'Example Cloud', base_url: 'https://status.example.com', active: true, poll_interval_seconds: 120, outage_after_minutes: 60, component_count: 3, link_count: 1, outage: { active: false } }],
        '/external-links$': [{ id: 4, component_id: 1, component_name: 'API', external_component_id: 7, external_component_name: 'VM', source_id: 1, source_name: 'Example Cloud', status: 'operational', warnings: [] }],
        '/components$': [{ ID: 1, Name: 'API', Status: 'operational' }],
        '/api/tags': [{ ID: 5, Name: 'EU' }],
      })
      await check('Liste')
      await wrapper.findAll('[role="menuitem"]').find(i => i.text().includes(i18n.global.t('common.edit'))).trigger('click')
      await check('Bearbeiten')
      await wrapper.findAll('button').find(b => b.text() === i18n.global.t('common.cancel')).trigger('click')
      await wrapper.findAll('button').find(b => b.text().includes(i18n.global.t('externalSources.newButton'))).trigger('click')
      await check('Neu')
      await wrapper.find('select[id]').setValue('1')
      await flushPromises()
      await check('Bausteine übernehmen: Quelle gewählt')
      await wrapper.find('.ext-import__row input').setValue(true)
      await wrapper.find('.ext-import .tag-chips__add')?.setValue('5')
      await check('Bausteine übernehmen: Auswahl')
    },
  },
]

function describedByHelp(el) {
  const ids = (el.getAttribute('aria-describedby') || '').split(/\s+/).filter(id => id.startsWith('help-'))
  return ids.some(id => document.getElementById(id)?.getAttribute('role') === 'tooltip')
}

// Liefert alle Felder/Buttons im Zustand, denen ein Hilfetext fehlt.
export function missingHelp(root) {
  const missing = []
  for (const el of root.querySelectorAll('input:not([type="hidden"]), select, textarea')) {
    const owner = describedByHelp(el) ? el : el.closest('[role="group"][aria-describedby]')
    if (!owner || !describedByHelp(owner)) missing.push(el.outerHTML.slice(0, 120))
  }
  for (const el of root.querySelectorAll('button')) {
    if (el.matches('[data-help-exempt]')) continue
    if (!describedByHelp(el)) missing.push(el.outerHTML.slice(0, 120))
  }
  return missing
}

export function helpKeysIn(src) {
  return [...new Set([...src.matchAll(/v-help="'([\w.-]+)'"/g)].map(m => m[1]))]
}

// ⓘ-Hilfesymbole des ersten Pilots (HelpTip): Trigger-Button bzw. „Hilfe: …“.
export function helpIcons(root) {
  return [...root.querySelectorAll('.help-tip, .help-tip__trigger, [aria-label^="Hilfe:"], [aria-label^="Help:"]')]
}

function incompleteEntries(key) {
  const problems = []
  for (const [locale, messages] of Object.entries({ de, en })) {
    for (const part of HELP_PARTS) {
      const text = lookup(messages, `help.${key}.${part}`)
      if (typeof text !== 'string' || !text.trim()) problems.push(`${locale}: help.${key}.${part}`)
    }
  }
  return problems
}

describe('Hilfetexte vollständig', () => {
  beforeEach(() => {
    i18n.global.locale.value = 'de'
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  afterEach(() => {
    wrappers.splice(0).forEach(w => w.unmount())
    vi.restoreAllMocks()
    useAuth().logout()
  })

  for (const view of HELP_COMPLETE_VIEWS) {
    it(`${view.file}: jedes Feld und jeder Button hat einen Hilfetext`, async () => {
      const results = {}
      await view.states(async (label) => {
        await flushPromises()
        const root = wrappers.at(-1).element
        const controls = root.querySelectorAll('input, select, textarea, button')
        expect(controls.length, `${label}: keine Felder/Buttons gefunden`).toBeGreaterThan(0)
        results[label] = missingHelp(root)
        expect(helpIcons(document.body), `${label}: ⓘ-Hilfesymbol gerendert`).toEqual([])
      })
      expect(Object.keys(results).length).toBeGreaterThan(0)
      expect(results).toEqual(Object.fromEntries(Object.keys(results).map(k => [k, []])))
    })

    it(`${view.file}: alle verwendeten help-Schlüssel gibt es in de und en mit what/effect/example`, () => {
      const keys = helpKeysIn(vueSources[view.file])
      expect(keys.length).toBeGreaterThan(0)
      expect(keys.flatMap(incompleteEntries)).toEqual([])
    })
  }

  it('der help-Katalog ist in de und en vollständig', () => {
    // parts sind die Überschriften des Tooltips selbst, kein Bereich.
    const fieldKeys = (help) => Object.entries(help)
      .filter(([area]) => area !== 'parts')
      .flatMap(([area, fields]) => Object.keys(fields).map(f => `${area}.${f}`))
      .sort()
    const keys = fieldKeys(de.help)
    const enKeys = fieldKeys(en.help)
    expect(keys.length).toBeGreaterThan(0)
    expect(enKeys).toEqual(keys)
    expect(keys.flatMap(incompleteEntries)).toEqual([])
  })

  it('missingHelp erkennt fehlende Hilfe an Feld und Button (Gegenprobe)', () => {
    document.body.insertAdjacentHTML('beforeend', `
      <div id="probe">
        <input aria-describedby="help-x">
        <button aria-describedby="nope">Speichern</button>
        <button data-help-exempt="weil">x</button>
        <div role="group" aria-describedby="help-y"><input type="checkbox"></div>
        <span id="help-y" role="tooltip"></span>
      </div>`)
    const probe = document.getElementById('probe')
    const missing = missingHelp(probe)
    probe.remove()

    expect(missing).toHaveLength(2)
    expect(missing[0]).toContain('help-x')
    expect(missing[1]).toContain('Speichern')
  })

  it('helpKeysIn findet v-help-Schlüssel, helpIcons alte ⓘ-Symbole (Gegenprobe)', () => {
    expect(helpKeysIn(`<OnyxInput v-help="'a.b'" /><button v-help="'c.d-e'">x</button><i v-help="'a.b'" />`)).toEqual(['a.b', 'c.d-e'])
    expect(helpKeysIn('<HelpTip help-key="a.b" />')).toEqual([])

    document.body.insertAdjacentHTML('beforeend', `
      <div id="icons"><span class="help-tip"><button class="help-tip__trigger" aria-label="Hilfe: Name">i</button></span></div>`)
    const probe = document.getElementById('icons')
    expect(helpIcons(probe)).toHaveLength(2)
    probe.remove()
  })

  it('AdminComponents ohne Hilfe (ausgeschaltet) fällt im Abdeckungs-Check durch (Gegenprobe)', async () => {
    const view = HELP_COMPLETE_VIEWS.find(v => v.file === 'views/admin/AdminComponents.vue')
    const missing = []
    await view.states(async () => {
      useAuth().currentUser.value = { ...useAuth().currentUser.value, help_tooltips: false }
      await flushPromises()
      missing.push(...missingHelp(wrappers.at(-1).element))
    })
    expect(missing.length).toBeGreaterThan(5)
  })
})
