<script setup lang="ts">
/*
  Profile section "Link with SSO" for page SSO (OIDC) and global providers.
  GET /api/me/sso returns the own links and the pages the account may still link
  (same rules as OIDCLinkHandler). Linking starts POST /api/auth/oidc/link?page=N
  for a page or POST /api/auth/provider/<id>/link for a global provider from the
  auth config (same { url } answer) and follows the authorize URL; the callback
  comes back here with next=profile, errors arrive as ?oidc_error=<code>. Without
  links, linkable pages and providers the section stays hidden.
*/
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useApi } from '../composables/useApi'
import { useAuthConfig } from '../composables/useAuthConfig'
import AppIcon from './AppIcon.vue'
import { safeIdpRedirect } from '../utils/safeIdpRedirect'
import { confirmDialog } from '../composables/useConfirm'

/** A linked identity from GET /api/me/sso: page SSO (page_name) or a global provider (provider id). */
interface SsoIdentity {
  id: number
  provider?: string
  page_name?: string
  email?: string
}

/** A page the account may still link. */
interface LinkablePage {
  status_page_id: number
  page_name: string
}

/** Response of GET /api/me/sso. */
interface SsoLinksResponse {
  identities: SsoIdentity[]
  linkable: LinkablePage[]
}

interface AuthProvider {
  id: string
  name: string
}

const { t, te } = useI18n()
const { apiFetch } = useApi()
const { authConfig, loadAuthConfig } = useAuthConfig()
const providers = computed(() => authConfig.value?.providers ?? [])

const identities = ref<SsoIdentity[]>([])
const linkable = ref<LinkablePage[]>([])
const errorMsg = ref('')
const statusMsg = ref('')

const callbackError = new URLSearchParams(window.location.search).get('oidc_error')
if (callbackError) {
  const key = `auth.oidcErrors.${callbackError}`
  errorMsg.value = te(key) ? t(key) : t('auth.oidcErrors.unknown')
}

const visible = computed(() =>
  identities.value.length > 0 || linkable.value.length > 0 || providers.value.length > 0
  || errorMsg.value !== '' || statusMsg.value !== ''
)

const pageName = (entry: SsoIdentity) => entry.page_name || t('profile.sso.unknownPage')
// Assumption about GET /api/me/sso: a link to a global provider carries its id in
// `provider`; it is labelled with the provider's name (or the id if unknown).
const identityName = (entry: SsoIdentity) => entry.provider
  ? providers.value.find((p) => p.id === entry.provider)?.name ?? entry.provider
  : pageName(entry)

const load = async () => {
  try {
    const data: SsoLinksResponse = await apiFetch('/api/me/sso')
    identities.value = data.identities
    linkable.value = data.linkable
  } catch {
    errorMsg.value = t('profile.sso.loadError')
  }
}

// Both link endpoints answer { url }; only a safe authorize URL is followed.
const followLink = async (endpoint: string) => {
  errorMsg.value = ''
  try {
    const { url }: { url?: unknown } = await apiFetch(endpoint, { method: 'POST' })
    const target = safeIdpRedirect(url)
    if (!target) throw new Error(t('auth.oidcErrors.unsafe_redirect'))
    window.location.assign(target)
  } catch (err) {
    errorMsg.value = t('profile.sso.linkError', { message: (err as Error).message })
  }
}

const startLink = (page: LinkablePage) => followLink(`/api/auth/oidc/link?page=${page.status_page_id}`)
const startProviderLink = (provider: AuthProvider) => followLink(`/api/auth/provider/${encodeURIComponent(provider.id)}/link`)

const unlink = async (identity: SsoIdentity) => {
  if (!(await confirmDialog({ message: t('profile.sso.unlinkConfirm', { page: pageName(identity) }), confirmLabel: t('confirm.unlink') }))) return
  errorMsg.value = ''
  statusMsg.value = ''
  try {
    await apiFetch(`/api/me/sso/${identity.id}`, { method: 'DELETE' })
    statusMsg.value = t('profile.sso.unlinked')
    await load()
  } catch {
    errorMsg.value = t('profile.sso.unlinkError')
  }
}

onMounted(() => {
  load()
  loadAuthConfig()
})
</script>

<template>
  <section v-if="visible" class="edit-card sso-links">
    <h3>{{ t('profile.sso.title') }}</h3>
    <p class="sso-links-text">{{ t('profile.sso.description') }}</p>

    <div v-if="errorMsg" class="alert alert-error mb-16" role="alert">{{ errorMsg }}</div>
    <div v-if="statusMsg" class="alert alert-success mb-16" role="status">
      {{ statusMsg }} <button type="button" class="alert-close" :aria-label="t('common.dismissMessage')" @click="statusMsg = ''"><AppIcon name="x" :size="14" /></button>
    </div>

    <template v-if="identities.length > 0">
      <h4>{{ t('profile.sso.linkedHeading') }}</h4>
      <ul class="sso-links-list">
        <li v-for="identity in identities" :key="identity.id" data-test="sso-linked">
          <span>
            <strong>{{ identityName(identity) }}</strong>
            <span v-if="identity.email" class="sso-links-email"> {{ t('profile.sso.linkedAs', { email: identity.email }) }}</span>
          </span>
          <button type="button" class="btn btn-secondary" @click="unlink(identity)">{{ t('profile.sso.unlink') }}</button>
        </li>
      </ul>
    </template>

    <template v-if="linkable.length > 0">
      <h4>{{ t('profile.sso.linkableHeading') }}</h4>
      <ul class="sso-links-list">
        <li v-for="page in linkable" :key="page.status_page_id" data-test="sso-linkable">
          <strong>{{ page.page_name }}</strong>
          <button type="button" class="btn btn-primary" @click="startLink(page)">{{ t('profile.sso.link') }}</button>
        </li>
      </ul>
    </template>

    <template v-if="providers.length > 0">
      <h4>{{ t('profile.sso.providersHeading') }}</h4>
      <ul class="sso-links-list">
        <li v-for="provider in providers" :key="provider.id" data-test="sso-provider">
          <strong>{{ provider.name }}</strong>
          <button type="button" class="btn btn-primary" @click="startProviderLink(provider)">
            {{ t('profile.sso.linkProvider', { name: provider.name }) }}
          </button>
        </li>
      </ul>
    </template>
  </section>
</template>

<style scoped>
.sso-links {
  margin-top: 24px;
}
.sso-links-text,
.sso-links-email {
  color: var(--text-3);
}
.sso-links-text {
  margin-bottom: 16px;
}
.sso-links-list {
  list-style: none;
  padding: 0;
  margin: 8px 0 16px;
}
.sso-links-list li {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 8px 0;
  border-bottom: 1px solid var(--border);
}
</style>
