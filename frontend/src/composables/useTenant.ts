import { ref, watch, computed } from 'vue'
import type { MyPage } from '../types/api'

/** ID of a status page: a number from the API, a string when restored from localStorage or a route. */
export type TenantId = number | string

const activeTenantId = ref<TenantId | null>(localStorage.getItem('activeTenantId') || null)
// Response of /api/me/pages, including the page-specific permissions per status page.
const myPages = ref<MyPage[]>([])

watch(activeTenantId, (newId) => {
  if (newId) {
    localStorage.setItem('activeTenantId', String(newId))
  } else {
    localStorage.removeItem('activeTenantId')
  }
})

export function useTenant() {
  const setActiveTenant = (id: TenantId | null) => {
    activeTenantId.value = id
  }

  const setMyPages = (pages: MyPage[] | null | undefined) => {
    myPages.value = pages || []
  }

  // The permissions string of the currently active status page (from the
  // page-specific UserStatusPageRole assignment, not the global role).
  const activeTenantPermissions = computed(() => {
    const page = myPages.value.find(p => String(p.ID) === String(activeTenantId.value))
    return page?.Permissions || ''
  })

  // Name of the role the user has on the currently active status page (e.g.
  // "Page Admin"). Important for the UI: the global role in the JWT
  // (currentUser.role) may be e.g. "Viewer" while the user is "Page Admin" on a
  // particular page; the display must show the page-specific role.
  const activeTenantRoleName = computed(() => {
    const page = myPages.value.find(p => String(p.ID) === String(activeTenantId.value))
    return page?.RoleName || ''
  })

  return {
    activeTenantId,
    setActiveTenant,
    myPages,
    setMyPages,
    activeTenantPermissions,
    activeTenantRoleName
  }
}
