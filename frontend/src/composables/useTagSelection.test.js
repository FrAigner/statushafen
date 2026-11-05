import { describe, it, expect, vi, afterEach } from 'vitest'
import { ref } from 'vue'
import { useTagToggle, tagNames, useAvailableTags } from './useTagSelection'
import { useAuth } from './useAuth'

describe('useTagToggle', () => {
  it('legt TagIDs an und fügt eine neue ID hinzu', () => {
    const formData = ref({})
    const toggle = useTagToggle(formData)

    toggle(5)

    expect(formData.value.TagIDs).toEqual([5])
  })

  it('entfernt eine bereits ausgewählte ID erneut', () => {
    const formData = ref({ TagIDs: [1, 5, 9] })
    const toggle = useTagToggle(formData)

    toggle(5)

    expect(formData.value.TagIDs).toEqual([1, 9])
  })

  it('fügt eine weitere ID zu bestehenden hinzu', () => {
    const formData = ref({ TagIDs: [1] })
    const toggle = useTagToggle(formData)

    toggle(2)

    expect(formData.value.TagIDs).toEqual([1, 2])
  })
})

describe('tagNames', () => {
  it('joint die Namen einer Tag-Liste', () => {
    expect(tagNames([{ Name: 'EU' }, { Name: 'US' }])).toBe('EU, US')
  })

  it('liefert "—" für eine leere/fehlende Liste', () => {
    expect(tagNames([])).toBe('—')
    expect(tagNames(undefined)).toBe('—')
  })
})

// Global Admins laden über /api/tags, Seiten-Rollen über
// /api/pages/:id/tags und bekommen nur seiteneigene Tags angeboten.
describe('useAvailableTags', () => {
  afterEach(() => useAuth().logout())
  const pageId = ref('3')

  it('lädt für Seiten-Rollen den Seiten-Endpunkt und bietet nur seiteneigene Tags an', async () => {
    useAuth().setAuth('jwt', { permissions: '' })
    const apiFetch = vi.fn().mockResolvedValue([
      { ID: 1, Name: 'global', StatusPageID: null },
      { ID: 2, Name: 'eigen', StatusPageID: 3 },
    ])
    const { availableTags, canSelectTags, loadAvailableTags } = useAvailableTags(apiFetch, pageId)
    expect(canSelectTags.value).toBe(false)
    await loadAvailableTags()
    expect(apiFetch).toHaveBeenCalledTimes(1)
    expect(apiFetch).toHaveBeenCalledWith('/api/pages/3/tags')
    expect(availableTags.value).toEqual([{ ID: 2, Name: 'eigen', StatusPageID: 3 }])
    expect(canSelectTags.value).toBe(true)
  })

  it('blendet das Feld für Seiten-Rollen aus, wenn es nur globale Tags gibt', async () => {
    useAuth().setAuth('jwt', { permissions: '' })
    const apiFetch = vi.fn().mockResolvedValue([{ ID: 1, Name: 'global', StatusPageID: null }])
    const { availableTags, canSelectTags, loadAvailableTags } = useAvailableTags(apiFetch, pageId)
    await loadAvailableTags()
    expect(availableTags.value).toEqual([])
    expect(canSelectTags.value).toBe(false)
  })

  it('behandelt eine leere Antwort und einen Fehler des Seiten-Endpunkts als keine Tags', async () => {
    useAuth().setAuth('jwt', { permissions: '' })
    const empty = useAvailableTags(vi.fn().mockResolvedValue(null), pageId)
    await empty.loadAvailableTags()
    expect(empty.availableTags.value).toEqual([])
    const failing = useAvailableTags(vi.fn().mockRejectedValue(new Error('403')), pageId)
    failing.availableTags.value = [{ ID: 9, StatusPageID: 3 }]
    await failing.loadAvailableTags()
    expect(failing.availableTags.value).toEqual([])
    expect(failing.canSelectTags.value).toBe(false)
  })

  it('zeigt Global Admins das Feld auch ohne vorhandene Tags', async () => {
    useAuth().setAuth('jwt', { is_global_admin: true })
    const { canSelectTags } = useAvailableTags(vi.fn(), pageId)
    expect(canSelectTags.value).toBe(true)
  })

  it('lädt für Global Admins die Tags der Seite', async () => {
    useAuth().setAuth('jwt', { is_global_admin: true })
    const apiFetch = vi.fn().mockResolvedValue([{ ID: 1, Name: 'EU' }])
    const { availableTags, canSelectTags, loadAvailableTags } = useAvailableTags(apiFetch, pageId)
    await loadAvailableTags()
    expect(apiFetch).toHaveBeenCalledWith('/api/tags?status_page_id=3')
    expect(availableTags.value).toEqual([{ ID: 1, Name: 'EU' }])
    expect(canSelectTags.value).toBe(true)
  })

  it('macht aus einer leeren Antwort eine leere Liste', async () => {
    useAuth().setAuth('jwt', { is_global_admin: true })
    const { availableTags, loadAvailableTags } = useAvailableTags(vi.fn().mockResolvedValue(null), pageId)
    availableTags.value = [{ ID: 9 }]
    await loadAvailableTags()
    expect(availableTags.value).toEqual([])
  })

  it('setzt die Liste bei einem Ladefehler zurück', async () => {
    useAuth().setAuth('jwt', { is_global_admin: true })
    const { availableTags, loadAvailableTags } = useAvailableTags(vi.fn().mockRejectedValue(new Error('403')), pageId)
    availableTags.value = [{ ID: 9 }]
    await loadAvailableTags()
    expect(availableTags.value).toEqual([])
  })
})
