import { useAuth } from '../composables/useAuth'
import { useTenant } from '../composables/useTenant'

// hasPerm wertet wie das Backend nur die Rolle auf der aktiven
// Statusseite aus. Tests, die Rechte über `permissions` vorgeben, melden den
// Nutzer deshalb mit genau diesen Rechten als Seitenrolle der aktiven Seite
// an (Standard: Seite 1). Global Admins brauchen keine Seitenrolle.
export function setAuthAsPageRole(token, user = {}) {
  const { setAuth } = useAuth()
  const { activeTenantId, setActiveTenant, myPages, setMyPages } = useTenant()
  setAuth(token, user)
  const id = activeTenantId.value || 1
  setActiveTenant(id)
  const Permissions = user.permissions || ''
  // Bereits gesetzte Seitennamen bleiben erhalten (Brotkrumen-Tests).
  const known = myPages.value.find(p => String(p.ID) === String(id))
  setMyPages([{ ID: Number(id), ...known, Permissions }])
}
