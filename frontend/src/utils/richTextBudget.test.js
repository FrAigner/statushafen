// jsdom statt happy-dom: DOMPurify braucht ein vollständiges DOM (siehe richText.test.js).
// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import {
  renderRichText, markdownToPlainText, resetRichTextBudget, richTextRenderCost,
  RICH_TEXT_RENDER_BUDGET, RICH_TEXT_RENDER_REFILL_PER_SECOND,
} from './richText'

// Jeder Text bleibt einzeln auf 5.000 Zeichen (Block) bzw. 8.000 (Gesamt)
// begrenzt, doch viele solche Texte auf einer Seite summierten sich zu Sekunden
// Blockierzeit (marked ist bei `*a_` quadratisch). Deshalb gilt ein Budget für
// das Formatieren neuer Texte (Zeichen plus quadratischer Anteil, es füllt sich
// mit der Zeit wieder auf); ist es aufgebraucht, erscheint der Text unformatiert
// als maskierter Klartext. Treffer im Zwischenspeicher kosten nichts.

// Beginnt mit echter Formatierung (`**fett**`), damit sich formatiertes Ergebnis und
// Klartext-Fallback unterscheiden lassen; der lange Rest ist das quadratische Muster.
const pathological = (salt) => `**fett ${salt}** ${'*a_'.repeat(1650)}`
const literalOf = (text) => `<p>${text}</p>` // Text enthält nur * _ a, Buchstaben und Ziffern: nichts zu maskieren
const isLiteral = (html, text) => html === literalOf(text)

describe('Render-Budget für formatierte Texte', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    resetRichTextBudget()
  })
  afterEach(() => vi.useRealTimers())

  it('Gegenprobe: normale Vorfalltexte werden alle formatiert', () => {
    for (let i = 0; i < 250; i++) {
      const html = renderRichText(`**Störung ${i}** bei der *API*, Details: https://example.com/status/${i}`)
      expect(html).toContain('<strong>')
      expect(html).toContain('<em>')
    }
  })

  it('Angriff: viele teure Texte laufen nach dem Budget in den Klartext-Fallback', () => {
    const texts = Array.from({ length: 40 }, (_, i) => pathological(String(i)))
    const results = texts.map(renderRichText)
    const fallbacks = results.filter((html, i) => isLiteral(html, texts[i])).length
    const maxFormatted = Math.floor(RICH_TEXT_RENDER_BUDGET / richTextRenderCost(pathological('0')))
    expect(fallbacks).toBeGreaterThanOrEqual(40 - maxFormatted - 1)
    expect(fallbacks).toBeLessThan(40) // die ersten werden noch formatiert
    // Der Fallback ist der vollständige Text (nichts abgeschnitten), die formatierten tragen <strong>.
    for (const [i, html] of results.entries()) expect(isLiteral(html, texts[i]) || html.includes('<strong>')).toBe(true)
  })

  it('der Fallback wird nicht gemerkt: mit aufgefülltem Budget wird derselbe Text formatiert', () => {
    const texts = Array.from({ length: 40 }, (_, i) => pathological(`x${i}`))
    texts.forEach(renderRichText)
    const last = texts[39]
    expect(isLiteral(renderRichText(last), last)).toBe(true)

    vi.advanceTimersByTime(30_000) // 30 s * Auffüllrate > Budget
    expect(isLiteral(renderRichText(last), last)).toBe(false)
  })

  it('füllt sich mit der festen Rate wieder auf, höchstens bis zum Budget', () => {
    const cost = richTextRenderCost(pathological('0'))
    const first = Array.from({ length: Math.ceil(RICH_TEXT_RENDER_BUDGET / cost) + 1 }, (_, i) => pathological(`a${i}`))
    first.forEach(renderRichText) // Budget leer
    const probe = pathological('probe')
    expect(isLiteral(renderRichText(probe), probe)).toBe(true)

    vi.advanceTimersByTime((cost / RICH_TEXT_RENDER_REFILL_PER_SECOND) * 1000 + 50)
    const again = pathological('probe2')
    expect(isLiteral(renderRichText(again), again)).toBe(false) // genau ein teurer Text passt wieder
    const next = pathological('probe3')
    expect(isLiteral(renderRichText(next), next)).toBe(true)
  })

  it('Treffer im Zwischenspeicher kosten kein Budget', () => {
    const text = pathological('einmal')
    const first = renderRichText(text)
    for (let i = 0; i < 500; i++) expect(renderRichText(text)).toBe(first)
    const other = pathological('zweiter')
    expect(isLiteral(renderRichText(other), other)).toBe(false)
  })

  it('der Klartext für Suche und Listen folgt demselben Budget', () => {
    const texts = Array.from({ length: 40 }, (_, i) => pathological(`p${i}`))
    const plain = texts.map(markdownToPlainText)
    expect(plain[0].startsWith('fett p0')).toBe(true) // formatiert: ohne Sternchen
    expect(plain[39].startsWith('**fett p39**')).toBe(true) // Budget leer: Rohtext
  })
})

