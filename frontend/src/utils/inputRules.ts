// Eingaberegeln, die das Backend (handlers/input_validation.go)
// ebenfalls erzwingt. Das Frontend prüft vorab für schnelle Rückmeldung am
// Feld; der Server bleibt maßgeblich und antwortet mit 400 und `code`.

// Muss mit minPasswordLength im Backend übereinstimmen.
export const MIN_PASSWORD_LENGTH = 8

// Muss mit database.MaxPasswordBytes im Backend übereinstimmen (bcrypt-Grenze, in Bytes).
export const MAX_PASSWORD_BYTES = 72

// BCP47 light, wie languageCodeRegex im Backend (Code wird klein geschrieben).
const LANGUAGE_CODE_RE = /^[a-z]{2,3}(-[a-z0-9]{2,8})*$/

export function isPasswordTooShort(password) {
  return [...(password || '')].length < MIN_PASSWORD_LENGTH
}

export function isPasswordTooLong(password) {
  return new TextEncoder().encode(password || '').length > MAX_PASSWORD_BYTES
}

export function isValidLanguageCode(code) {
  return LANGUAGE_CODE_RE.test((code || '').trim().toLowerCase())
}

const SERVER_INPUT_CODES = ['name_required', 'password_too_short', 'password_too_long', 'language_code_invalid', 'slug_reserved', 'slug_taken']

// Liefert den übersetzten Text zu einem Eingabe-Fehlercode des Backends
// (err.data.code, von apiFetch durchgereicht) oder null bei anderen Fehlern.
export function serverInputErrorText(err, t) {
  const code = err?.data?.code
  if (!SERVER_INPUT_CODES.includes(code)) return null
  return t(`inputErrors.${code}`, { min: MIN_PASSWORD_LENGTH, max: MAX_PASSWORD_BYTES })
}
