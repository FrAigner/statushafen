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
