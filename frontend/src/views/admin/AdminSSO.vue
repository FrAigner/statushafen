<script setup lang="ts">
import { ref, computed, watch } from 'vue'
// In the full test run v8 reports the getter line of this import as not
// executed (the lazy-loading test loads the view without rendering; same in
// AdminLanguages, AdminUsers and others), although AdminSSO.test.js renders it.
/* v8 ignore next */
import { OnyxButton } from 'sit-onyx'
import { useI18n } from 'vue-i18n'
import { useApi, isForbidden } from '../../composables/useApi'
import { useResourcePerms } from '../../composables/useResourcePerms'
import { useTenant } from '../../composables/useTenant'
import AppIcon from '../../components/AppIcon.vue'
import ForbiddenNotice from '../../components/ForbiddenNotice.vue'
import PageHeader from '../../components/PageHeader.vue'
import ListCard from '../../components/ListCard.vue'
import { usePageCrumbs } from '../../composables/usePageCrumbs'
import FormField from '../../components/FormField.vue'
import { confirmDialog } from '../../composables/useConfirm'
import { safeIdpRedirect } from '../../utils/safeIdpRedirect'
import type { SsoConfig, SsoForm } from '../../types/admin'

// OIDC configuration of the active status page. As in the other admin views
// the page comes from useTenant (selection in AdminLayout), not from the
// route; requests go through apiFetch (auth header, 403 detection) to
// /api/admin/pages/:page_id/sso with the backend's oidc_* fields.
//
// Role mapping from a claim (oidc_attribute_role) is deliberately not in the
// form: a page's IdP is controlled by that page's admin, and a role from its
// claim would be a privilege escalation that bypasses role management. New is
// the test login (test_login_enabled).
//
// SAML is gone; page SSO is OIDC only. If an older backend still sends saml_*
// fields, applyConfig only takes over the keys of emptyForm.

const { t, te } = useI18n()
const breadcrumbs = usePageCrumbs(computed(() => t('admin.sso.title')))
const { apiFetch } = useApi()
const { activeTenantId } = useTenant()
// Permissions of the sso resource. Without update the fields are locked, without delete the button is gone.
const { canUpdate, canDelete } = useResourcePerms('sso')

const emptyForm = (): SsoForm => ({
  oidc_enabled: false,
  oidc_discovery_url: '',
  oidc_client_id: '',
  oidc_client_secret: '',
  oidc_scopes: '',
  oidc_attribute_email: '',
  oidc_attribute_name: '',
  test_login_enabled: true,
})

const form = ref(emptyForm())
const acsUrl = ref('')
// Last loaded issuer. If it changes on save, the backend deletes the page's
// links; the page warns beforehand.
const loadedIssuer = ref('')
const oidcIssuerOf = (url: string) => url.trim().replace(/\/\.well-known\/openid-configuration$/, '')
const identityResetWarning = computed(() =>
  loadedIssuer.value !== '' && loadedIssuer.value !== oidcIssuerOf(form.value.oidc_discovery_url))
// GET never returns the secret, only whether one is stored. An empty field
// then keeps the stored one on save (backend).
const secretSet = ref(false)
const loading = ref(false)
const loadFailed = ref(false)
const forbidden = ref(false)
const isSaving = ref(false)
const alertMsg = ref('')
const alertType = ref('success')

const ssoUrl = () => `/api/admin/pages/${activeTenantId.value}/sso`

// Result of the test login, which the OIDC callback returns as
// ?oidc_test=<ok|code>&missing=email,name. Only known codes and claim keys
// are shown, missing claims under their configured name.
const testParams = new URLSearchParams(window.location.search)
const testCode = ref(testParams.get('oidc_test'))
const testMissing = (testParams.get('missing') || '').split(',').filter((k) => k === 'email' || k === 'name')
const testResult = computed(() => {
  if (!testCode.value) return null
  if (testCode.value !== 'ok') {
    const key = `auth.oidcErrors.${testCode.value}`
    return { type: 'error', msg: t('admin.sso.testFailed', { message: te(key) ? t(key) : t('auth.oidcErrors.unknown') }) }
  }
  const okMsg = t('admin.sso.testOk')
  if (testMissing.length === 0) return { type: 'success', msg: okMsg }
  const claims = testMissing.map((k) => form.value[`oidc_attribute_${k}` as keyof SsoForm] || k).join(', ')
  return { type: 'warning', msg: `${okMsg} ${t('admin.sso.testMissingClaims', { claims })}` }
})

