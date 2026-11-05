<script setup>
/*
  Profilbereich „Mit SSO verknüpfen“ für das OIDC-Seiten-SSO.
  GET /api/me/sso liefert die eigenen Verknüpfungen und die Seiten, die das
  Konto noch verknüpfen darf (Regeln wie in OIDCLinkHandler). Verknüpfen
  startet POST /api/auth/oidc/link?page=N und folgt der gelieferten
  Authorize-URL; der Callback führt danach mit next=profile hierher zurück,
  Fehler kommen als ?oidc_error=<code>. Ohne Verknüpfungen und verknüpfbare
  Seiten bleibt der Bereich ausgeblendet.

  Einträge tragen ihr Protokoll (oidc/saml); SAML verknüpft über
  POST /api/auth/saml/link?page=N, Fehler kommen über denselben Parameter
  oidc_error. Fehlt das Feld (ältere Antwort), gilt OIDC.
*/
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useApi } from '../composables/useApi'
import AppIcon from './AppIcon.vue'
import { safeIdpRedirect } from '../utils/safeIdpRedirect'
import { confirmDialog } from '../composables/useConfirm'

const { t, te } = useI18n()
const { apiFetch } = useApi()

const identities = ref([])
const linkable = ref([])
const errorMsg = ref('')
const statusMsg = ref('')

const callbackError = new URLSearchParams(window.location.search).get('oidc_error')
if (callbackError) {
  const key = `auth.oidcErrors.${callbackError}`
  errorMsg.value = te(key) ? t(key) : t('auth.oidcErrors.unknown')
}

const visible = computed(() =>
  identities.value.length > 0 || linkable.value.length > 0 || errorMsg.value !== '' || statusMsg.value !== ''
)

const pageName = (entry) => entry.page_name || t('profile.sso.unknownPage')
const isSaml = (entry) => entry.protocol === 'saml'
const protocolLabel = (entry) => t(isSaml(entry) ? 'profile.sso.protocolSaml' : 'profile.sso.protocolOidc')

const load = async () => {
  try {
    const data = await apiFetch('/api/me/sso')
    identities.value = data.identities
    linkable.value = data.linkable
  } catch {
    errorMsg.value = t('profile.sso.loadError')
  }
}

const startLink = async (page) => {
  errorMsg.value = ''
  try {
    const endpoint = isSaml(page) ? 'saml' : 'oidc'
    const { url } = await apiFetch(`/api/auth/${endpoint}/link?page=${page.status_page_id}`, { method: 'POST' })
    const target = safeIdpRedirect(url)
    if (!target) throw new Error(t('auth.oidcErrors.unsafe_redirect'))
    window.location.assign(target)
  } catch (err) {
    errorMsg.value = t('profile.sso.linkError', { message: err.message })
  }
}

const unlink = async (identity) => {
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

onMounted(load)
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
            <strong>{{ pageName(identity) }}</strong>
            <span class="sso-links-protocol">{{ protocolLabel(identity) }}</span>
            <span v-if="identity.email" class="sso-links-email"> {{ t('profile.sso.linkedAs', { email: identity.email }) }}</span>
          </span>
          <button type="button" class="btn btn-secondary" @click="unlink(identity)">{{ t('profile.sso.unlink') }}</button>
        </li>
      </ul>
    </template>

    <template v-if="linkable.length > 0">
      <h4>{{ t('profile.sso.linkableHeading') }}</h4>
      <ul class="sso-links-list">
        <li v-for="page in linkable" :key="`${page.status_page_id}-${page.protocol}`" data-test="sso-linkable">
          <span>
            <strong>{{ page.page_name }}</strong>
            <span class="sso-links-protocol">{{ protocolLabel(page) }}</span>
          </span>
          <button type="button" class="btn btn-primary" @click="startLink(page)">{{ t('profile.sso.link') }}</button>
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
.sso-links-email,
.sso-links-protocol {
  color: var(--text-3);
}
.sso-links-protocol {
  margin-left: 6px;
  font-size: 0.8125rem;
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
