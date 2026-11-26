import { onBeforeUnmount, ref } from 'vue'

// Reactive wrapper around window.matchMedia; used e.g. by ResponsiveTable.vue
// to switch between table (tablet+) and card layout (mobile) without needing
// CSS display:none/block duplicates in the DOM.
export function useMediaQuery(query: string) {
  const mql = window.matchMedia(query)
  const matches = ref(mql.matches)

  const onChange = (e: MediaQueryListEvent) => { matches.value = e.matches }
  mql.addEventListener('change', onChange)
  onBeforeUnmount(() => mql.removeEventListener('change', onChange))

  return matches
}

// Tablet breakpoint (--bp-md: 768px in style.css) as the mobile boundary: below
// it the view counts as "mobile" (card layout, hamburger menu, ...).
export function useIsMobile() {
  return useMediaQuery('(max-width: 767px)')
}
