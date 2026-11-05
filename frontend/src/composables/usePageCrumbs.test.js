import { describe, it, expect, beforeEach } from 'vitest'
import { ref } from 'vue'
import i18n from '../i18n'
import { usePageCrumbs } from './usePageCrumbs'
import { useTenant } from './useTenant'

// useI18n braucht eine Komponente; für das Composable genügt ein Setup-Aufruf.
import { mount } from '@vue/test-utils'
function run(...args) {
  let result
  mount({ template: '<div />', setup() { result = usePageCrumbs(...args); return {} } }, { global: { plugins: [i18n] } })
  return result
}

describe('usePageCrumbs', () => {
  beforeEach(() => {
    i18n.global.locale.value = 'de'
    useTenant().setActiveTenant('2')
    useTenant().setMyPages([{ ID: 2, Name: 'Acme' }])
  })

  it('beginnt seitenbezogene Seiten mit dem Namen der aktiven Statusseite (Link zur Übersicht)', () => {
    expect(run('Komponenten').value).toEqual([{ label: 'Acme', to: '/admin' }, { label: 'Komponenten' }])
  })

  it('beginnt globale Seiten mit „Administration“, auch wenn global eine Ref ist', () => {
    expect(run('Audit-Log', { global: true }).value[0]).toEqual({ label: 'Administration', to: '/admin' })
    const scope = ref(true)
    const crumbs = run('UI-Texte', { global: scope })
    expect(crumbs.value[0].label).toBe('Administration')
    scope.value = false
    expect(crumbs.value[0].label).toBe('Acme')
  })

  it('lässt die erste Krume weg, wenn der Seitenname unbekannt ist', () => {
    useTenant().setMyPages([])
    expect(run('Komponenten').value).toEqual([{ label: 'Komponenten' }])
  })

  it('stellt den Namen ohne Seitenliste nicht voran und findet die aktive Seite bei Zahl- wie Text-ID', () => {
    useTenant().setMyPages([{ ID: 3, Name: 'Hafen' }, { ID: 4, Name: 'Anderswo' }])
    useTenant().setActiveTenant('3')
    expect(run('Tags').value).toEqual([{ label: 'Hafen', to: '/admin' }, { label: 'Tags' }])
  })

  // Regression (nach einer Zusammenführung): ein Getter als Titel wurde als Funktion
  // statt als Text ausgegeben; ein statisches `extra` folgte dem Sprachwechsel nicht.
  it('löst Getter für Titel und extra auf', () => {
    const lang = ref('Vorfälle')
    const crumbs = run(() => `#${lang.value}`, { extra: () => [{ label: lang.value, to: '/admin/incidents' }] })
    expect(crumbs.value).toEqual([{ label: 'Acme', to: '/admin' }, { label: 'Vorfälle', to: '/admin/incidents' }, { label: '#Vorfälle' }])
    lang.value = 'Incidents'
    expect(crumbs.value.map(c => c.label)).toEqual(['Acme', 'Incidents', '#Incidents'])
  })

  it('hängt weitere Krumen vor die aktuelle Seite und folgt einer Titel-Ref', () => {
    const title = ref('Übersetzungen')
    const crumbs = run(title, { extra: [{ label: 'Komponenten', to: '/admin/components' }] })
    expect(crumbs.value.map(c => c.label)).toEqual(['Acme', 'Komponenten', 'Übersetzungen'])
    title.value = 'Translations'
    expect(crumbs.value.at(-1)).toEqual({ label: 'Translations' })
  })
})
