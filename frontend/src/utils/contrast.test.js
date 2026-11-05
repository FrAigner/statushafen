import { describe, it, expect } from 'vitest'
import { parseHex, toHex, luminance, contrastRatio, ensureContrast } from './contrast'

describe('parseHex', () => {
  it('liest sechsstellige Werte mit und ohne #, auch mit Leerraum und Großbuchstaben', () => {
    expect(parseHex('#4c6ef5')).toEqual([76, 110, 245])
    expect(parseHex('4C6EF5')).toEqual([76, 110, 245])
    expect(parseHex('  #0a0b0c ')).toEqual([10, 11, 12])
  })

  it('verdoppelt die Ziffern dreistelliger Werte', () => {
    expect(parseHex('#abc')).toEqual([170, 187, 204])
  })

  it('liefert null für alles andere', () => {
    for (const v of ['', 'red', '#12345', '#1234567', '#ggg', 'x#123456', '#123456x', null, undefined]) {
      expect(parseHex(v)).toBeNull()
    }
  })
})

describe('toHex', () => {
  it('rundet und füllt einstellige Kanäle mit 0 auf', () => {
    expect(toHex([0, 5.4, 255])).toBe('#0005ff')
    expect(toHex([15.5, 16, 254.6])).toBe('#1010ff')
  })
})

describe('luminance und contrastRatio (WCAG 2.1)', () => {
  it('kennt die Endpunkte und den linearen Bereich dunkler Kanäle', () => {
    expect(luminance([0, 0, 0])).toBe(0)
    expect(luminance([255, 255, 255])).toBeCloseTo(1, 10)
    expect(luminance([10, 10, 10])).toBeCloseTo(10 / 255 / 12.92, 10)
    expect(luminance([255, 0, 0])).toBeCloseTo(0.2126, 10)
    expect(luminance([0, 255, 0])).toBeCloseTo(0.7152, 10)
    expect(luminance([0, 0, 255])).toBeCloseTo(0.0722, 10)
    expect(luminance([128, 128, 128])).toBeCloseTo(0.2158605, 6)
  })

  it('ist symmetrisch und entspricht den bekannten Werten', () => {
    expect(contrastRatio([255, 255, 255], [0, 0, 0])).toBeCloseTo(21, 10)
    expect(contrastRatio([0, 0, 0], [255, 255, 255])).toBeCloseTo(21, 10)
    expect(contrastRatio(parseHex('#4c6ef5'), [255, 255, 255])).toBeCloseTo(4.32, 2)
    expect(contrastRatio([40, 40, 40], [40, 40, 40])).toBe(1)
  })
})

describe('ensureContrast', () => {
  it('lässt eine ausreichende Farbe unverändert (auch die Schreibweise)', () => {
    expect(ensureContrast('#123456', '#ffffff')).toBe('#123456')
    expect(ensureContrast('ABC', '#000', 3)).toBe('ABC')
  })

  it('dunkelt die Standard-Markenfarbe für weiße Schrift gerade so weit ab wie nötig', () => {
    const fixed = ensureContrast('#4c6ef5', '#ffffff')
    expect(fixed).toBe('#4869e9')
    expect(contrastRatio(parseHex(fixed), [255, 255, 255])).toBeGreaterThanOrEqual(4.5)
  })

  it('hellt auf dunklem Grund auf und prüft gegen alle Gegenparts', () => {
    const fixed = ensureContrast('#5e6c84', ['#161b22', '#21262d'])
    expect(fixed).toBe('#8691a3')
    for (const bg of ['#161b22', '#21262d']) {
      expect(contrastRatio(parseHex(fixed), parseHex(bg))).toBeGreaterThanOrEqual(4.5)
    }
    // Nur gegen den dunkleren Grund reichen weniger Mischschritte.
    expect(ensureContrast('#5e6c84', ['#161b22'])).toBe('#7e899d')
  })

  it('nutzt das geforderte Verhältnis (Standard 4,5:1)', () => {
    expect(ensureContrast('#777777', '#ffffff')).toBe('#717171')
    expect(ensureContrast('#777777', '#ffffff', 3)).toBe('#777777')
    expect(ensureContrast('#777777', '#ffffff', 7)).toBe('#535353')
  })

  it('akzeptiert eine Farbe, die das Verhältnis genau erreicht', () => {
    const exact = contrastRatio(parseHex('#777777'), [255, 255, 255])
    expect(ensureContrast('#777777', '#ffffff', exact)).toBe('#777777')
  })

  it('fällt auf Schwarz bzw. Weiß zurück, wenn kein Mischschritt reicht', () => {
    expect(ensureContrast('#ffffff', '#ffffff', 21)).toBe('#000000')
    expect(ensureContrast('#000000', '#000000', 21)).toBe('#ffffff')
  })

  it('lässt ungültige Eingaben unverändert', () => {
    expect(ensureContrast('rot', '#ffffff')).toBe('rot')
    expect(ensureContrast('#4c6ef5', 'weiß')).toBe('#4c6ef5')
    expect(ensureContrast('#4c6ef5', ['#ffffff', 'weiß'])).toBe('#4c6ef5')
    expect(ensureContrast('#4c6ef5', [])).toBe('#4c6ef5')
  })
})
