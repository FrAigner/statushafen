import { useAuth } from '../composables/useAuth'
import { useTenant } from '../composables/useTenant'
import type { AuthUser, MyPage } from '../types/api'

// Like the backend, hasPerm only evaluates the role on the active
// status page. Tests that specify permissions via `permissions` therefore sign
// the user in with exactly these permissions as the page role of the active page
// (default: page 1). Global admins need no page role.
export function setAuthAsPageRole(token: string, user: AuthUser = {}): void {
  const { setAuth } = useAuth()
  const { activeTenantId, setActiveTenant, myPages, setMyPages } = useTenant()
  setAuth(token, user)
  const id = activeTenantId.value || 1
  setActiveTenant(id)
  const Permissions = user.permissions || ''
  // Page names already set are kept (breadcrumb tests).
  const known = myPages.value.find(p => String(p.ID) === String(id))
  // Tests only need ID and Permissions; the remaining page fields stay as they were (or absent).
  setMyPages([{ ID: Number(id), ...known, Permissions } as MyPage])
}
