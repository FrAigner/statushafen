import { describe, it, expect } from 'vitest'
import { vueSources, templatePart, startTags, i18nKeyOf, lookup } from './templateScan.js'

// Der Scanner hinter den Quelltext-Guards muss selbst stimmen,
// sonst prüfen die Guards still nichts.

describe('templateScan', () => {
  it('liest alle .vue-Dateien mit Pfad ab src/', () => {
    expect(Object.keys(vueSources)).toContain('views/admin/AdminComponents.vue')
  })

  it('schneidet den <template>-Teil aus, leer ohne Template', () => {
    expect(templatePart('<script>x</script><template><a/></template>')).toBe('<template><a/>')
    expect(templatePart('<script>x</script>')).toBe('')
  })

  it('findet Tags auch mit ">" in Attributwerten und liest Attribute', () => {
    const [form, select] = startTags(`<form novalidate class='a'><OnyxSelect :options="xs.map(x => x)" required :label="t('k.l')" />`)

    expect(form.name).toBe('form')
    expect(form.attrs).toEqual({ novalidate: '', class: 'a' })
    expect(select.name).toBe('OnyxSelect')
    expect(select.attrs[':options']).toBe('xs.map(x => x)')
    expect(select.attrs.required).toBe('')
    expect(select.raw.endsWith('/>')).toBe(true)
  })

  it('liest i18n-Schlüssel aus t() und $t()', () => {
    expect(i18nKeyOf("t('a.b')")).toBe('a.b')
    expect(i18nKeyOf('$t("c.d", { x })')).toBe('c.d')
    expect(i18nKeyOf('label')).toBeNull()
    expect(i18nKeyOf(undefined)).toBeNull()
  })

  it('löst Punkt-Pfade auf', () => {
    expect(lookup({ a: { b: 'x' } }, 'a.b')).toBe('x')
    expect(lookup({ a: {} }, 'a.b.c')).toBeUndefined()
  })
})
