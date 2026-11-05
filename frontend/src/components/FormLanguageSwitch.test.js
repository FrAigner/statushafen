import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref } from 'vue'
import i18n from '../i18n'
import { onyxFormStubs } from '../test-support/onyxStubs'
import FormLanguageSwitch from './FormLanguageSwitch.vue'

// Eine Seite mit 31 Sprachen ließ den Reiter-Umschalter aus dem Formular laufen (Bild vom 31.10.).
// Bis 3 Einträge bleibt es ein Umschalter, darüber eine Auswahlliste.

function fakeTrans(extraCount) {
  const options = [{ value: '', label: 'Deutsch (Standard)' }]
  for (let i = 1; i <= extraCount; i++) options.push({ value: `l${i}`, label: `Sprache ${i} · leer` })
  const active = ref('')
  return {
    active,
    showSwitcher: ref(true),
    langOptions: ref(options),
    onDefaultLang: ref(true),
    translating: ref(false),
    translateError: ref(''),
    translateFallback: ref(false),
    emptyLanguages: ref(options.slice(1)),
    canTranslateAll: ref(true),
    translatingAll: ref(false),
    translateAllProgress: ref(''),
    translateAllSummary: ref(''),
    translateAll: () => {},
    canTranslate: ref(true),
    nameOf: (c) => c,
    defaultCode: ref('de'),
    translate: () => {},
  }
}

function mountSwitch(trans) {
  return mount(FormLanguageSwitch, { props: { trans }, global: { plugins: [i18n], stubs: onyxFormStubs } })
}

describe('FormLanguageSwitch: Umschalter oder Auswahlliste', () => {
  it('zeigt bis 3 Einträge Reiter nebeneinander', () => {
    const w = mountSwitch(fakeTrans(2))
    expect(w.findAll('.segment__item')).toHaveLength(3)
    expect(w.find('[data-test="lang-select"]').exists()).toBe(false)
  })

  it('zeigt ab 4 Einträgen eine Auswahlliste mit allen Sprachen statt überlaufender Reiter', () => {
    const w = mountSwitch(fakeTrans(30))
    expect(w.find('.segment').exists()).toBe(false)
    const options = w.findAll('[data-test="lang-select"] option')
    expect(options).toHaveLength(31)
    expect(options[0].text()).toBe('Deutsch (Standard)')
    expect(options[1].text()).toBe('Sprache 1 · leer')
  })

  it('wechselt die Sprache über die Auswahlliste und zeigt dann die Übersetzen-Hilfe', async () => {
    const trans = fakeTrans(5)
    const w = mountSwitch(trans)
    await w.find('[data-test="lang-select"]').setValue('l3')
    expect(trans.active.value).toBe('l3')
    trans.onDefaultLang.value = false
    await w.vm.$nextTick()
    expect(w.find('[data-test="lang-translate"]').exists()).toBe(true)
  })

  it('verlässt die Formularbreite nicht: .segment ist auf die Containerbreite begrenzt', () => {
    const css = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), '..', 'style.css'), 'utf-8')
    expect(css).toMatch(/\.segment\s*\{[^}]*max-inline-size:\s*100%/)
  })
})
