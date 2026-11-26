// Components page following direction C: the "Changed" column is relative
// ("3 days ago") instead of a timestamp. Intl.RelativeTimeFormat supplies the
// wording per language; only the choice of unit lives here.

const MINUTE = 60
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR
const MONTH = 30 * DAY
const YEAR = 365 * DAY

// Unit and divisor per magnitude of the difference (seconds), from small to large.
const STEPS: { below: number; unit: Intl.RelativeTimeFormatUnit; size: number }[] = [
  { below: MINUTE, unit: 'second', size: 1 },
  { below: HOUR, unit: 'minute', size: MINUTE },
  { below: DAY, unit: 'hour', size: HOUR },
  { below: MONTH, unit: 'day', size: DAY },
  { below: YEAR, unit: 'month', size: MONTH },
  { below: Infinity, unit: 'year', size: YEAR },
]

// An invalid or missing date yields '' (the cell stays empty);
// the future (clock skew) is treated like "just now".
export function formatRelative(value: string | number | Date | null | undefined, locale: string | undefined, now = Date.now()): string {
  const time = new Date(value as string).getTime()
  if (!value || Number.isNaN(time)) return ''
  const seconds = Math.max(0, Math.round((now - time) / 1000))
  // The last step (below: Infinity) always matches.
  const step = STEPS.find(s => seconds < s.below) as (typeof STEPS)[number]
  const formatter = new Intl.RelativeTimeFormat(locale, { numeric: 'auto', style: 'long' })
  return formatter.format(-Math.floor(seconds / step.size), step.unit)
}
