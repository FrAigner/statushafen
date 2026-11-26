// Invitation token for the lifetime of the tab (sessionStorage, never
// localStorage, never in a URL). Shared key, so that InvitationPage and
// logout (useAuth.logout) clear the same place. Every access is guarded:
// in private windows or with blocked cookies the storage throws, and the
// page then works without it (a reload loses the token, the mail link
// works again).
export const INVITATION_TOKEN_KEY = 'invitationToken'

export function readInvitationToken(): string {
  try {
    return sessionStorage.getItem(INVITATION_TOKEN_KEY) || ''
  } catch {
    return ''
  }
}

export function storeInvitationToken(value: string): void {
  try {
    sessionStorage.setItem(INVITATION_TOKEN_KEY, value)
  } catch {
    // blocked: nothing to do
  }
}

export function clearInvitationToken(): void {
  try {
    sessionStorage.removeItem(INVITATION_TOKEN_KEY)
  } catch {
    // blocked: nothing to do
  }
}
