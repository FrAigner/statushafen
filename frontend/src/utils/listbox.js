// Redesign Stufe 3: Tastaturlogik des Listbox-Buttons
// (QuickStatus) als kleine, einzeln testbare Funktionen, nach dem
// ARIA-Muster „Select-Only Combobox“. Der Fokus bleibt auf dem Button, die
// aktive Option wird über aria-activedescendant angezeigt.

// Tasten, die am geschlossenen Button die Liste öffnen.
export function isOpenKey(key) {
  return key === 'ArrowDown' || key === 'ArrowUp' || key === 'Enter' || key === ' '
}

// Tasten, die in der offenen Liste die aktive Option übernehmen.
export function isSelectKey(key) {
  return key === 'Enter' || key === ' '
}

// Neuer aktiver Index zu einer Navigationstaste; null, wenn die Taste nicht
// navigiert. Die Liste springt nicht von Ende zu Anfang (kein Umbruch), damit
// man bei einer Statusliste nicht versehentlich „Großstörung“ erreicht.
export function nextActiveIndex(current, key, length) {
  if (length < 1) return null
  const last = length - 1
  if (key === 'ArrowDown') return Math.min(current + 1, last)
  if (key === 'ArrowUp') return Math.max(current - 1, 0)
  if (key === 'Home') return 0
  if (key === 'End') return last
  return null
}
