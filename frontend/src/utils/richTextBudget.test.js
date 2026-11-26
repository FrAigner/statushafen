// jsdom instead of happy-dom: DOMPurify needs a complete DOM (see richText.test.js).
// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import {
  renderRichText, markdownToPlainText, resetRichTextBudget, richTextRenderCost,
  RICH_TEXT_RENDER_BUDGET, RICH_TEXT_RENDER_REFILL_PER_SECOND,
} from './richText'

// Every text stays individually limited to 5,000 characters (block) or 8,000 (total),
// but many such texts on one page added up to seconds of blocking time
// (marked is quadratic for `*a_`). Hence a budget applies to
// formatting new texts (characters plus a quadratic share; it refills
// over time); once it is used up, the text appears unformatted
// as escaped plain text. Cache hits cost nothing.

// Starts with real formatting (`**fett**`), so that the formatted result and the
// plain-text fallback can be told apart; the long rest is the quadratic pattern.
const pathological = (salt) => `**fett ${salt}** ${'*a_'.repeat(1650)}`
const literalOf = (text) => `<p>${text}</p>` // the text only contains * _ a, letters and digits: nothing to escape
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
    expect(fallbacks).toBeLessThan(40) // the first ones are still formatted
    // The fallback is the complete text (nothing cut off); the formatted ones carry <strong>.
    for (const [i, html] of results.entries()) expect(isLiteral(html, texts[i]) || html.includes('<strong>')).toBe(true)
  })

  it('der Fallback wird nicht gemerkt: mit aufgefülltem Budget wird derselbe Text formatiert', () => {
    const texts = Array.from({ length: 40 }, (_, i) => pathological(`x${i}`))
    texts.forEach(renderRichText)
    const last = texts[39]
    expect(isLiteral(renderRichText(last), last)).toBe(true)

    vi.advanceTimersByTime(30_000) // 30 s * refill rate > budget
    expect(isLiteral(renderRichText(last), last)).toBe(false)
  })

  it('füllt sich mit der festen Rate wieder auf, höchstens bis zum Budget', () => {
    const cost = richTextRenderCost(pathological('0'))
    const first = Array.from({ length: Math.ceil(RICH_TEXT_RENDER_BUDGET / cost) + 1 }, (_, i) => pathological(`a${i}`))
    first.forEach(renderRichText) // budget empty
    const probe = pathological('probe')
    expect(isLiteral(renderRichText(probe), probe)).toBe(true)

    vi.advanceTimersByTime((cost / RICH_TEXT_RENDER_REFILL_PER_SECOND) * 1000 + 50)
    const again = pathological('probe2')
    expect(isLiteral(renderRichText(again), again)).toBe(false) // exactly one expensive text fits again
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
    expect(plain[0].startsWith('fett p0')).toBe(true) // formatted: without asterisks
    expect(plain[39].startsWith('**fett p39**')).toBe(true) // budget empty: raw text
  })
})

describe('Render-Budget: feste Größen', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    resetRichTextBudget()
  })
  afterEach(() => vi.useRealTimers())

  // Without fixed numbers, an accidentally enlarged constant would drag all other
  // tests along (they compute with the exported values).
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
    expect(RICH_TEXT_RENDER_BUDGET / RICH_TEXT_RENDER_REFILL_PER_SECOND).toBeGreaterThanOrEqual(5) // a full budget takes seconds
  })

  // Empties the budget exactly: plain text without formatting characters costs its length.
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
    const exact = `*x*${'y'.repeat(1497)}` // 1500 characters, 2 formatting characters: cost 1500 + 2*1500/1500 = 1502
    expect(richTextRenderCost(exact)).toBe(1502)
    expect(renderRichText(exact)).toContain('<em>x</em>')
    const next = '*z*'
    expect(renderRichText(next)).not.toContain('<em>') // budget now exactly empty
  })
})

describe('Render-Budget: Messung', () => {
  // Control: as many expensive texts as the budget allows (unthrottled). Attack:
  // forty different ones. Without a budget that would be about 7 times as many seconds.
  // The threshold is generous, so that slow machines do not flake.
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
