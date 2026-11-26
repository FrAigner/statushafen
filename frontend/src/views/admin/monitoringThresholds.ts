// Error-rate thresholds on the monitoring page (operator decision of
// 2026-11-01). The rate counts only responses with status 5xx; 4xx (404, 409, 422,
// 429, ...) are normal client cases and do not count.
export const ERROR_RATE_DEGRADED = 0.05
export const ERROR_RATE_MAJOR = 0.1

// Level for StatusBadge: 'major' from 10 %, 'degraded' from 5 %, otherwise no badge.
export const errorLevel = (rate: number): 'major' | 'degraded' | null => (
  rate >= ERROR_RATE_MAJOR ? 'major' : rate >= ERROR_RATE_DEGRADED ? 'degraded' : null
)
