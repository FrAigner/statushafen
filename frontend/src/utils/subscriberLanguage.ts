// Mailsprache eines Abonnenten (Abo-Manager und Admin-Formular).
// Leer = Standardsprache der Seite. Die Liste kommt normalisiert als
// [{ code, name, is_default }] (öffentlich aus dem Dashboard, im Admin aus
// /api/pages/:id/languages).

// Normalisiert die Antwort von /api/pages/:id/languages
// ([{ LanguageCode, IsDefault, Language: { Name } }]).
export function fromAdminPageLanguages(rows) {
  if (!Array.isArray(rows)) return []
  return rows
    .filter((r) => r && typeof r.LanguageCode === 'string' && r.LanguageCode !== '')
    .map((r) => ({
      code: r.LanguageCode,
      name: r.Language?.Name || r.LanguageCode,
      is_default: !!r.IsDefault,
      is_active: r.Language?.IsActive !== false,
    }))
}

// Nur im Sprachkatalog aktive Sprachen sind für Abonnenten wählbar (das Backend
// verwirft andere); der Sprachumschalter der Statusseite zeigt weiter alle.
// Fehlt is_active (ältere Antwort), gilt die Sprache als aktiv.
const selectable = (languages) => (Array.isArray(languages) ? languages : []).filter((l) => l.is_active !== false)

// Die Sprachwahl lohnt sich erst ab zwei Sprachen.
export function hasLanguageChoice(languages) {
  return selectable(languages).length > 1
}

// Auswahlpunkte: zuerst "Seitenstandard (Name)" mit dem Wert '', danach die
// übrigen Sprachen nach Name. defaultLabel(name) liefert die übersetzte Beschriftung.
export function subscriberLanguageOptions(languages, defaultLabel) {
  const list = selectable(languages)
  const def = list.find((l) => l.is_default)
  const options = [{ value: '', label: defaultLabel(def ? def.name : '') }]
  list
    .filter((l) => !l.is_default)
    .sort((a, b) => a.name.localeCompare(b.name))
    .forEach((l) => options.push({ value: l.code, label: l.name }))
  return options
}

// Eine gespeicherte Sprache, die der Seitenstandard ist oder bei der Seite nicht
// (mehr) aktiv ist, erscheint als "Seitenstandard" - der Versand verhält sich genauso.
export function selectableSubscriberLanguage(value, languages) {
  const list = selectable(languages)
  const hit = list.find((l) => l.code === value)
  return hit && !hit.is_default ? hit.code : ''
}
