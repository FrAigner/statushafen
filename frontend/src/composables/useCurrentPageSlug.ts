import { ref } from 'vue'

// Slug of the currently shown status page. StatusPage.vue sets it, the login
// button in the header (PublicLayout.vue) passes it as ?page= to the login
// page, so that this page's "Sign in with SSO" button appears there. Shared
// state instead of a route parameter, because the page is also reachable via
// subdomain or custom domain (PublicRoot.vue) without :slug.
const currentPageSlug = ref<string | null>(null)

export function useCurrentPageSlug() {
  return { currentPageSlug }
}
