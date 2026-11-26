// axe-core-Prüfung für Component-Tests.
//
// Nutzung:
//   const wrapper = mount(Comp, { global: { plugins: [i18n] } })
//   expect(await axe(wrapper)).toHaveNoViolations()
//
// Übergeben werden kann ein VTU-Wrapper oder ein DOM-Element. Beim Wrapper
// wird der Mount-Container geprüft, damit Komponenten mit mehreren
// Wurzelknoten (Fragmente) vollständig erfasst werden. axe braucht den
// Baum im Dokument (z. B. für <label for>); ein ohne attachTo gemounteter
// Baum wird deshalb für die Dauer der Prüfung an document.body gehängt und
// danach wieder gelöst, damit bestehende Tests ihren Mount nicht ändern
// müssen. Per Teleport ausgelagerte Inhalte liegen außerhalb des Containers
// und werden nicht mitgeprüft.
//
// Warum nur ausgewählte Regeln statt des vollen WCAG-Satzes:
// - Die Tests laufen in happy-dom ohne Layout und ohne CSS. Regeln, die
//   gerenderte Farben, Größen oder Sichtbarkeit brauchen (color-contrast,
//   target-size, scrollable-region-focusable), liefern dort nur Rauschen
//   oder gar nichts. Kontrast muss weiter im Browser geprüft werden.
// - Gemountet wird meist eine einzelne View ohne App-Rahmen. Seitenregeln
//   (region, landmark-*, page-has-heading-one, document-title, html-has-lang,
//   bypass) treffen deshalb jedes Fragment und gehören in einen E2E-Lauf.
// - sit-onyx ist in vielen Tests durch src/test-support/onyxStubs.js ersetzt.
//   Die Stubs bilden nur die Beschriftung nach (label -> aria-label bzw.
//   Button-Text), nicht onyx' internes Markup. Geprüft wird damit, ob unsere
//   Views den Komponenten eine Beschriftung mitgeben; onyx' eigenes Markup
//   ist Sache der Bibliothek. Strukturregeln wie nested-interactive oder
//   list/listitem würden an den Stubs Funde erzeugen, die es im echten
//   Markup nicht gibt (oder umgekehrt übersehen) und bleiben deshalb aus.
// Übrig bleiben die Regeln, um die es geht: Namen für Buttons,
// Links, Bilder und Formularfelder sowie gültige ARIA-Attribute.
import axeCore from 'axe-core'
import { expect } from 'vitest'

export const A11Y_RULES = [
  'button-name',
  'input-button-name',
  'input-image-alt',
  'label',
  'select-name',
  'link-name',
  'image-alt',
  'svg-img-alt',
  'role-img-alt',
  'aria-allowed-attr',
  'aria-allowed-role',
  'aria-command-name',
  'aria-dialog-name',
  'aria-hidden-focus',
  'aria-input-field-name',
  'aria-prohibited-attr',
  'aria-required-attr',
  'aria-roles',
  'aria-toggle-field-name',
  'aria-valid-attr',
  'aria-valid-attr-value',
  'duplicate-id-aria',
]

function topAncestor(node) {
  let root = node
  while (root.parentNode) root = root.parentNode
  return root
}

export async function axe(target) {
  const element = target.element ? (target.element.parentElement || target.element) : target
  const root = topAncestor(element)
  const detached = root !== document
  if (detached) document.body.appendChild(root)
  try {
    const result = await axeCore.run(element, {
      runOnly: { type: 'rule', values: A11Y_RULES },
      resultTypes: ['violations'],
    })
    return result.violations
  } finally {
    if (detached) root.remove()
  }
}

// Je Regel nur die ersten Knoten zeigen: Listen wie die 90 Verfügbarkeits-
// balken erzeugen sonst seitenlange Meldungen mit identischem Markup.
const MAX_NODES = 3

export function formatViolations(violations) {
  return violations
    .map(v => {
      const shown = v.nodes.slice(0, MAX_NODES).map(n => `  ${n.html}`)
      const rest = v.nodes.length - MAX_NODES
      if (rest > 0) shown.push(`  … und ${rest} weitere`)
      return `${v.id}: ${v.help}\n${shown.join('\n')}`
    })
    .join('\n')
}

expect.extend({
  toHaveNoViolations(violations) {
    const pass = violations.length === 0
    return {
      pass,
      message: () => (pass
        ? 'axe-Funde erwartet, aber keine gefunden'
        : `axe-Funde:\n${formatViolations(violations)}`),
    }
  },
})
