// Landesflaggen zu Sprachcodes (Betreiber-Wunsch 31.10.2026). Eine Sprache ist kein Land: gezeigt wird die
// Flagge des Landes, das man mit der Sprache am ehesten verbindet (en → Vereinigtes Königreich, ga → Irland,
// lb → Luxemburg, rm → Schweiz). Sprachen ohne Eintrag (vom Admin angelegte, z. B. ein Testcode) bekommen
// keine Flagge. Die Flaggen sind Emoji aus zwei Regionalzeichen; unter Windows zeigen manche Browser statt
// der Flagge den Ländercode, das ist ein reiner Anzeigeeffekt.
const COUNTRY_OF_LANGUAGE = {
  bg: 'BG', bs: 'BA', cs: 'CZ', da: 'DK', de: 'DE', el: 'GR', en: 'GB', es: 'ES', et: 'EE', fi: 'FI',
  fr: 'FR', ga: 'IE', hr: 'HR', hu: 'HU', it: 'IT', lb: 'LU', lt: 'LT', lv: 'LV', mk: 'MK', mt: 'MT',
  nl: 'NL', pl: 'PL', pt: 'PT', rm: 'CH', ro: 'RO', sk: 'SK', sl: 'SI', sr: 'RS', sv: 'SE', tr: 'TR',
}

const REGIONAL_A = 0x1f1e6 // Regionalzeichen „A“

// Flaggen-Emoji für einen Sprachcode ('' wenn unbekannt). Untertags wie „en-us“ nutzen die Basissprache.
export function languageFlag(code) {
  const country = COUNTRY_OF_LANGUAGE[String(code ?? '').toLowerCase().split('-')[0]]
  if (!country) return ''
  return String.fromCodePoint(...[...country].map(c => REGIONAL_A + c.charCodeAt(0) - 65))
}

// Name der Sprache in der Oberflächensprache („Bulgarisch“ / „Bulgarian“); ohne Browser-Unterstützung
// oder für unbekannte Codes der Code selbst.
export function languageDisplayName(code, locale) {
  try {
    return new Intl.DisplayNames([locale], { type: 'language' }).of(code) || code
  } catch {
    return code
  }
}
