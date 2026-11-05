// Einladungs-Token für die Dauer des Tabs (sessionStorage, nie
// localStorage, nie in einer URL). Geteilter Schlüssel, damit InvitationPage
// und das Abmelden (useAuth.logout) dieselbe Stelle löschen. Jeder Zugriff
// ist abgesichert: in privaten Fenstern oder bei gesperrten Cookies wirft der
// Speicher, die Seite funktioniert dann ohne (Neuladen verliert den Token, der
// Mail-Link funktioniert erneut).
export const INVITATION_TOKEN_KEY = 'invitationToken'

export function readInvitationToken() {
  try {
    return sessionStorage.getItem(INVITATION_TOKEN_KEY) || ''
  } catch {
    return ''
  }
}

export function storeInvitationToken(value) {
  try {
    sessionStorage.setItem(INVITATION_TOKEN_KEY, value)
  } catch {
    // blockiert: nichts zu tun
  }
}

export function clearInvitationToken() {
  try {
    sessionStorage.removeItem(INVITATION_TOKEN_KEY)
  } catch {
    // blockiert: nichts zu tun
  }
}
