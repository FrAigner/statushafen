// Komponentenseite nach Richtung C: „Geändert“-Spalte relativ
// („vor 3 Tagen“) statt als Zeitstempel. Intl.RelativeTimeFormat liefert den
// Wortlaut je Sprache; hier steht nur die Wahl der Einheit.

const MINUTE = 60
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR
const MONTH = 30 * DAY
const YEAR = 365 * DAY

// Einheit und Teiler je Größenordnung der Differenz (Sekunden), von klein nach groß.
const STEPS = [
  { below: MINUTE, unit: 'second', size: 1 },
  { below: HOUR, unit: 'minute', size: MINUTE },
  { below: DAY, unit: 'hour', size: HOUR },
  { below: MONTH, unit: 'day', size: DAY },
  { below: YEAR, unit: 'month', size: MONTH },
  { below: Infinity, unit: 'year', size: YEAR },
]

// Ungültiges oder fehlendes Datum ergibt '' (die Zelle bleibt leer),
// Zukunft (Uhrenabweichung) wird wie „gerade eben“ behandelt.
export function formatRelative(value, locale, now = Date.now()) {
  const time = new Date(value).getTime()
  if (!value || Number.isNaN(time)) return ''
  const seconds = Math.max(0, Math.round((now - time) / 1000))
  const step = STEPS.find(s => seconds < s.below)
  const formatter = new Intl.RelativeTimeFormat(locale, { numeric: 'auto', style: 'long' })
  return formatter.format(-Math.floor(seconds / step.size), step.unit)
}
