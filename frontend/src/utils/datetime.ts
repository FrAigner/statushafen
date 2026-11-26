// Formats an ISO timestamp for <input type="datetime-local">
// (local time zone, no "Z"/offset suffix as with Date#toISOString).
export function toLocalInputValue(iso: string | number | Date | null | undefined): string {
  if (!iso) return ''
  const d = new Date(iso)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

// Maps the 2-letter code from i18n.global.locale (see
// ONYX_LOCALE_MAP in main.ts) to a BCP 47 tag for Intl/toLocaleString.
// An unknown/missing locale falls back to German (previous behaviour).
const INTL_LOCALES: Record<string, string> = { de: 'de-DE', en: 'en-US' }

export function toIntlLocale(locale: string | null | undefined): string {
  return INTL_LOCALES[locale as string] ?? 'de-DE'
}

export function formatDateTimeShort(d: string | number | Date | null | undefined, locale: string | null | undefined): string {
  return d ? new Date(d).toLocaleString(toIntlLocale(locale), { dateStyle: 'short', timeStyle: 'short' }) : '—'
}
