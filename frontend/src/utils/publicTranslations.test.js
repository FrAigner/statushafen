import { describe, it, expect, vi } from 'vitest'
import {
  parsePublicTranslations, fetchPublicTranslations, fetchPublicUpdateTranslations, localizeUpdates, localizeTree, localizeIncidents, localizeNotices,
} from './publicTranslations'

// Anwenden und Abrufen der gebündelten Statusseiten-Übersetzungen.

const valid = { language: 'en', components: { 1: { title: 'A', text: 'a' } }, incidents: {}, notices: {} }

describe('parsePublicTranslations', () => {
  it('übernimmt eine gültige Antwort', () => {
    expect(parsePublicTranslations(valid)).toEqual({ language: 'en', components: valid.components, incidents: {}, notices: {} })
  })
  it('setzt eine fehlende Sprache auf leer', () => {
    expect(parsePublicTranslations({ components: {}, incidents: {}, notices: {} }).language).toBe('')
  })
  it.each([
    ['null', null],
    ['Text', 'x'],
    ['Liste', []],
    ['Dashboard-Form (Komponenten als Liste)', { components: [], incidents: [], notices: [] }],
    ['ohne notices', { components: {}, incidents: {} }],
    ['notices null', { components: {}, incidents: {}, notices: null }],
    ['incidents fehlt', { components: {}, notices: {} }],
  ])('verwirft %s', (_n, res) => {
    expect(parsePublicTranslations(res)).toBeNull()
  })
})

describe('fetchPublicTranslations', () => {
  it('baut die URL mit Sprache und Token', async () => {
    const api = vi.fn().mockResolvedValue(valid)
    await fetchPublicTranslations(api, 'acme', 'en', 'tok 1')
    expect(api).toHaveBeenCalledWith('/api/public/pages/acme/translations?lang=en&token=tok%201', { expectedStatuses: [400] })
  })
  it('lässt ohne Sprache und Token die Query weg', async () => {
    const api = vi.fn().mockResolvedValue(valid)
    await fetchPublicTranslations(api, 'acme', '', undefined)
    expect(api).toHaveBeenCalledWith('/api/public/pages/acme/translations', expect.anything())
  })
  it('nur Token ohne Sprache', async () => {
    const api = vi.fn().mockResolvedValue(valid)
    await fetchPublicTranslations(api, 'acme', '', 't')
    expect(api).toHaveBeenCalledWith('/api/public/pages/acme/translations?token=t', expect.anything())
  })
  it('wiederholt bei 400 ohne Sprache, behält den Token', async () => {
    const api = vi.fn().mockRejectedValueOnce(Object.assign(new Error('x'), { status: 400 })).mockResolvedValueOnce(valid)
    expect((await fetchPublicTranslations(api, 'acme', 'es', 't')).language).toBe('en')
    expect(api.mock.calls.map(c => c[0])).toEqual([
      '/api/public/pages/acme/translations?lang=es&token=t',
      '/api/public/pages/acme/translations?token=t',
    ])
  })
  it('gibt null, wenn auch der zweite Abruf scheitert', async () => {
    const api = vi.fn().mockRejectedValue(Object.assign(new Error('x'), { status: 400 }))
    expect(await fetchPublicTranslations(api, 'acme', 'es')).toBeNull()
    expect(api).toHaveBeenCalledTimes(2)
  })
  it.each([[500], [401], [403], [404], [undefined]])('gibt bei Status %s null ohne zweiten Abruf', async (status) => {
    const api = vi.fn().mockRejectedValue(Object.assign(new Error('x'), { status }))
    expect(await fetchPublicTranslations(api, 'acme', 'en')).toBeNull()
    expect(api).toHaveBeenCalledTimes(1)
  })
  it('wiederholt bei 400 ohne angefragte Sprache nicht', async () => {
    const api = vi.fn().mockRejectedValue(Object.assign(new Error('x'), { status: 400 }))
    expect(await fetchPublicTranslations(api, 'acme', '')).toBeNull()
    expect(api).toHaveBeenCalledTimes(1)
  })
  it('gibt null bei Fehler ohne Fehlerobjekt', async () => {
    const api = vi.fn().mockRejectedValue(undefined)
    expect(await fetchPublicTranslations(api, 'acme', 'en')).toBeNull()
  })
})

