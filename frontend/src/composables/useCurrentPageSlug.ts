import { ref } from 'vue'

// Slug der gerade angezeigten Statusseite. StatusPage.vue setzt
// ihn, der Login-Knopf im Kopf (PublicLayout.vue) gibt ihn als ?page= an die
// Login-Seite weiter, damit dort der Knopf „Mit SSO anmelden“ dieser Seite
// erscheint. Als geteilter Zustand statt Routen-Parameter, weil die Seite auch
// über Subdomain oder eigene Domain (PublicRoot.vue) ohne :slug erreichbar ist.
const currentPageSlug = ref(null)

export function useCurrentPageSlug() {
  return { currentPageSlug }
}
