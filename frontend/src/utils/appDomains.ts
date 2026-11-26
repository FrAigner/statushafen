// Basisdomain(en) der Installation (VITE_APP_BASE_DOMAINS, kommagetrennt).
// Unter <slug>.<Basisdomain> erreicht man die Statusseite eines Mandanten.
// Ohne Konfiguration gibt es keine Subdomain-Konvention: Statusseiten sind dann
// unter /status/<slug> oder über eine eigene Domain erreichbar. Es gibt bewusst
// keinen Standardwert, damit kein Betreibername im Code steht.
// Die Variable wird bei jedem Aufruf gelesen (testbar per vi.stubEnv).
export function appBaseDomains() {
  return (import.meta.env.VITE_APP_BASE_DOMAINS || '')
    .split(',')
    .map(d => d.trim().toLowerCase())
    .filter(Boolean)
}

// Erste Basisdomain oder '' ohne Konfiguration.
export function primaryBaseDomain() {
  return appBaseDomains()[0] || ''
}
