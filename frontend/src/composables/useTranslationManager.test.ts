import { describe, it, expect, vi } from 'vitest'
import { useTranslationManager } from './useTranslationManager'

describe('useTranslationManager', () => {
  it('starts closed with an info alert and empty ids', () => {
    const m = useTranslationManager(vi.fn())
    expect(m.alertMsg.value).toBe('')
    expect(m.alertType.value).toBe('info')
    expect(m.isEditing.value).toBe(false)
    expect(m.formData.value).toEqual({})
    expect(m.isSaving.value).toBe(false)
    expect(m.confirmingId.value).toBeNull()
    expect(m.deletingId.value).toBeNull()
  })

  it('shows an alert with the given type, info by default', () => {
    const m = useTranslationManager(vi.fn())
    m.showAlert('saved', 'success')
    expect([m.alertMsg.value, m.alertType.value]).toEqual(['saved', 'success'])
    m.showAlert('note')
    expect([m.alertMsg.value, m.alertType.value]).toEqual(['note', 'info'])
  })

  it('opens and cancels the form, clearing its data', () => {
    const m = useTranslationManager(vi.fn())
    m.openNew()
    expect(m.isEditing.value).toBe(true)
    m.formData.value = { title: 'x' }
    m.cancelEdit()
    expect(m.isEditing.value).toBe(false)
    expect(m.formData.value).toEqual({})
  })

  it('reloads through the given loader', async () => {
    const load = vi.fn(async () => {})
    const apiFetch = vi.fn()
    const m = useTranslationManager(load)
    await m.reloadTranslations(apiFetch, 7)
    expect(load).toHaveBeenCalledWith(apiFetch, 7)
    expect(m.alertMsg.value).toBe('')
  })

  it('reports a failed reload as an error alert', async () => {
    const m = useTranslationManager(vi.fn(async () => { throw new Error('offline') }))
    await m.reloadTranslations(vi.fn(), 7)
    expect(m.alertMsg.value).toBe('Fehler beim Neuladen: offline')
    expect(m.alertType.value).toBe('error')
  })
})
