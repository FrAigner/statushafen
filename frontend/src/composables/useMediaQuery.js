import { onBeforeUnmount, ref } from 'vue'

// Reaktiver Wrapper um window.matchMedia; wird u.a. von ResponsiveTable.vue
// genutzt, um zwischen Tabellen- (Tablet+) und Card-Darstellung (Mobile) zu
// wechseln, ohne CSS display:none/block-Duplikate im DOM zu benötigen.
export function useMediaQuery(query) {
  const mql = window.matchMedia(query)
  const matches = ref(mql.matches)

  const onChange = (e) => { matches.value = e.matches }
  mql.addEventListener('change', onChange)
  onBeforeUnmount(() => mql.removeEventListener('change', onChange))

  return matches
}

// Tablet-Breakpoint (--bp-md: 768px in style.css) als Mobile-Grenze: darunter
// gilt die Ansicht als "mobile" (Card-Layout, Hamburger-Menü, ...).
export function useIsMobile() {
  return useMediaQuery('(max-width: 767px)')
}
