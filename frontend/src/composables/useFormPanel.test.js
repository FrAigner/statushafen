import { describe, it, expect } from 'vitest'
import { defineComponent, h, nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { useFormPanel } from './useFormPanel'

// Ein <aside> mit Formular, wie es FormPanel liefert.
function mountPanel() {
  let api
  const Comp = defineComponent({
    setup() {
      api = useFormPanel()
      return () => h('aside', [h('form', { ref: api.formEl }, [
        h('input', { id: 'a', 'aria-invalid': api.errors.value.a ? 'true' : undefined }),
        h('input', { id: 'b', 'aria-invalid': api.errors.value.b ? 'true' : undefined }),
      ])])
    },
  })
  const wrapper = mount(Comp, { attachTo: document.body })
  return { wrapper, api }
}

describe('useFormPanel', () => {
  it('validateRequired meldet gültig, setzt keine Fehler und fokussiert nichts', async () => {
    const { wrapper, api } = mountPanel()
    expect(await api.validateRequired([{ key: 'a', value: 'x', message: 'A fehlt' }, { key: 'b', value: ['1'], message: 'B fehlt' }])).toBe(true)
    expect(api.errors.value).toEqual({})
    wrapper.unmount()
  })

  it('validateRequired setzt Fehler je leerem Feld (Leerraum, null, leere Liste) und fokussiert das erste', async () => {
    const { wrapper, api } = mountPanel()
    const ok = await api.validateRequired([
      { key: 'a', value: '   ', message: 'A fehlt' },
      { key: 'b', value: [], message: 'B fehlt' },
      { key: 'c', value: null, message: 'C fehlt' },
    ])
    expect(ok).toBe(false)
    expect(api.errors.value).toEqual({ a: 'A fehlt', b: 'B fehlt', c: 'C fehlt' })
    await nextTick()
    expect(document.activeElement.id).toBe('a')
    wrapper.unmount()
  })

  it('clearError räumt nur das genannte Feld ab, clearErrors alle', async () => {
    const { wrapper, api } = mountPanel()
    await api.validateRequired([{ key: 'a', value: '', message: 'A' }, { key: 'b', value: '', message: 'B' }])
    api.clearError('a')
    expect(api.errors.value).toEqual({ a: '', b: 'B' })
    api.clearError('zzz') // unbekanntes Feld: nichts passiert
    expect(api.errors.value).toEqual({ a: '', b: 'B' })
    api.clearErrors()
    expect(api.errors.value).toEqual({})
    wrapper.unmount()
  })

  it('focusInvalid fokussiert das Feld mit aria-invalid und bleibt ohne Formular still', async () => {
    const { wrapper, api } = mountPanel()
    api.errors.value = { b: 'B fehlt' }
    await nextTick()
    await api.focusInvalid()
    expect(document.activeElement.id).toBe('b')
    wrapper.unmount()
    await expect(useFormPanel().focusInvalid()).resolves.toBeUndefined()
  })

  it('focusForm fokussiert das erste Feld und kommt ohne Formular klar', async () => {
    const { wrapper, api } = mountPanel()
    await api.focusForm()
    expect(document.activeElement.id).toBe('a')
    wrapper.unmount()

    const lone = useFormPanel()
    await expect(lone.focusForm()).resolves.toBeUndefined()
  })
})
