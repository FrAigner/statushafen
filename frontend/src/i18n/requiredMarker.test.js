import { describe, it, expect } from 'vitest'
import de from './locales/de.json'
import en from './locales/en.json'
import { vueSources, templatePart, startTags, i18nKeyOf, lookup } from '../test-support/templateScan.js'

// Im Screenshot stand „Name * *“. Ursache: Das i18n-Label trug
// den Pflichtstern schon im Text („Name *“), und sit-onyx hängt an
// `required`-Felder seinen eigenen Marker (.onyx-required-marker::after,
// content "*") an. Ergebnis: zwei Sterne. Betroffen waren 23 Labels in
// sieben Views (Vorfälle, Wartung, Ankündigungen, Vorlagen, Abonnenten, …).
//
// Regel ab jetzt: Den Stern setzt ausschließlich die Darstellung, nie der
// Text. Für sit-onyx-Felder macht das `required`. Native Felder mit eigenem
// <label for> bekommen am Label die Klasse `onyx-required-marker`, damit der
// Stern gleich aussieht und aus derselben Quelle kommt.

const LOCALES = { de, en }

function requiredOnyxLabels() {
  const found = []
  for (const [file, src] of Object.entries(vueSources)) {
    for (const tag of startTags(templatePart(src))) {
      // FormField (Stufe 3) setzt den Stern ebenfalls selbst.
      if (!(tag.name.startsWith('Onyx') || tag.name === 'FormField') || !('required' in tag.attrs)) continue
      const key = i18nKeyOf(tag.attrs[':label'])
      if (key) found.push({ file, key })
    }
  }
  return found
}

// Native Paare <label for="x">{{ t('k') }}</label> + <input id="x" required>.
function requiredNativeLabels() {
  const found = []
  for (const [file, src] of Object.entries(vueSources)) {
    const template = templatePart(src)
    const tags = startTags(template)
    const requiredIds = new Set(tags.filter(t => 'required' in t.attrs && t.attrs.id).map(t => t.attrs.id))
    for (const tag of tags) {
      if (tag.name !== 'label' || !requiredIds.has(tag.attrs.for)) continue
      const body = template.slice(tag.end, template.indexOf('</label>', tag.end))
      found.push({ file, key: i18nKeyOf(body), classes: (tag.attrs.class || '').split(/\s+/) })
    }
  }
  return found
}

describe('Pflichtstern', () => {
  it('findet die bekannten Pflichtfelder (Scanner greift)', () => {
    const keys = requiredOnyxLabels().map(f => f.key)
    expect(keys).toContain('components.fieldName')
    expect(keys).toContain('incidents.fieldTitle')
  })

  it('kein Label eines required-Onyx-Felds endet mit "*" (sonst doppelter Stern)', () => {
    const offenders = []
    for (const { file, key } of [...requiredOnyxLabels(), ...requiredNativeLabels()]) {
      for (const [locale, messages] of Object.entries(LOCALES)) {
        const text = lookup(messages, key)
        if (typeof text === 'string' && text.trim().endsWith('*')) offenders.push(`${file}: ${locale}:${key} = "${text}"`)
      }
    }
    expect(offenders).toEqual([])
  })

  // Die Wartungs-Zeitfelder trugen den Stern bisher im Text. Seit Stufe 4b
  // setzt FormField ihn; sie müssen als Pflichtfelder erkannt
  // bleiben, sonst geht die Kennzeichnung verloren. Native Pflichtfelder ohne
  // jeden Marker.
  it('die Wartungs-Zeitfelder sind als required-FormField gekennzeichnet', () => {
    const keys = requiredOnyxLabels().map(f => f.key)
    expect(keys).toContain('maintenance.fieldStart')
    expect(keys).toContain('maintenance.fieldEnd')
  })

  // Rest: Login, Bestellung, Passwort setzen und Konto löschen
  // nutzen native Felder. Jedes native Pflichtfeld mit <label for> trägt die
  // Marker-Klasse (Stern aus der Darstellung) und aria-required am Feld.
  it('native Pflichtfelder haben Marker-Klasse am Label und aria-required am Feld', () => {
    const offenders = []
    for (const [file, src] of Object.entries(vueSources)) {
      const tags = startTags(templatePart(src))
      const required = tags.filter(t => 'required' in t.attrs && t.attrs.id && t.name === 'input')
      for (const input of required) {
        const label = tags.find(t => t.name === 'label' && t.attrs.for === input.attrs.id)
        if (!label) continue
        if (!(label.attrs.class || '').split(/\s+/).includes('onyx-required-marker')) offenders.push(`${file}: label ${input.attrs.id} ohne Marker`)
        if (input.attrs['aria-required'] !== 'true') offenders.push(`${file}: ${input.attrs.id} ohne aria-required`)
      }
    }
    expect(offenders).toEqual([])
  })

  it('die vier Einstiegs-/Konto-Dateien werden vom Scanner erfasst', () => {
    const files = new Set(requiredNativeLabels().map(f => f.file))
    for (const f of ['views/auth/LoginView.vue', 'views/public/OrderPage.vue', 'views/public/OrderSetPasswordPage.vue', 'components/AccountDeletion.vue']) {
      expect(files.has(f)).toBe(true)
    }
  })
})
