import { describe, it, expect, vi } from 'vitest'
import { ref } from 'vue'
import { useSaveAction, useDeleteAction } from './useAdminCrudActions'
import { stubConfirm } from '../test-support/confirm'

// onSuccess bekommt die Antwort (neue ID) und wird abgewartet, bevor neu geladen wird.
describe('useSaveAction', () => {
  it('reicht die Antwort an onSuccess und wartet darauf, bevor die Liste neu geladen wird', async () => {
    const order = []
    const apiFetch = vi.fn(async () => ({ ID: 7 }))
    const isSaving = ref(false)
    const save = useSaveAction({ apiFetch, isSaving, reload: async () => { order.push('reload') }, onError: vi.fn() })
    await save('/x', 'POST', { a: 1 }, async (saved) => { await Promise.resolve(); order.push(`ok:${saved.ID}`) })
    expect(order).toEqual(['ok:7', 'reload'])
    expect(isSaving.value).toBe(false)
  })

  it('meldet einen Fehler aus onSuccess und lädt nicht neu', async () => {
    const onError = vi.fn()
    const reload = vi.fn()
    const save = useSaveAction({ apiFetch: async () => ({}), isSaving: ref(false), reload, onError })
    await save('/x', 'PUT', {}, async () => { throw new Error('Übersetzung kaputt') })
    expect(onError).toHaveBeenCalledOnce()
    expect(reload).not.toHaveBeenCalled()
  })

  it('funktioniert ohne onSuccess', async () => {
    const reload = vi.fn()
    await useSaveAction({ apiFetch: async () => ({}), isSaving: ref(false), reload, onError: vi.fn() })('/x', 'PUT', {})
    expect(reload).toHaveBeenCalledOnce()
  })
})

// onStart räumt die Meldung des vorigen Schritts ab, onDeleted meldet das Löschen.
describe('onStart / onDeleted', () => {
  it('ruft onStart beim Speichern vor der Anfrage auf', async () => {
    const order = []
    const save = useSaveAction({
      apiFetch: async () => { order.push('fetch'); return {} }, isSaving: ref(false), reload: vi.fn(),
      onStart: () => order.push('start'), onError: vi.fn(),
    })
    await save('/x', 'PUT', {})
    expect(order).toEqual(['start', 'fetch'])
  })

  it('ruft beim Löschen onStart, nach Erfolg onDeleted, bei Fehler nur onError', async () => {
    stubConfirm(true)
    const calls = []
    const make = (apiFetch) => useDeleteAction({
      apiFetch, deletingId: ref(null), reload: async () => { calls.push('reload') },
      onStart: () => calls.push('start'), onDeleted: () => calls.push('deleted'), onError: () => calls.push('error'),
    })
    await make(async () => ({}))('/x', 1, 'sicher?')
    expect(calls).toEqual(['start', 'deleted', 'reload'])

    calls.length = 0
    await make(async () => { throw new Error('kaputt') })('/x', 1, 'sicher?')
    expect(calls).toEqual(['start', 'error'])
  })

  it('ruft nichts auf, wenn die Bestätigung abgelehnt wird', async () => {
    stubConfirm(false)
    const onStart = vi.fn()
    await useDeleteAction({ apiFetch: vi.fn(), deletingId: ref(null), reload: vi.fn(), onStart, onError: vi.fn() })('/x', 1, 'sicher?')
    expect(onStart).not.toHaveBeenCalled()
  })
})
