// Kontrast nach WCAG 2.1 (relative Leuchtdichte, 1.4.3) für Farben,
// die nicht aus unseren Design-Tokens kommen, sondern vom Admin gesetzt werden
// (Branding je Statusseite: PrimaryColor, SecondaryColor). Die Tokens selbst
// sind passend gewählt; eine frei gewählte Markenfarbe kann aber zu hell oder
// zu dunkel sein (der Standard #4c6ef5 trägt weiße Schrift nur mit 4,3:1).
// ensureContrast verschiebt eine solche Farbe in kleinen Schritten zu Schwarz
// bzw. Weiß, bis sie das Verhältnis erreicht; passende Farben bleiben exakt.

const HEX = /^#?([0-9a-f]{3}|[0-9a-f]{6})$/i

export function parseHex(value) {
  const m = HEX.exec(String(value).trim())
  if (!m) return null
  let hex = m[1]
  if (hex.length === 3) hex = hex.split('').map(c => c + c).join('')
  return [0, 2, 4].map(i => parseInt(hex.slice(i, i + 2), 16))
}

export function toHex(rgb) {
  return '#' + rgb.map(c => Math.round(c).toString(16).padStart(2, '0')).join('')
}

function channel(c) {
  const s = c / 255
  // Stryker disable next-line EqualityOperator: kein 8-Bit-Wert trifft 0,04045 exakt
  return s <= 0.04045 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4
}

export function luminance(rgb) {
  const [r, g, b] = rgb.map(channel)
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

export function contrastRatio(a, b) {
  const la = luminance(a)
  const lb = luminance(b)
  // Stryker disable next-line EqualityOperator: bei Gleichheit liefern beide Zweige dasselbe
  const [hi, lo] = la > lb ? [la, lb] : [lb, la]
  return (hi + 0.05) / (lo + 0.05)
}

const STEPS = 20

// Mischt color schrittweise (je 5 %) mit Schwarz (heller Gegenpart) bzw. Weiß
// (dunkler Gegenpart), bis jeder Gegenpart in against mindestens minRatio
// erreicht; spätestens kommt Schwarz bzw. Weiß zurück. Ungültige Eingaben
// kommen unverändert zurück, damit das Branding nie ganz wegfällt.
export function ensureContrast(color, against, minRatio = 4.5) {
  const rgb = parseHex(color)
  const backgrounds = (Array.isArray(against) ? against : [against]).map(parseHex)
  // Eine leere Liste fällt ohne eigene Prüfung durch: every() ist dann true.
  if (!rgb || backgrounds.includes(null)) return color
  const enough = c => backgrounds.every(bg => contrastRatio(c, bg) >= minRatio)
  if (enough(rgb)) return color
  // 0,18 ist etwa die Leuchtdichte, ab der Schwarz mehr Kontrast hat als Weiß.
  // Stryker disable next-line EqualityOperator: kein Hintergrund trifft 0,18 exakt
  const target = luminance(backgrounds[0]) > 0.18 ? 0 : 255
  // Stryker disable next-line EqualityOperator: Schritt 20 ist die reine Zielfarbe, die danach ohnehin zurückkommt
  for (let step = 1; step < STEPS; step++) {
    const mixed = rgb.map(c => c + (target - c) * (step / STEPS))
    if (enough(mixed)) return toHex(mixed)
  }
  return toHex([target, target, target])
}
