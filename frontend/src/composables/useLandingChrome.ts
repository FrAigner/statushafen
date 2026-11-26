import { ref } from 'vue'
import type { ApiFetch } from './useApi'

// Direction A, landing page: the landing page has its own full header (logo,
// navigation, sign-in, order button); status pages and the other public pages
// keep the plain header. PublicLayout does not know the route (on "/"
// PublicRoot may also render a status page depending on the host), so
// LandingPage itself reports that it is shown. Shared state following the
// pattern of useCurrentPageSlug.
const isLanding = ref(false)
// Self-service ordering switched off -> both order buttons (header and hero)
// disappear. Default true (optimistic).
const ordersEnabled = ref(true)

export function useLandingChrome() {
  return { isLanding, ordersEnabled }
}

// Loads whether self-service ordering is on, for the order buttons of the landing header.
// Pages that show the landing header (landing page, docs) call it on mount.
export async function loadOrdersEnabled(apiFetch: ApiFetch) {
  try {
    const flag = await apiFetch<{ enabled: boolean }>('/api/public/orders/enabled')
    ordersEnabled.value = flag.enabled
  } catch (err) {
    console.error('Failed to load orders flag', err)
  }
}
