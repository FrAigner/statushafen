import { ref } from 'vue'

// Richtung A, Startseite: Die Startseite hat eine eigene, volle
// Kopfzeile (Logo, Navigation, Anmelden, Bestell-Knopf), Statusseiten und die
// übrigen öffentlichen Seiten behalten den schlichten Kopf. PublicLayout kennt
// die Route nicht (auf „/“ rendert PublicRoot je nach Host auch eine
// Statusseite), deshalb meldet LandingPage selbst, dass sie angezeigt wird.
// Geteilter Zustand nach Muster von useCurrentPageSlug.
const isLanding = ref(false)
// Self-Service-Bestellung abgeschaltet -> beide Bestell-Knöpfe
// (Kopfzeile und Hero) verschwinden. Default true (optimistisch).
const ordersEnabled = ref(true)

export function useLandingChrome() {
  return { isLanding, ordersEnabled }
}

// Loads whether self-service ordering is on, for the order buttons of the landing header.
// Pages that show the landing header (landing page, docs) call it on mount.
export async function loadOrdersEnabled(apiFetch) {
  try {
    const flag = await apiFetch('/api/public/orders/enabled')
    ordersEnabled.value = flag.enabled
  } catch (err) {
    console.error('Failed to load orders flag', err)
  }
}
