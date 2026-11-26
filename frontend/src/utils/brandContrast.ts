// 4a: backgrounds against which a status page's brand colours are
// checked for contrast (utils/contrast.ts computes with hex values and cannot
// resolve a var(--…)). The values mirror the tokens of the "public" layer
// in assets/tokens.css; brandContrast.test.ts checks that they do not
// deviate from them. They live here and not in the view, so that the hex ban
// (hexColors.test.ts) only counts colours in UIs, not calculation inputs.
export const BRAND_ON_PRIMARY = '#ffffff' // --on-primary: text on --primary
export const BRAND_SECONDARY_BACKGROUNDS = Object.freeze({
  // --surface, --surface-2, then the status tints of the hero:
  // --ok-soft, --deg-soft, --part-soft, --major-soft, --maint-soft
  light: ['#ffffff', '#f3f3f5', '#e9f6ee', '#fbf3dc', '#fdeee4', '#fcebeb', '#e8f0fd'],
  dark: ['#141418', '#1b1b21', '#132a1e', '#2c2410', '#311c10', '#341617', '#14223a'],
})
