import { describe, it, expect } from 'vitest'
import { parseSearchQuery, matchesSearchQuery } from './useSearchQuery'

describe('parseSearchQuery', () => {
  it('liefert bei leerer oder nur aus Leerraum bestehender Suche keine Gruppen', () => {
    expect(parseSearchQuery('')).toEqual([])
    expect(parseSearchQuery('   ')).toEqual([])
    expect(parseSearchQuery(undefined)).toEqual([])
  })

  it('behandelt mehrere Wörter ohne Operator als UND', () => {
    expect(parseSearchQuery('Frankfurt Datenbank')).toEqual([['frankfurt', 'datenbank']])
  })

  it.each(['AND', 'UND', 'and', 'und', 'Und'])('erkennt %s als UND', (op) => {
    expect(parseSearchQuery(`A ${op} B`)).toEqual([['a', 'b']])
  })

  it.each(['OR', 'ODER', 'or', 'oder', 'Oder'])('erkennt %s als ODER', (op) => {
    expect(parseSearchQuery(`A ${op} B`)).toEqual([['a'], ['b']])
  })

  it('bindet UND stärker als ODER', () => {
    expect(parseSearchQuery('a OR b AND c')).toEqual([['a'], ['b', 'c']])
    expect(parseSearchQuery('a AND b OR c')).toEqual([['a', 'b'], ['c']])
  })

  it('behandelt Phrasen in Anführungszeichen als einen Term', () => {
    expect(parseSearchQuery('"Frankfurt Süd" AND db')).toEqual([['frankfurt süd', 'db']])
  })

  it('behandelt ein Operator-Wort in Anführungszeichen als normalen Term', () => {
    expect(parseSearchQuery('"and" b')).toEqual([['and', 'b']])
  })

  it('erkennt Operatoren nur als eigenständige Wörter', () => {
    expect(parseSearchQuery('android order')).toEqual([['android', 'order']])
  })

  it('ignoriert lose Operatoren ohne Operand', () => {
    expect(parseSearchQuery('Frankfurt AND')).toEqual([['frankfurt']])
    expect(parseSearchQuery('OR Paris')).toEqual([['paris']])
    expect(parseSearchQuery('a OR OR b')).toEqual([['a'], ['b']])
  })

  it('ergibt bei nur einem Operator keine Einschränkung', () => {
    expect(parseSearchQuery('AND')).toEqual([])
    expect(parseSearchQuery('oder')).toEqual([])
  })

  it('behandelt ein nicht geschlossenes Anführungszeichen als Phrase bis zum Ende', () => {
    expect(parseSearchQuery('a "b c')).toEqual([['a', 'b c']])
  })

  it('ignoriert leere Phrasen', () => {
    expect(parseSearchQuery('"" a')).toEqual([['a']])
  })

  it('trennt eine Phrase direkt hinter einem Wort ab', () => {
    expect(parseSearchQuery('a"b c"')).toEqual([['a', 'b c']])
  })
})

describe('matchesSearchQuery', () => {
  const fields = ['Frankfurt Rechenzentrum', 'Datenbank Cluster', null, undefined]

  it('trifft bei leerer Anfrage immer', () => {
    expect(matchesSearchQuery([], fields)).toBe(true)
  })

  it('verlangt bei UND alle Terme, auch über verschiedene Felder', () => {
    expect(matchesSearchQuery([['frankfurt', 'datenbank']], fields)).toBe(true)
    expect(matchesSearchQuery([['frankfurt', 'london']], fields)).toBe(false)
  })

  it('verlangt bei ODER nur eine Gruppe', () => {
    expect(matchesSearchQuery([['london'], ['cluster']], fields)).toBe(true)
    expect(matchesSearchQuery([['london'], ['paris']], fields)).toBe(false)
  })

  it('vergleicht ohne Beachtung der Groß-/Kleinschreibung als Teilstring', () => {
    expect(matchesSearchQuery([['rechenzent']], ['FRANKFURT RECHENZENTRUM'])).toBe(true)
  })

  it('kommt mit leerer Feldliste zurecht', () => {
    expect(matchesSearchQuery([['a']], [])).toBe(false)
  })
})
