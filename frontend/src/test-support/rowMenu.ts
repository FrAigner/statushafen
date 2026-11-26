// Richtung C: Zeilenmenü (RowMenu) in Component-Tests bedienen.
// Die Einträge stehen auch im geschlossenen Menü im DOM (v-show), ein Klick auf
// den Eintrag genügt; der Knopf öffnet das Menü für Tastatur und Maus.
export const menuItem = (scope, text) =>
  scope.findAll('[role="menuitem"]').find(i => i.text().includes(text))

export const clickMenuItem = (scope, text) => menuItem(scope, text).trigger('click')
