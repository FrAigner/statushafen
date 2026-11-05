import { describe, it, expect } from 'vitest'
import { MIN_PASSWORD_LENGTH, MAX_PASSWORD_BYTES, isPasswordTooLong, isPasswordTooShort, isValidLanguageCode, serverInputErrorText } from './inputRules'

// gleiche Regeln wie handlers/input_validation.go.
describe('inputRules', () => {
  it('Mindestlänge 8 zählt Zeichen, nicht Bytes', () => {
    expect(MIN_PASSWORD_LENGTH).toBe(8)
    expect(isPasswordTooShort('1234567')).toBe(true)
    expect(isPasswordTooShort('12345678')).toBe(false)
    expect(isPasswordTooShort('äöüäöüä')).toBe(true)
    expect(isPasswordTooShort('äöüäöüäö')).toBe(false)
    expect(isPasswordTooShort('')).toBe(true)
    expect(isPasswordTooShort(undefined)).toBe(true)
  })

  it('Höchstlänge 72 zählt Bytes (bcrypt-Grenze)', () => {
    expect(MAX_PASSWORD_BYTES).toBe(72)
    expect(isPasswordTooLong('a'.repeat(72))).toBe(false)
    expect(isPasswordTooLong('a'.repeat(73))).toBe(true)
    expect(isPasswordTooLong('ä'.repeat(36))).toBe(false)
    expect(isPasswordTooLong('ä'.repeat(37))).toBe(true)
    expect(isPasswordTooLong(undefined)).toBe(false)
  })

  it('Sprachcode: BCP47 light, Groß-/Kleinschreibung egal, Leerraum wird gekürzt', () => {
    for (const ok of ['de', 'fil', 'en-us', 'PT-br', 'zh-hant-tw', ' fr ']) expect(isValidLanguageCode(ok)).toBe(true)
    for (const bad of ['', 'x', 'x1', 'abcd', 'de_de', 'de-', 'de-x', 'en-us-abcdefghi', 'de de', undefined]) {
      expect(isValidLanguageCode(bad)).toBe(false)
    }
  })

  it('übersetzt nur bekannte Eingabe-Fehlercodes', () => {
    const t = (key, params) => `${key}|${params.min}|${params.max}`
    expect(serverInputErrorText({ data: { code: 'password_too_short' } }, t)).toBe('inputErrors.password_too_short|8|72')
    for (const code of ['name_required', 'password_too_long', 'language_code_invalid', 'slug_reserved', 'slug_taken']) {
      expect(serverInputErrorText({ data: { code } }, t)).toBe(`inputErrors.${code}|8|72`)
    }
    expect(serverInputErrorText({ data: { code: 'tag_name_taken' } }, t)).toBeNull()
    expect(serverInputErrorText({ message: 'x' }, t)).toBeNull()
    expect(serverInputErrorText(null, t)).toBeNull()
  })
})