describe('Render-Budget: feste Größen', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    resetRichTextBudget()
  })
  afterEach(() => vi.useRealTimers())

  // Ohne feste Zahlen würde eine versehentlich vergrößerte Konstante alle anderen
  // Tests mitziehen (sie rechnen mit den exportierten Werten).
  it('die Kosten sind Zeichen plus ein Anteil je Formatierzeichen', () => {
    expect(richTextRenderCost('a'.repeat(1000))).toBe(1000)
    for (const c of ['*', '_', '`', '>', '~', '[', ']']) {
      expect(richTextRenderCost(c.repeat(1500)), `Formatierzeichen ${c}`).toBe(3000)
    }
  })

  it('das Budget lässt nur wenige teure Texte zu und füllt sich langsam auf', () => {
    const cost = richTextRenderCost(pathological('0'))
    const allowed = Math.floor(RICH_TEXT_RENDER_BUDGET / cost)
    expect(allowed).toBeGreaterThanOrEqual(2)
    expect(allowed).toBeLessThanOrEqual(6)
    expect(RICH_TEXT_RENDER_BUDGET / RICH_TEXT_RENDER_REFILL_PER_SECOND).toBeGreaterThanOrEqual(5) // volles Budget braucht Sekunden
  })

  // Leert das Budget genau: Klartext ohne Formatierzeichen kostet seine Länge.
  const drain = (amount) => {
    let left = amount
    for (let i = 0; left > 0; i++) {
      const n = Math.min(5000, left)
      renderRichText(`p${i}-${amount}`.padEnd(n, 'a').slice(0, n))
      left -= n
    }
  }

  it('nach einer Sekunde ist noch kein weiterer teurer Text frei', () => {
    drain(RICH_TEXT_RENDER_BUDGET)
    vi.advanceTimersByTime(1000)
    const probe = pathological('nach-einer-sekunde')
    expect(isLiteral(renderRichText(probe), probe)).toBe(true)
  })

  it('auch nach langer Pause ist das Budget nicht größer als der Höchstwert', () => {
    vi.advanceTimersByTime(3_600_000)
    const allowed = Math.floor(RICH_TEXT_RENDER_BUDGET / richTextRenderCost(pathological('0')))
    let formatted = 0
    for (let i = 0; i < 20; i++) {
      const text = pathological(`pause${i}`)
      if (!isLiteral(renderRichText(text), text)) formatted++
    }
    expect(formatted).toBe(allowed)
  })

  it('jeder formatierte Text verbraucht Budget, auch ein einzelner', () => {
    const cost = richTextRenderCost(pathological('0'))
    const allowed = Math.floor(RICH_TEXT_RENDER_BUDGET / cost)
    const texts = Array.from({ length: allowed + 1 }, (_, i) => pathological(`v${i}`))
    const results = texts.map(renderRichText)
    expect(results.slice(0, allowed).every((html, i) => !isLiteral(html, texts[i]))).toBe(true)
    expect(isLiteral(results[allowed], texts[allowed])).toBe(true)
  })

  it('ein Text, der genau dem Rest des Budgets entspricht, passt noch hinein', () => {
    drain(RICH_TEXT_RENDER_BUDGET - 1502)
    const exact = `*x*${'y'.repeat(1497)}` // 1500 Zeichen, 2 Formatierzeichen: Kosten 1500 + 2*1500/1500 = 1502
    expect(richTextRenderCost(exact)).toBe(1502)
    expect(renderRichText(exact)).toContain('<em>x</em>')
    const next = '*z*'
    expect(renderRichText(next)).not.toContain('<em>') // Budget jetzt genau leer
  })
})

describe('Render-Budget: Messung', () => {
  // Kontrolle: so viele teure Texte, wie das Budget erlaubt (ungebremst). Angriff:
  // vierzig verschiedene. Ohne Budget wären das ca. 7-mal so viele Sekunden.
  // Die Schwelle ist großzügig, damit langsame Rechner nicht flackern.
  it('vierzig teure Texte kosten höchstens ein Mehrfaches der Kontrolle', () => {
    resetRichTextBudget()
    const allowed = Math.floor(RICH_TEXT_RENDER_BUDGET / richTextRenderCost(pathological('0')))
    const t0 = performance.now()
    for (let i = 0; i < allowed; i++) renderRichText(pathological(`k${i}`))
    const control = performance.now() - t0

    resetRichTextBudget()
    const t1 = performance.now()
    for (let i = 0; i < 40; i++) renderRichText(pathological(`m${i}`))
    const attack = performance.now() - t1

    expect(attack).toBeLessThan(control * 3 + 300)
  })
})
