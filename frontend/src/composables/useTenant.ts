import { ref, watch, computed } from 'vue'

const activeTenantId = ref(localStorage.getItem('activeTenantId') || null)
// Antwort von /api/me/pages, inkl. der seitenbezogenen Permissions je Statusseite.
const myPages = ref([])

watch(activeTenantId, (newId) => {
  if (newId) {
    localStorage.setItem('activeTenantId', newId)
  } else {
    localStorage.removeItem('activeTenantId')
  }
})

export function useTenant() {
  const setActiveTenant = (id) => {
    activeTenantId.value = id
  }

  const setMyPages = (pages) => {
    myPages.value = pages || []
  }

  // Die Permissions-Zeichenkette der gerade aktiven Statusseite (aus der
  // seitenbezogenen UserStatusPageRole-Zuweisung, nicht der globalen Rolle).
  const activeTenantPermissions = computed(() => {
    const page = myPages.value.find(p => String(p.ID) === String(activeTenantId.value))
    return page?.Permissions || ''
  })

  // Name der Rolle, die der Nutzer auf der gerade aktiven Statusseite hat (z.B.
  // "Page Admin"). Wichtig fürs UI: die globale Rolle im JWT (currentUser.role)
  // kann z.B. "Viewer" sein, während der Nutzer auf einer bestimmten Seite
  // "Page Admin" ist – die Anzeige muss die seitenbezogene Rolle zeigen.
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
