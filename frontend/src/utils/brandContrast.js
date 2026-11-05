// 4a: Hintergründe, gegen die die Markenfarben einer Statusseite
// auf Kontrast geprüft werden (utils/contrast.js rechnet mit Hex-Werten, ein
// var(--…) kann es nicht auflösen). Die Werte spiegeln die Tokens der Ebene
// „public“ in assets/tokens.css; brandContrast.test.js prüft, dass sie nicht
// davon abweichen. Sie stehen hier und nicht in der View, damit die Hex-Sperre
// (hexColors.test.js) nur Farben in Oberflächen zählt, nicht Rechengrundlagen.
export const BRAND_ON_PRIMARY = '#ffffff' // --on-primary: Schrift auf --primary
export const BRAND_SECONDARY_BACKGROUNDS = Object.freeze({
  // --surface, --surface-2, dann die Statustönungen des Hero:
  // --ok-soft, --deg-soft, --part-soft, --major-soft, --maint-soft
  light: ['#ffffff', '#f3f3f5', '#e9f6ee', '#fbf3dc', '#fdeee4', '#fcebeb', '#e8f0fd'],
  dark: ['#141418', '#1b1b21', '#132a1e', '#2c2410', '#311c10', '#341617', '#14223a'],
})
