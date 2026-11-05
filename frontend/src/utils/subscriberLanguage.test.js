import { describe, it, expect } from 'vitest'
import {
  fromAdminPageLanguages,
  hasLanguageChoice,
  subscriberLanguageOptions,
  selectableSubscriberLanguage,
} from './subscriberLanguage'

const langs = [
  { code: 'en', name: 'English', is_default: false },
  { code: 'de', name: 'Deutsch', is_default: true },
  { code: 'fr', name: 'Français', is_default: false },
]

describe('subscriberLanguage', () => {
  it('normalisiert die Admin-Antwort und lässt Müll weg', () => {
    expect(fromAdminPageLanguages([
      { LanguageCode: 'de', IsDefault: true, Language: { Name: 'Deutsch' } },
      { LanguageCode: 'en', IsDefault: false, Language: {} },
      { LanguageCode: '' },
      null,
    ])).toEqual([
      { code: 'de', name: 'Deutsch', is_default: true, is_active: true },
      { code: 'en', name: 'en', is_default: false, is_active: true },
    ])
    expect(fromAdminPageLanguages(null)).toEqual([])
    expect(fromAdminPageLanguages('x')).toEqual([])
  })

  it('übernimmt is_active aus dem Katalog (fehlend = aktiv)', () => {
    const [a, b] = fromAdminPageLanguages([
      { LanguageCode: 'hr', IsDefault: false, Language: { Name: 'Hrvatski', IsActive: false } },
      { LanguageCode: 'cs', IsDefault: false, Language: { Name: 'Čeština', IsActive: true } },
    ])
    expect(a.is_active).toBe(false)
    expect(b.is_active).toBe(true)
  })

  it('bietet im Katalog deaktivierte Sprachen weder zur Wahl an noch zählt sie dafür', () => {
    const withInactive = [...langs, { code: 'hr', name: 'Hrvatski', is_default: false, is_active: false }]
    expect(subscriberLanguageOptions(withInactive, () => 'S').map(o => o.value)).toEqual(['', 'en', 'fr'])
    expect(hasLanguageChoice([langs[1], { code: 'hr', name: 'H', is_default: false, is_active: false }])).toBe(false)
    expect(selectableSubscriberLanguage('hr', withInactive)).toBe('')
    // is_active fehlt (ältere Antwort): gilt als aktiv
    expect(subscriberLanguageOptions(langs, () => 'S')).toHaveLength(3)
  })

  it('bietet die Wahl erst ab zwei Sprachen an', () => {
    expect(hasLanguageChoice(langs)).toBe(true)
    expect(hasLanguageChoice([langs[0]])).toBe(false)
    expect(hasLanguageChoice([])).toBe(false)
    expect(hasLanguageChoice(undefined)).toBe(false)
  })

  it('setzt "Seitenstandard" zuerst, danach die übrigen nach Name', () => {
    const opts = subscriberLanguageOptions(langs, (n) => `Standard (${n})`)
    expect(opts).toEqual([
      { value: '', label: 'Standard (Deutsch)' },
      { value: 'en', label: 'English' },
      { value: 'fr', label: 'Français' },
    ])
    // ohne Standard in der Liste: Beschriftung ohne Namen, keine Ausnahme
    expect(subscriberLanguageOptions([], (n) => `S(${n})`)).toEqual([{ value: '', label: 'S()' }])
    expect(subscriberLanguageOptions(undefined, (n) => `S(${n})`)).toEqual([{ value: '', label: 'S()' }])
  })

  it('mutiert die übergebene Liste nicht', () => {
    const copy = JSON.parse(JSON.stringify(langs))
    subscriberLanguageOptions(langs, () => '')
    expect(langs).toEqual(copy)
  })

  it('zeigt Standard- und nicht aktive Sprachen als "Seitenstandard"', () => {
    expect(selectableSubscriberLanguage('en', langs)).toBe('en')
    expect(selectableSubscriberLanguage('de', langs)).toBe('')
    expect(selectableSubscriberLanguage('xx', langs)).toBe('')
    expect(selectableSubscriberLanguage('', langs)).toBe('')
    expect(selectableSubscriberLanguage('en', undefined)).toBe('')
  })
})
