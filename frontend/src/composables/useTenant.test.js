import { describe, it, expect, beforeEach } from 'vitest'
import { nextTick } from 'vue'
import { useTenant } from './useTenant'

describe('useTenant', () => {
  beforeEach(() => {
    localStorage.clear()
    useTenant().setActiveTenant(null)
    useTenant().setMyPages([])
  })

  it('persistiert die aktive Tenant-ID im localStorage', async () => {
    const { setActiveTenant } = useTenant()

    setActiveTenant('42')
    await nextTick()

    expect(localStorage.getItem('activeTenantId')).toBe('42')
  })

  it('entfernt den localStorage-Eintrag, wenn die aktive Tenant-ID gelöscht wird', async () => {
    const { setActiveTenant } = useTenant()
    setActiveTenant('42')
    await nextTick()

    setActiveTenant(null)
    await nextTick()

    expect(localStorage.getItem('activeTenantId')).toBeNull()
  })

  it('liefert die Permissions/Rolle der aktiven Statusseite', () => {
    const { setActiveTenant, setMyPages, activeTenantPermissions, activeTenantRoleName } = useTenant()
    setMyPages([
      { ID: 1, Permissions: 'view', RoleName: 'Viewer' },
      { ID: 2, Permissions: 'view,edit', RoleName: 'Page Admin' },
    ])

    setActiveTenant('2')

    expect(activeTenantPermissions.value).toBe('view,edit')
    expect(activeTenantRoleName.value).toBe('Page Admin')
  })

  it('liefert leere Werte, wenn keine passende Seite gefunden wird', () => {
    const { setActiveTenant, setMyPages, activeTenantPermissions, activeTenantRoleName } = useTenant()
    setMyPages([{ ID: 1, Permissions: 'view', RoleName: 'Viewer' }])

    setActiveTenant('999')

    expect(activeTenantPermissions.value).toBe('')
    expect(activeTenantRoleName.value).toBe('')
  })

  it('normalisiert myPages auf ein leeres Array bei null/undefined', () => {
    const { setMyPages, myPages } = useTenant()

    setMyPages(undefined)

    expect(myPages.value).toEqual([])
  })
})