describe('localizeTree', () => {
  const tree = () => [
    { ID: 1, Name: 'Web', Description: 'Webseite', Children: [{ ID: 2, Name: 'CDN', Description: '', Children: [] }] },
    { ID: 3, Name: 'API', Description: 'x', Children: [] },
  ]
  it('übersetzt Name und Beschreibung, auch verschachtelt', () => {
    const out = localizeTree(tree(), { 1: { title: 'Website', text: 'The site' }, 2: { title: 'Edge', text: 'cache' } })
    expect(out[0].Name).toBe('Website')
    expect(out[0].Description).toBe('The site')
    expect(out[0].Children[0].Name).toBe('Edge')
    expect(out[0].Children[0].Description).toBe('cache')
    expect(out[1]).toEqual(tree()[1])
  })
  it('übersetzt Kinder unter einer nicht übersetzten Gruppe', () => {
    const out = localizeTree(tree(), { 2: { title: 'Edge', text: '' } })
    expect(out[0].Name).toBe('Web')
    expect(out[0].Children[0].Name).toBe('Edge')
    expect(out[0].Children[0].Description).toBe('')
  })
  it('behält die Objekte ohne Übersetzung unverändert (gleiche Identität)', () => {
    const t = tree()
    const out = localizeTree(t, { 99: { title: 'x', text: 'y' } })
    expect(out).toBe(t)
  })
  it('behält das Original bei leerem oder fehlendem Text', () => {
    const out = localizeTree(tree(), { 3: { title: '   ', text: undefined } })
    expect(out[1].Name).toBe('API')
    expect(out[1].Description).toBe('x')
  })
  it('gibt ohne Karte die Eingabe zurück', () => {
    const t = tree()
    expect(localizeTree(t, null)).toBe(t)
    expect(localizeTree(t, {})).toBe(t)
    expect(localizeTree(t, [])).toBe(t)
    expect(localizeTree(null, { 1: {} })).toBeNull()
  })
})

describe('localizeIncidents', () => {
  const incs = () => [
    { ID: 10, Title: 'Down', Message: 'Msg', Components: [{ ID: 1, Name: 'Web' }, { ID: 2, Name: 'API' }] },
    { ID: 11, Title: 'Other', Message: 'M2', Components: [{ ID: 2, Name: 'API' }] },
    { ID: 12, Title: 'NoComp', Message: 'M3' },
  ]
  it('übersetzt Titel, Erstmeldung und die Namen der betroffenen Komponenten', () => {
    const out = localizeIncidents(incs(), { incidents: { 10: { title: 'Gestört', text: 'Text' } }, components: { 1: { title: 'Website', text: '' } } })
    expect(out[0].Title).toBe('Gestört')
    expect(out[0].Message).toBe('Text')
    expect(out[0].Components.map(c => c.Name)).toEqual(['Website', 'API'])
  })
  it('übersetzt nur betroffene Komponenten, wenn der Vorfall selbst keine Übersetzung hat', () => {
    const out = localizeIncidents(incs(), { incidents: {}, components: { 2: { title: 'Schnittstelle', text: '' } } })
    expect(out[0].Title).toBe('Down')
    expect(out[0].Components.map(c => c.Name)).toEqual(['Web', 'Schnittstelle'])
    expect(out[1].Components[0].Name).toBe('Schnittstelle')
  })
  it('lässt Vorfälle ohne Treffer unverändert und legt keine Components an', () => {
    const list = incs()
    const out = localizeIncidents(list, { incidents: { 12: { title: 'T', text: 'M' } }, components: {} })
    expect(out[0]).toBe(list[0])
    expect(out[2].Title).toBe('T')
    expect('Components' in out[2]).toBe(false)
  })
  it('behält das Original bei leeren Texten', () => {
    const out = localizeIncidents(incs(), { incidents: { 10: { title: '', text: ' ' } }, components: {} })
    expect(out[0].Title).toBe('Down')
    expect(out[0].Message).toBe('Msg')
  })
  it('kommt ohne Komponenten-Karte zurecht', () => {
    const out = localizeIncidents(incs(), { incidents: { 10: { title: 'Gestört', text: 'Text' } } })
    expect(out[0].Title).toBe('Gestört')
    expect(out[0].Components.map(c => c.Name)).toEqual(['Web', 'API'])
  })
  it('gibt ohne Übersetzungen die Eingabe zurück', () => {
    const list = incs()
    expect(localizeIncidents(list, null)).toBe(list)
    expect(localizeIncidents(null, {})).toBeNull()
  })
})

