import { describe, it, expect } from 'vitest'
import de from './locales/de.json'
import en from './locales/en.json'
import { vueSources, templatePart, startTags, i18nKeyOf, lookup } from '../test-support/templateScan'

// The screenshot showed "Name * *". Cause: the i18n label already carried
// the required asterisk in the text ("Name *"), and sit-onyx appends its own
// marker to `required` fields (.onyx-required-marker::after,
// content "*"). Result: two asterisks. 23 labels in seven views were
// affected (incidents, maintenance, notices, templates, subscribers, …).
//
// Rule from now on: only the presentation sets the asterisk, never the
// text. For sit-onyx fields `required` does that. Native fields with their own
// <label for> get the class `onyx-required-marker` on the label, so that the
// asterisk looks the same and comes from the same source.

const LOCALES = { de, en }

function requiredOnyxLabels() {
  const found = []
  for (const [file, src] of Object.entries(vueSources)) {
    for (const tag of startTags(templatePart(src))) {
      // FormField (stage 3) also sets the asterisk itself.
      if (!(tag.name.startsWith('Onyx') || tag.name === 'FormField') || !('required' in tag.attrs)) continue
      const key = i18nKeyOf(tag.attrs[':label'])
      if (key) found.push({ file, key })
    }
  }
  return found
}

// Native pairs <label for="x">{{ t('k') }}</label> + <input id="x" required>.
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

  // The maintenance time fields used to carry the asterisk in the text. Since stage 4b
  // FormField sets it; they must still be recognized as required
  // fields, otherwise the marking gets lost. Native required fields without
  // any marker.
  it('die Wartungs-Zeitfelder sind als required-FormField gekennzeichnet', () => {
    const keys = requiredOnyxLabels().map(f => f.key)
    expect(keys).toContain('maintenance.fieldStart')
    expect(keys).toContain('maintenance.fieldEnd')
  })

  // Remainder: login, ordering, setting a password and deleting the account
  // use native fields. Every native required field with <label for> carries the
  // marker class (asterisk from the presentation) and aria-required on the field.
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
