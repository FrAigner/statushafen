// 4a: presentation of the 90-day availability
// (GET /api/public/pages/{slug}/availability, docs/api/openapi.yaml).
// Pure functions, so that the mapping is tested and not spread across
// templates.

export type AvailabilityLevel = 'ok' | 'deg' | 'part' | 'major' | 'maint' | 'none'

export interface AvailabilityDayMeta {
  level: AvailabilityLevel
  labelKey: string
}

/** One day of the availability response. */
export interface AvailabilityDay {
  date: string
  status: string
  [key: string]: unknown
}

/** Response of GET /api/public/pages/{slug}/availability (only the part the view relies on). */
export interface AvailabilityResponse {
  days: AvailabilityDay[]
  [key: string]: unknown
}

export interface DurationParts {
  key: string
  params: Record<string, number>
}

// Daily status of the backend -> level/text. `no_data` (days before the page was
// created) is deliberately neutral grey and claims neither operation nor outage;
// unknown values are treated the same way.
const DAY_META: Readonly<Record<string, AvailabilityDayMeta>> = Object.freeze({
  operational: { level: 'ok', labelKey: 'status.operational' },
  degraded_performance: { level: 'deg', labelKey: 'status.degradedPerformance' },
  // Severity in the history (backend: impact of the external report or weighting).
  partial_outage: { level: 'part', labelKey: 'status.partialOutage' },
  major_outage: { level: 'major', labelKey: 'status.majorOutage' },
  under_maintenance: { level: 'maint', labelKey: 'status.maintenance' },
})
const NO_DATA: AvailabilityDayMeta = Object.freeze({ level: 'none', labelKey: 'availability.noData' })

export const availabilityDayMeta = (status: string | null | undefined): AvailabilityDayMeta =>
  (Object.hasOwn(DAY_META, status as PropertyKey) ? DAY_META[status as string] as AvailabilityDayMeta : NO_DATA)

// Percentage for display. null (no data) yields null, so that "0 %" or
// "100 %" is never invented. It truncates instead of rounding: 99.996 %
// must not appear as "100.00 %" if there was downtime.
// One formatter per language: a page draws 90 days per component, and a
// new Intl formatter per call cost seconds in PageLayoutList (review finding 4).
const numberFormats = new Map<string | undefined, Intl.NumberFormat>()
const numberFormat = (locale: string | undefined): Intl.NumberFormat => {
  if (!numberFormats.has(locale)) {
    numberFormats.set(locale, new Intl.NumberFormat(locale, { minimumFractionDigits: 2, maximumFractionDigits: 2 }))
  }
  return numberFormats.get(locale) as Intl.NumberFormat
}

export const formatAvailability = (value: unknown, locale: string | undefined): string | null => {
  if (typeof value !== 'number' || Number.isNaN(value)) return null
  const truncated = Math.floor(value * 100) / 100
  return numberFormat(locale).format(truncated) + ' %'
}

// UTC day ("2026-10-29") as a localized date, also with one formatter
// per language. Noon UTC, so that the browser's time zone does not shift the day.
const dayFormats = new Map<string | undefined, Intl.DateTimeFormat>()
export const formatDay = (isoDate: string, locale: string | undefined): string => {
  if (!dayFormats.has(locale)) {
    dayFormats.set(locale, new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeZone: 'UTC' }))
  }
  return (dayFormats.get(locale) as Intl.DateTimeFormat).format(new Date(`${isoDate}T12:00:00Z`))
}

// Checks the response far enough that the view can draw it safely.
// Everything else counts as "no availability data" (bars are omitted).
export const parseAvailability = (res: unknown): AvailabilityResponse | null => (res && Array.isArray((res as AvailabilityResponse).days) && (res as AvailabilityResponse).days.length > 0 ? res as AvailabilityResponse : null)

// Duration for the tooltip as key + parameters for vue-i18n: "2 h 5 min",
// "45 min", below one minute "< 1 min". Negative or invalid values count as 0.
export const durationParts = (seconds: unknown): DurationParts => {
  const total = typeof seconds === 'number' && seconds > 0 ? Math.floor(seconds) : 0
  const minutes = Math.floor(total / 60)
  if (minutes < 1) return { key: 'availability.durationLessThanMinute', params: {} }
  if (minutes < 60) return { key: 'availability.durationMinutes', params: { minutes } }
  return { key: 'availability.durationHoursMinutes', params: { hours: Math.floor(minutes / 60), minutes: minutes % 60 } }
}
