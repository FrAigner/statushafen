// Country flags for language codes (operator request 31.10.2026). A language is not a country: shown is the
// flag of the country most associated with the language (en → United Kingdom, ga → Ireland,
// lb → Luxembourg, rm → Switzerland). Languages without an entry (created by the admin, e.g. a test code) get
// no flag. The flags are emoji made of two regional indicator symbols; on Windows some browsers show the
// country code instead of the flag, which is purely a display effect.
const COUNTRY_OF_LANGUAGE: Record<string, string> = {
  bg: 'BG', bs: 'BA', cs: 'CZ', da: 'DK', de: 'DE', el: 'GR', en: 'GB', es: 'ES', et: 'EE', fi: 'FI',
  fr: 'FR', ga: 'IE', hr: 'HR', hu: 'HU', it: 'IT', lb: 'LU', lt: 'LT', lv: 'LV', mk: 'MK', mt: 'MT',
  nl: 'NL', pl: 'PL', pt: 'PT', rm: 'CH', ro: 'RO', sk: 'SK', sl: 'SI', sr: 'RS', sv: 'SE', tr: 'TR',
}

const REGIONAL_A = 0x1f1e6 // regional indicator symbol "A"

// Flag emoji for a language code ('' if unknown). Subtags like "en-us" use the base language.
export function languageFlag(code: string | null | undefined): string {
  const country = COUNTRY_OF_LANGUAGE[String(code ?? '').toLowerCase().split('-')[0] as string]
  if (!country) return ''
  return String.fromCodePoint(...[...country].map(c => REGIONAL_A + c.charCodeAt(0) - 65))
}

// Name of the language in the UI language ("Bulgarisch" / "Bulgarian"); without browser support
// or for unknown codes, the code itself.
export function languageDisplayName(code: string, locale: string): string {
  try {
    return new Intl.DisplayNames([locale], { type: 'language' }).of(code) || code
  } catch {
    return code
  }
}
