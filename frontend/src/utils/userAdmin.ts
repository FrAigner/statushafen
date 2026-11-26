// Shared helpers of the user and page permission management
// (AdminUsers, AdminStatusPages).
import type { TranslateFn } from '../types/i18n'

// Roles for a page role selection. Roles with GlobalOnly (set by the backend in
// GET /api/roles, today "Global Admin") are not page roles and are therefore
// missing. Exception: if such a role is already assigned on this page
// (legacy data), it stays as an option; otherwise the selection would show a
// wrong value and saving would silently revoke permissions.
export function pageRoleOptions<T extends { ID: number; GlobalOnly?: boolean }>(roles: T[] | null | undefined, currentRoleId = 0): T[] {
  return (roles || []).filter(r => !r.GlobalOnly || r.ID === currentRoleId)
}

const KNOWN_ERROR_CODES = [
  'global_admin_role_not_assignable',
  'cannot_delete_self',
  'cannot_demote_self',
  'last_global_admin',
]

// Translates the error codes of the user management (backend: "code" in the body,
// passed on by apiFetch as err.data); otherwise the error's message.
export function userAdminErrorText(err: unknown, t: TranslateFn): string {
  const e = err as { data?: { code?: unknown }; message?: string } | null | undefined
  const code = e?.data?.code
  if (KNOWN_ERROR_CODES.includes(code as string)) return t(`users.errors.${code}`)
  return e?.message || ''
}
