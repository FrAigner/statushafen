import { describe, it, expect, vi, afterEach } from 'vitest'
import i18n from '../i18n'
import { helpDomId, helpTextFor } from './useHelpText'

// Hilfetexte per Schlüssel, nichts bei unvollständigem Eintrag.

describe('helpTextFor', () => {
  afterEach(() => {
    vi.unstubAllEnvs()
    vi.restoreAllMocks()
    i18n.global.locale.value = 'de'
  })

  it('liefert what/effect/example in der aktiven Sprache', () => {
    i18n.global.locale.value = 'de'
    const de = helpTextFor('components.name')
    expect(Object.keys(de)).toEqual(['what', 'effect', 'example'])
    expect(de.what).toContain('Anzeigename')

    i18n.global.locale.value = 'en'
    expect(helpTextFor('components.name').what).not.toBe(de.what)
  })

  it('liefert nichts und warnt, wenn ein Teil fehlt', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    i18n.global.mergeLocaleMessage('de', { help: { probe472: { teil: { what: 'W', effect: 'E' } } } })

    expect(helpTextFor('probe472.teil')).toBeNull()
    expect(warn).toHaveBeenCalledWith('[v-help] Hilfetext fehlt: help.probe472.teil.{example}')
  })

  it('warnt außerhalb von DEV nicht', () => {
    vi.stubEnv('DEV', false)
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})

    expect(helpTextFor('components.gibtEsNicht')).toBeNull()
    expect(warn).not.toHaveBeenCalled()
  })

  it('bildet stabile, gültige DOM-IDs', () => {
    expect(helpDomId('components.name')).toBe('help-components-name')
    expect(helpDomId('a b/c')).toBe('help-a-b-c')
  })
})
