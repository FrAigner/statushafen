// Contrast per WCAG 2.1 (relative luminance, 1.4.3) for colours that do
// not come from our design tokens but are set by the admin (branding per
// status page: PrimaryColor, SecondaryColor). The tokens themselves are chosen
// to fit; a freely chosen brand colour can however be too light or too dark
// (the default #4c6ef5 carries white text only at 4.3:1).
// ensureContrast shifts such a colour in small steps towards black or white
// until it reaches the ratio; fitting colours stay exactly as they are.

/** An sRGB colour as [r, g, b] with channels 0..255 (fractional while mixing). */
export type Rgb = number[]

const HEX = /^#?([0-9a-f]{3}|[0-9a-f]{6})$/i

export function parseHex(value: unknown): Rgb | null {
  const m = HEX.exec(String(value).trim())
  if (!m) return null
  let hex = m[1] as string
  if (hex.length === 3) hex = hex.split('').map(c => c + c).join('')
  return [0, 2, 4].map(i => parseInt(hex.slice(i, i + 2), 16))
}

export function toHex(rgb: Rgb): string {
  return '#' + rgb.map(c => Math.round(c).toString(16).padStart(2, '0')).join('')
}

function channel(c: number): number {
  const s = c / 255
  // Stryker disable next-line EqualityOperator: no 8-bit value hits 0.04045 exactly
  return s <= 0.04045 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4
}

export function luminance(rgb: Rgb): number {
  const [r, g, b] = rgb.map(channel) as [number, number, number]
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

export function contrastRatio(a: Rgb, b: Rgb): number {
  const la = luminance(a)
  const lb = luminance(b)
  // Stryker disable next-line EqualityOperator: on equality both branches return the same
  const [hi, lo] = la > lb ? [la, lb] : [lb, la]
  return (hi + 0.05) / (lo + 0.05)
}

const STEPS = 20

// Mixes color step by step (5 % each) with black (light counterpart) or white
// (dark counterpart) until every counterpart in against reaches at least
// minRatio; at the latest black or white comes back. Invalid inputs come
// back unchanged, so that the branding never disappears entirely.
export function ensureContrast(color: string, against: string | readonly string[], minRatio = 4.5): string {
  const rgb = parseHex(color)
  const backgrounds = (Array.isArray(against) ? against : [against]).map(parseHex)
  // An empty list passes without a check of its own: every() is true then.
  if (!rgb || backgrounds.includes(null)) return color
  const enough = (c: Rgb) => backgrounds.every(bg => contrastRatio(c, bg as Rgb) >= minRatio)
  if (enough(rgb)) return color
  // 0.18 is roughly the luminance above which black has more contrast than white.
  // Stryker disable next-line EqualityOperator: no background hits 0.18 exactly
  const target = luminance(backgrounds[0] as Rgb) > 0.18 ? 0 : 255
  // Stryker disable next-line EqualityOperator: step 20 is the pure target colour, which is returned afterwards anyway
  for (let step = 1; step < STEPS; step++) {
    const mixed = rgb.map(c => c + (target - c) * (step / STEPS))
    if (enough(mixed)) return toHex(mixed)
  }
  return toHex([target, target, target])
}
