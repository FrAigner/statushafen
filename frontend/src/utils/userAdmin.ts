// gemeinsame Helfer der Benutzer- und Seitenrechteverwaltung
// (AdminUsers, AdminStatusPages).

// Rollen für eine Seitenrollen-Auswahl. Rollen mit GlobalOnly (vom Backend in
// GET /api/roles gesetzt, heute "Global Admin") sind keine Seitenrollen und
// fehlen deshalb. Ausnahme: Ist eine solche Rolle auf dieser Seite schon
// vergeben (Altbestand), bleibt sie als Option stehen, sonst zeigte die
// Auswahl einen falschen Wert und das Speichern entzöge stillschweigend Rechte.
export function pageRoleOptions(roles, currentRoleId = 0) {
  return (roles || []).filter(r => !r.GlobalOnly || r.ID === currentRoleId)
}

const KNOWN_ERROR_CODES = [
  'global_admin_role_not_assignable',
  'cannot_delete_self',
  'cannot_demote_self',
  'last_global_admin',
]

// Übersetzt die Fehlercodes der Benutzerverwaltung (Backend: "code" im Body,
// von apiFetch als err.data weitergereicht); sonst die Meldung des Fehlers.
export function userAdminErrorText(err, t) {
  const code = err?.data?.code
  if (KNOWN_ERROR_CODES.includes(code)) return t(`users.errors.${code}`)
  return err?.message || ''
}