const startTest = async () => {
  try {
    const { url }: { url: string } = await apiFetch(`${ssoUrl()}/test`, { method: 'POST' })
    const target = safeIdpRedirect(url)
    if (!target) throw new Error(t('auth.oidcErrors.unsafe_redirect'))
    window.location.assign(target)
  } catch (err) {
    if (isForbidden(err)) forbidden.value = true
    else showAlert(t('admin.sso.testStartError', { message: (err as Error).message }), 'error')
  }
}

const showAlert = (msg: string, type: string) => {
  alertMsg.value = msg
  alertType.value = type
}

// GET and PUT return all form fields (even without a stored config).
// The secret field always starts empty.
const applyConfig = (cfg: SsoConfig) => {
  form.value = Object.fromEntries(Object.keys(emptyForm()).map((k) => [k, cfg[k as keyof SsoForm]])) as unknown as SsoForm
  form.value.oidc_client_secret = ''
  acsUrl.value = cfg.oidc_acs_url
  secretSet.value = cfg.oidc_client_secret_set
  loadedIssuer.value = oidcIssuerOf(cfg.oidc_discovery_url)
}

// 403 -> permission notice instead of an error alert.
const handleError = (err: unknown, prefix: string) => {
  if (isForbidden(err)) forbidden.value = true
  else showAlert(prefix + (err as Error).message, 'error')
}

const loadConfig = async () => {
  forbidden.value = false
  loadFailed.value = false
  alertMsg.value = ''
  loading.value = true
  try {
    applyConfig(await apiFetch(ssoUrl()))
  } catch (err) {
    if (isForbidden(err)) forbidden.value = true
    else loadFailed.value = true
  } finally {
    loading.value = false
  }
}

watch(activeTenantId, (id) => {
  if (id) loadConfig()
}, { immediate: true })

const saveConfig = async () => {
  isSaving.value = true
  try {
    applyConfig(await apiFetch(ssoUrl(), { method: 'PUT', body: JSON.stringify(form.value) }))
    showAlert(t('admin.sso.saveSuccess'), 'success')
  } catch (err) {
    handleError(err, t('common.saveError'))
  } finally {
    isSaving.value = false
  }
}

const deleteConfig = async () => {
  // Build the URL before the dialog so that a page switch meanwhile does not hit the wrong page.
  const url = ssoUrl()
  if (!(await confirmDialog({ message: t('admin.sso.deleteConfirm') }))) return
  try {
    await apiFetch(url, { method: 'DELETE' })
    await loadConfig()
    showAlert(t('admin.sso.deleteSuccess'), 'success')
  } catch (err) {
    handleError(err, t('common.deleteError'))
  }
}
</script>

