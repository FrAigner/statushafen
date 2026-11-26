// Input rules that the backend (handlers/input_validation.go) enforces
// as well. The frontend checks up front for quick feedback at the field;
// the server stays authoritative and answers with 400 and `code`.
import type { TranslateFn } from '../types/i18n'

// Must match minPasswordLength in the backend.
export const MIN_PASSWORD_LENGTH = 8

// Must match database.MaxPasswordBytes in the backend (bcrypt limit, in bytes).
export const MAX_PASSWORD_BYTES = 72

// BCP47 light, like languageCodeRegex in the backend (the code is lowercased).
const LANGUAGE_CODE_RE = /^[a-z]{2,3}(-[a-z0-9]{2,8})*$/

export function isPasswordTooShort(password: string | null | undefined): boolean {
  return [...(password || '')].length < MIN_PASSWORD_LENGTH
}

export function isPasswordTooLong(password: string | null | undefined): boolean {
  return new TextEncoder().encode(password || '').length > MAX_PASSWORD_BYTES
}

export function isValidLanguageCode(code: string | null | undefined): boolean {
  return LANGUAGE_CODE_RE.test((code || '').trim().toLowerCase())
}

const SERVER_INPUT_CODES = ['name_required', 'password_too_short', 'password_too_long', 'language_code_invalid', 'slug_reserved', 'slug_taken']

// Returns the translated text for a backend input error code
// (err.data.code, passed through by apiFetch) or null for other errors.
export function serverInputErrorText(err: unknown, t: TranslateFn): string | null {
  const code = (err as { data?: { code?: unknown } } | null | undefined)?.data?.code
  if (!SERVER_INPUT_CODES.includes(code as string)) return null
  return t(`inputErrors.${code}`, { min: MIN_PASSWORD_LENGTH, max: MAX_PASSWORD_BYTES })
}
