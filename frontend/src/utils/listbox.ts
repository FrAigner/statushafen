// Redesign stage 3: keyboard logic of the listbox button
// (QuickStatus) as small, individually testable functions, following the
// ARIA "Select-Only Combobox" pattern. Focus stays on the button; the
// active option is indicated via aria-activedescendant.

// Keys that open the list on the closed button.
export function isOpenKey(key: string): boolean {
  return key === 'ArrowDown' || key === 'ArrowUp' || key === 'Enter' || key === ' '
}

// Keys that pick the active option in the open list.
export function isSelectKey(key: string): boolean {
  return key === 'Enter' || key === ' '
}

// New active index for a navigation key; null if the key does not
// navigate. The list does not jump from end to start (no wrap-around), so that
// one does not accidentally reach "major outage" in a status list.
export function nextActiveIndex(current: number, key: string, length: number): number | null {
  if (length < 1) return null
  const last = length - 1
  if (key === 'ArrowDown') return Math.min(current + 1, last)
  if (key === 'ArrowUp') return Math.max(current - 1, 0)
  if (key === 'Home') return 0
  if (key === 'End') return last
  return null
}
