// Schwellen der Fehlerquote auf der Monitoring-Seite (Betreiber-Entscheidung
// 1.11.2026). Die Quote zählt nur Antworten mit Status 5xx; 4xx (404, 409, 422,
// 429, ...) sind normale Client-Fälle und zählen nicht.
export const ERROR_RATE_DEGRADED = 0.05
export const ERROR_RATE_MAJOR = 0.1

// Stufe für StatusBadge: 'major' ab 10 %, 'degraded' ab 5 %, sonst keine Marke.
export const errorLevel = (rate) => (
  rate >= ERROR_RATE_MAJOR ? 'major' : rate >= ERROR_RATE_DEGRADED ? 'degraded' : null
)
