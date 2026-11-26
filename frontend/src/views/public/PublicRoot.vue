<script setup lang="ts">
import { ref, onMounted } from 'vue'
import StatusPage from './StatusPage.vue'
import LandingPage from './LandingPage.vue'
import { useApi } from '../../composables/useApi'
import { appBaseDomains } from '../../utils/appDomains'

const { apiFetch } = useApi()

const slug = ref<string | null>(null)
const isResolvingCustomDomain = ref(false)

// PaaS base domains where the first subdomain level is the app name of the
// deployment (e.g. "statushafen-xxxx.onrender.com"), not a
// tenant slug. On such hosts tenant detection only works via
// the host-independent path /status/:slug.
const PLATFORM_DOMAINS = ['onrender.com', 'vercel.app', 'netlify.app', 'herokuapp.com']

// Subdomains of the own base domain that can NEVER be a tenant slug,
// because they are already taken otherwise.
const RESERVED_SUBDOMAINS = new Set(['www', 'staging'])

function isPlatformHost(host: string) {
  return PLATFORM_DOMAINS.some(domain => host.endsWith(`.${domain}`))
}

// The application's own base domain(s) (VITE_APP_BASE_DOMAINS): sub.<one of these
// domains> is always a tenant slug by subdomain convention, see below. Everything
// else (e.g. "status.kundenfirma.de") is a potential custom domain
// and is resolved by a backend lookup instead of guessed by a name heuristic. Without
// a configured base domain there is no subdomain convention.
function isOwnBaseDomain(host: string) {
  return appBaseDomains().some(base => host === base || host.endsWith(`.${base}`))
}

onMounted(async () => {
  const host = window.location.hostname
  const parts = host.split('.')

  // Simple logic: localhost or 127.0.0.1 means no slug, unless it is sub.localhost
  // In production: sub.domain.com -> parts.length >= 3
  if (host === 'localhost' || host === '127.0.0.1') {
    slug.value = null
  } else if (host.endsWith('localhost') && parts.length > 1) {
    slug.value = parts[0] as string
  } else if (isPlatformHost(host)) {
    slug.value = null
  } else if (isOwnBaseDomain(host)) {
    // "staging" is reserved: staging.<basedomain> is the staging environment
    // of the app itself, not a tenant subdomain - otherwise
    // the app tries to load a status page with slug "staging" that will
    // never exist, and shows a load error instead of the landing page.
    slug.value = parts.length >= 3 && !RESERVED_SUBDOMAINS.has(parts[0] as string) ? parts[0] as string : null
  } else {
    // Neither a platform nor an own base domain -> possible custom domain.
    // Resolution runs via the server, since no naming
    // convention applies here (e.g. "status.kundenfirma.de").
    isResolvingCustomDomain.value = true
    try {
      const resp = await apiFetch<{ slug?: string }>(`/api/public/resolve-domain?host=${encodeURIComponent(host)}`)
      slug.value = resp.slug || null
    } catch {
      slug.value = null
    } finally {
      isResolvingCustomDomain.value = false
    }
  }
})
</script>

<template>
  <StatusPage v-if="slug" :slug="slug" />
  <LandingPage v-else-if="!isResolvingCustomDomain" />
</template>