describe('localizeNotices', () => {
  const notes = () => [{ ID: 5, Title: 'Hinweis', Message: 'Text', Level: 'info' }, { ID: 6, Title: 'B', Message: 'b' }]
  it('übersetzt Titel und Meldung, behält die übrigen Felder', () => {
    const out = localizeNotices(notes(), { 5: { title: 'Notice', text: 'Body' } })
    expect(out[0]).toEqual({ ID: 5, Title: 'Notice', Message: 'Body', Level: 'info' })
    expect(out[1]).toEqual(notes()[1])
  })
  it('gibt ohne Karte die Eingabe zurück', () => {
    const list = notes()
    expect(localizeNotices(list, null)).toBe(list)
    expect(localizeNotices(list, [])).toBe(list)
    expect(localizeNotices(null, {})).toBeNull()
  })
})

// Update-Texte eines Vorfalls.
describe('fetchPublicUpdateTranslations', () => {
  const ok = { language: 'en', default_language: 'de', updates: { 7: 'Seven' } }
  it('baut die URL mit Vorfall, Sprache und Token und liefert die Map', async () => {
    const api = vi.fn().mockResolvedValue(ok)
    expect(await fetchPublicUpdateTranslations(api, 'acme', 100, 'en', 'tok 1')).toEqual({ 7: 'Seven' })
    expect(api).toHaveBeenCalledWith('/api/public/pages/acme/incidents/100/update-translations?lang=en&token=tok%201', { expectedStatuses: [400] })
  })
  it('lässt ohne Sprache und Token die Query weg', async () => {
    const api = vi.fn().mockResolvedValue(ok)
    await fetchPublicUpdateTranslations(api, 'acme', 100, '', undefined)
    expect(api).toHaveBeenCalledWith('/api/public/pages/acme/incidents/100/update-translations', expect.anything())
  })
  it('fragt bei 400 (Sprache nicht aktiv) ohne Sprache noch einmal nach', async () => {
    const api = vi.fn().mockRejectedValueOnce(Object.assign(new Error('x'), { status: 400 })).mockResolvedValueOnce(ok)
    expect(await fetchPublicUpdateTranslations(api, 'acme', 100, 'es', 't')).toEqual({ 7: 'Seven' })
    expect(api.mock.calls[1][0]).toBe('/api/public/pages/acme/incidents/100/update-translations?token=t')
  })
  it('liefert bei Fehlern und unbrauchbaren Antworten eine leere Map', async () => {
    expect(await fetchPublicUpdateTranslations(vi.fn().mockRejectedValue(new Error('boom')), 'acme', 1, 'en')).toEqual({})
    expect(await fetchPublicUpdateTranslations(vi.fn().mockRejectedValue(Object.assign(new Error('x'), { status: 400 })), 'acme', 1, '')).toEqual({})
    for (const res of [null, 'x', [], { updates: [] }, { updates: null }, {}]) {
      expect(await fetchPublicUpdateTranslations(vi.fn().mockResolvedValue(res), 'acme', 1, 'en')).toEqual({})
    }
  })
})

describe('localizeUpdates', () => {
  const list = [{ id: 1, message: 'eins', status: 'monitoring' }, { id: 2, message: 'zwei' }]
  it('ersetzt nur die Nachricht der übersetzten Updates', () => {
    expect(localizeUpdates(list, { 2: 'two' })).toEqual([list[0], { id: 2, message: 'two' }])
    expect(localizeUpdates(list, { 1: 'one' })[0]).toEqual({ id: 1, message: 'one', status: 'monitoring' })
  })
  it('behält das Original bei leerem oder unbrauchbarem Text', () => {
    expect(localizeUpdates(list, { 1: '  ', 2: 5 })).toEqual(list)
  })
  it('liefert dieselbe Liste, wenn es nichts zu übersetzen gibt', () => {
    expect(localizeUpdates(list, {})).toBe(list)
    expect(localizeUpdates(list, undefined)).toBe(list)
    expect(localizeUpdates(list, [])).toBe(list)
    expect(localizeUpdates(undefined, { 1: 'x' })).toBeUndefined()
  })
})
