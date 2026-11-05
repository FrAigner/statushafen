<script setup>
import { ref, onMounted } from 'vue'
import StatusPage from './StatusPage.vue'
import LandingPage from './LandingPage.vue'
import { useApi } from '../../composables/useApi'
import { appBaseDomains } from '../../utils/appDomains'

const { apiFetch } = useApi()

const slug = ref(null)
const isResolvingCustomDomain = ref(false)

// PaaS-Basis-Domains, bei denen die erste Subdomain-Ebene der App-Name des
// Deployments ist (z.B. "statushafen-xxxx.onrender.com"), nicht ein
// Mandanten-Slug. Auf solchen Hosts greift die Mandanten-Erkennung nur über
// den host-unabhängigen Pfad /status/:slug.
const PLATFORM_DOMAINS = ['onrender.com', 'vercel.app', 'netlify.app', 'herokuapp.com']

// Subdomains der eigenen Basisdomain, die NIE ein Mandanten-Slug sein können,
// weil sie bereits anderweitig belegt sind.
const RESERVED_SUBDOMAINS = new Set(['www', 'staging'])

function isPlatformHost(host) {
  return PLATFORM_DOMAINS.some(domain => host.endsWith(`.${domain}`))
}

// Eigene Basis-Domain(s) der Anwendung (VITE_APP_BASE_DOMAINS): sub.<eine dieser
// Domains> ist immer ein Mandanten-Slug per Subdomain-Konvention, siehe unten. Alles
// andere (z.B. "status.kundenfirma.de") ist eine potenzielle Custom Domain
// und wird per Backend-Lookup aufgelöst statt per Namensheuristik geraten. Ohne
// konfigurierte Basisdomain gibt es keine Subdomain-Konvention.
function isOwnBaseDomain(host) {
  return appBaseDomains().some(base => host === base || host.endsWith(`.${base}`))
}

onMounted(async () => {
  const host = window.location.hostname
  const parts = host.split('.')

  // Einfache Logik: wenn localhost oder 127.0.0.1, dann kein slug, es sei denn es ist sub.localhost
  // In Produktion: sub.domain.com -> parts.length >= 3
  if (host === 'localhost' || host === '127.0.0.1') {
    slug.value = null
  } else if (host.endsWith('localhost') && parts.length > 1) {
    slug.value = parts[0]
  } else if (isPlatformHost(host)) {
    slug.value = null
  } else if (isOwnBaseDomain(host)) {
    // "staging" ist reserviert: staging.<basedomain> ist die Staging-Umgebung
    // der App selbst, keine Mandanten-Subdomain - sonst
    // versucht die App, eine Statusseite mit Slug "staging" zu laden, die es
    // nie geben wird, und zeigt statt der Startseite einen Ladefehler.
    slug.value = parts.length >= 3 && !RESERVED_SUBDOMAINS.has(parts[0]) ? parts[0] : null
  } else {
    // Weder Plattform- noch eigene Basis-Domain -> mögliche Custom Domain
    // Auflösung läuft über den Server, da hier keine
    // Namenskonvention greift (z.B. "status.kundenfirma.de").
    isResolvingCustomDomain.value = true
    try {
      const resp = await apiFetch(`/api/public/resolve-domain?host=${encodeURIComponent(host)}`)
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