<template>
  <div class="admin-sso">
    <PageHeader :level="2" :title="t('admin.sso.title')" :breadcrumbs="breadcrumbs" />

    <div v-if="alertMsg" :class="`alert alert-${alertType} fade-in mb-16`">
      <span>{{ alertMsg }}</span>
      <button class="alert-close" @click="alertMsg = ''" :aria-label="t('common.dismissMessage')"><AppIcon name="x" :size="14" /></button>
    </div>

    <div v-if="testResult" :class="`alert alert-${testResult.type} mb-16`" data-test="sso-test-result">
      <span>{{ testResult.msg }}</span>
      <button class="alert-close" @click="testCode = null" :aria-label="t('common.dismissMessage')"><AppIcon name="x" :size="14" /></button>
    </div>

    <p v-if="!activeTenantId" class="text-muted">{{ t('admin.sso.noPage') }}</p>
    <ForbiddenNotice v-else-if="forbidden" />
    <div v-else-if="loading" class="text-muted" role="status" aria-busy="true">{{ t('common.loading') }}</div>
    <div v-else-if="loadFailed" class="alert alert-error" role="alert">{{ t('admin.sso.loadError') }}</div>

    <!-- Direction C: one card with a title per method, one shared form. -->
    <form novalidate v-else class="sso-form edit-form" @submit.prevent="saveConfig">
      <p v-if="!canUpdate" class="form-lock-hint" role="status">{{ t('common.readOnlyForm') }}</p>
      <fieldset class="form-lock" :disabled="!canUpdate">
      <ListCard :title="t('admin.sso.oidcHeading')" padded>
        <div class="form-group">
          <div class="checkbox-row">
            <input id="sso-enabled" v-model="form.oidc_enabled" type="checkbox" />
            <label for="sso-enabled">{{ t('admin.sso.enable') }}</label>
          </div>
        </div>

        <FormField v-slot="f" :label="t('admin.sso.discoveryUrl')" field-id="sso-discovery-url" :required="form.oidc_enabled" :hint="t('admin.sso.discoveryUrlHelp')">
          <input class="form-input" :required="f.required" :aria-describedby="f.describedby" :id="f.id" v-model="form.oidc_discovery_url" type="url" />
        </FormField>

        <FormField v-slot="f" :label="t('admin.sso.clientId')" field-id="sso-client-id" :required="form.oidc_enabled">
          <input class="form-input" :required="f.required" :aria-describedby="f.describedby" :id="f.id" v-model="form.oidc_client_id" type="text" />
        </FormField>

        <FormField v-slot="f" :label="t('admin.sso.clientSecret')" field-id="sso-client-secret" :required="form.oidc_enabled && !secretSet" :hint="secretSet ? t('admin.sso.secretSetHint') : null">
          <input class="form-input" :required="f.required" :aria-describedby="f.describedby"
            :id="f.id"
            v-model="form.oidc_client_secret"
            type="password"
            autocomplete="new-password"
            :placeholder="secretSet ? t('admin.sso.secretSetPlaceholder') : ''"
          />
        </FormField>

        <FormField v-slot="f" :label="t('admin.sso.scopes')" field-id="sso-scopes" :hint="t('admin.sso.scopesHelp')">
          <input class="form-input" :aria-describedby="f.describedby" :id="f.id" v-model="form.oidc_scopes" type="text" />
        </FormField>

        <FormField v-slot="f" :label="t('admin.sso.attributeEmail')" field-id="sso-attribute-email" :hint="t('admin.sso.attributeEmailHelp')">
          <input class="form-input" :aria-describedby="f.describedby" :id="f.id" v-model="form.oidc_attribute_email" type="text" />
        </FormField>

        <FormField v-slot="f" :label="t('admin.sso.attributeName')" field-id="sso-attribute-name" :hint="t('admin.sso.attributeNameHelp')">
          <input class="form-input" :aria-describedby="f.describedby" :id="f.id" v-model="form.oidc_attribute_name" type="text" />
        </FormField>

        <FormField v-slot="f" :label="t('admin.sso.acsUrl')" field-id="sso-acs-url" :hint="t('admin.sso.acsUrlHelp')">
          <input class="form-input" :aria-describedby="f.describedby" :id="f.id" :value="acsUrl" type="url" readonly />
        </FormField>

      </ListCard>

      <ListCard :title="t('admin.sso.testCardTitle')" padded>
        <div class="form-group">
          <div class="checkbox-row">
            <input id="sso-test-login" v-model="form.test_login_enabled" type="checkbox" />
            <label for="sso-test-login">{{ t('admin.sso.testLogin') }}</label>
          </div>
          <small class="adm-hint">{{ t('admin.sso.testLoginHelp') }}</small>
          <div v-if="form.test_login_enabled" class="sso-test-row">
            <OnyxButton class="sso-test" type="button" mode="outline" :label="t('admin.sso.testButton')" @click="startTest" />
            <small class="adm-hint">{{ t('admin.sso.testHint') }}</small>
          </div>
        </div>

      </ListCard>
      </fieldset>

      <div v-if="identityResetWarning" class="alert alert-warning mb-16" role="status" data-test="sso-identity-reset-warning">
        {{ t('admin.sso.identityResetWarning') }}
      </div>

      <div class="form-actions">
        <OnyxButton v-if="canDelete" class="sso-delete" type="button" mode="outline" color="danger" :label="t('admin.sso.deleteButton')" @click="deleteConfig" />
        <OnyxButton v-if="canUpdate" type="submit" :label="isSaving ? t('common.saving') : t('common.save')" :disabled="isSaving" />
      </div>
    </form>
  </div>
</template>

<style scoped>
.sso-form .form-actions {
  margin-top: var(--sp-4);
}

.sso-test-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  margin-top: 0.5rem;
}

</style>
