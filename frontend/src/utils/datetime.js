// Formatiert einen ISO-Zeitstempel für <input type="datetime-local">
// (lokale Zeitzone, kein "Z"/Offset-Suffix wie bei Date#toISOString).
export function toLocalInputValue(iso) {
  if (!iso) return ''
  const d = new Date(iso)
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

// Bildet den 2-Buchstaben-Code aus i18n.global.locale (siehe
// ONYX_LOCALE_MAP in main.js) auf ein BCP-47-Tag für Intl/toLocaleString ab.
// Unbekannte/fehlende Locale fällt auf Deutsch zurück (bisheriges Verhalten).
const INTL_LOCALES = { de: 'de-DE', en: 'en-US' }

export function toIntlLocale(locale) {
  return INTL_LOCALES[locale] ?? 'de-DE'
}

export function formatDateTimeShort(d, locale) {
  return d ? new Date(d).toLocaleString(toIntlLocale(locale), { dateStyle: 'short', timeStyle: 'short' }) : '—'
}
