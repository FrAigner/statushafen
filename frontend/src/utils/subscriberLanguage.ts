// Mail language of a subscriber (subscription manager and admin form).
// Empty = default language of the page. The list arrives normalized as
// [{ code, name, is_default }] (publicly from the dashboard, in the admin from
// /api/pages/:id/languages).

export interface SubscriberLanguage {
  code: string
  name: string
  is_default?: boolean
  is_active?: boolean
}

export interface SubscriberLanguageOption {
  value: string
  label: string
}

/** One row of /api/pages/:id/languages (models.PageLanguage with the preloaded language). */
export interface AdminPageLanguageRow {
  LanguageCode: string
  IsDefault?: boolean
  Language?: { Name?: string; IsActive?: boolean } | null
}

// Normalizes the response of /api/pages/:id/languages
// ([{ LanguageCode, IsDefault, Language: { Name } }]).
export function fromAdminPageLanguages(rows: unknown): SubscriberLanguage[] {
  if (!Array.isArray(rows)) return []
  return (rows as (AdminPageLanguageRow | null | undefined)[])
    .filter((r): r is AdminPageLanguageRow => !!r && typeof r.LanguageCode === 'string' && r.LanguageCode !== '')
    .map((r) => ({
      code: r.LanguageCode,
      name: r.Language?.Name || r.LanguageCode,
      is_default: !!r.IsDefault,
      is_active: r.Language?.IsActive !== false,
    }))
}

// Only languages active in the language catalogue can be chosen by subscribers (the backend
// rejects others); the status page's language switcher still shows all of them.
// If is_active is missing (older response), the language counts as active.
const selectable = (languages: unknown): SubscriberLanguage[] => (Array.isArray(languages) ? languages as SubscriberLanguage[] : []).filter((l) => l.is_active !== false)

// A language choice only makes sense from two languages on.
export function hasLanguageChoice(languages: unknown): boolean {
  return selectable(languages).length > 1
}

// Options: first "page default (name)" with the value '', then the
// remaining languages by name. defaultLabel(name) returns the translated label.
export function subscriberLanguageOptions(languages: unknown, defaultLabel: (name: string) => string): SubscriberLanguageOption[] {
  const list = selectable(languages)
  const def = list.find((l) => l.is_default)
  const options: SubscriberLanguageOption[] = [{ value: '', label: defaultLabel(def ? def.name : '') }]
  list
    .filter((l) => !l.is_default)
    .sort((a, b) => a.name.localeCompare(b.name))
    .forEach((l) => options.push({ value: l.code, label: l.name }))
  return options
}

// A stored language that is the page default or is not (or no longer) active
// on the page appears as "page default" - delivery behaves the same way.
export function selectableSubscriberLanguage(value: string | null | undefined, languages: unknown): string {
  const list = selectable(languages)
  const hit = list.find((l) => l.code === value)
  return hit && !hit.is_default ? hit.code : ''
}
