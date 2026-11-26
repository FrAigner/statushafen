import { ref } from 'vue'

// Slug of the currently shown status page. StatusPage.vue sets it, the login
// button in the header (PublicLayout.vue) passes it as ?page= to the login
// page, so that this page's "Sign in with SSO" button appears there. Shared
// state instead of a route parameter, because the page is also reachable via
// subdomain or custom domain (PublicRoot.vue) without :slug.
const currentPageSlug = ref<string | null>(null)
// True while a status page shows its subscription section. The user menu in the
// header offers "Manage subscription" only then: it jumps to that section and
// would do nothing anywhere else (landing page, docs, login, private page without access).
const subscriptionsAvailable = ref(false)

export function useCurrentPageSlug() {
  return { currentPageSlug, subscriptionsAvailable }
}
