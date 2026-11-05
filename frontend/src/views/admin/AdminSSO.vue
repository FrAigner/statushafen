<script setup>
import { ref, computed, watch } from 'vue'
// v8 meldet im vollen Testlauf die Getter-Zeile dieses Imports als nicht
// ausgeführt (Lazy-Loading-Test lädt die View ohne Rendern; gleiches Bild in
// AdminLanguages, AdminUsers u. a.), obwohl AdminSSO.test.js sie rendert.
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

// OIDC-Konfiguration der aktiven Statusseite. Die Seite kommt wie
// in den übrigen Admin-Views aus useTenant (Auswahl im AdminLayout), nicht aus
// der Route; Requests laufen über apiFetch (Auth-Header, 403-Erkennung) an
// /api/admin/pages/:page_id/sso mit den oidc_*-Feldern des Backends.
//
// Das Rollen-Mapping aus einem Claim (oidc_attribute_role) ist
// bewusst nicht im Formular: Den IdP einer Seite kontrolliert deren Admin, eine
// Rolle aus seinem Claim wäre eine Rechteausweitung an der Rollenverwaltung
// vorbei. Neu ist die Testanmeldung (test_login_enabled).
//
// Abschnitt SAML 2.0 mit den saml_*-Feldern. Entity-ID, ACS-URL,
// SP-Metadaten-URL und SP-Zertifikat liefert das Backend nur zur Anzeige (sie
// gehen nicht zurück an PUT); Schlüssel und Zertifikat entstehen beim ersten
// Speichern. Auch für SAML gibt es kein Rollen-Mapping.

const { t, te } = useI18n()
const breadcrumbs = usePageCrumbs(computed(() => t('admin.sso.title')))
const { apiFetch } = useApi()
const { activeTenantId } = useTenant()
// Rechte der Ressource sso. Ohne Ändern sind die Felder gesperrt, ohne Löschen entfällt der Knopf.
const { canUpdate, canDelete } = useResourcePerms('sso')

const emptyForm = () => ({
  oidc_enabled: false,
  oidc_discovery_url: '',
  oidc_client_id: '',
  oidc_client_secret: '',
  oidc_scopes: '',
  oidc_attribute_email: '',
  oidc_attribute_name: '',
  test_login_enabled: true,
  // Von den Startwerten zählen nur die Schlüssel (applyConfig,
  // PUT); das Formular erscheint erst nach dem Laden, die Werte kommen dann
  // vom Backend. Andere Startwerte wären äquivalente Mutanten.
  // Stryker disable next-line BooleanLiteral: Startwert äquivalent, s. o.
  saml_enabled: false,
  // Stryker disable next-line StringLiteral: Startwert äquivalent, s. o.
  saml_idp_metadata_url: '',
  // Stryker disable next-line StringLiteral: Startwert äquivalent, s. o.
  saml_idp_metadata_xml: '',
  // Stryker disable next-line StringLiteral: Startwert äquivalent, s. o.
  saml_attribute_email: '',
  // Stryker disable next-line StringLiteral: Startwert äquivalent, s. o.
  saml_attribute_name: '',
  // Stryker disable next-line BooleanLiteral: Startwert äquivalent, s. o.
  saml_allow_unspecified_nameid: false,
})

const form = ref(emptyForm())
const acsUrl = ref('')
// Nur lesende SAML-Werte für den IdP, gesetzt von applyConfig.
const samlInfo = ref({})
// Zuletzt geladene Vertrauensanker. Wechselt beim
// Speichern der OIDC-Issuer oder die SAML-Metadaten (Quelle, Entity-ID,
// Zertifikat), löscht das Backend die Verknüpfungen der Seite. Die Seite warnt
// vorher; ob Entity-ID oder Zertifikat im XML wirklich wechseln, prüft erst
// das Backend, deshalb warnt jede inhaltliche Änderung des XML.
const loadedAnchors = ref({})
const oidcIssuerOf = (url) => url.trim().replace(/\/\.well-known\/openid-configuration$/, '')
const samlSourceOf = (url, xml) => `${url.trim()}|${xml.trim()}`
const identityResetWarning = computed(() => {
  const loaded = loadedAnchors.value
  const oidcChanged = loaded.oidc !== '' && loaded.oidc !== oidcIssuerOf(form.value.oidc_discovery_url)
  const samlChanged = loaded.saml !== '|' && loaded.saml !== samlSourceOf(form.value.saml_idp_metadata_url, form.value.saml_idp_metadata_xml)
  return oidcChanged || samlChanged
})
// GET liefert das Secret nie aus, nur ob eins gespeichert ist. Ein leeres Feld
// behält dann beim Speichern das gespeicherte (Backend).
const secretSet = ref(false)
const loading = ref(false)
const loadFailed = ref(false)
const forbidden = ref(false)
const isSaving = ref(false)
const alertMsg = ref('')
const alertType = ref('success')

const ssoUrl = () => `/api/admin/pages/${activeTenantId.value}/sso`

// Ergebnis der Testanmeldung, das der OIDC-Callback als
// ?oidc_test=<ok|code>&missing=email,name zurückgibt. Nur bekannte Codes und
// Claim-Schlüssel werden angezeigt, fehlende Claims unter ihrem konfigurierten
// Namen. Der SAML-ACS hängt sso=saml an; dann gelten die
// SAML-Attributnamen und -Texte.
const testParams = new URLSearchParams(window.location.search)
const testCode = ref(testParams.get('oidc_test'))
const testProtocol = testParams.get('sso') === 'saml' ? 'saml' : 'oidc'
const testMissing = (testParams.get('missing') || '').split(',').filter((k) => k === 'email' || k === 'name')
const testResult = computed(() => {
  if (!testCode.value) return null
  if (testCode.value !== 'ok') {
    const key = `auth.oidcErrors.${testCode.value}`
    return { type: 'error', msg: t('admin.sso.testFailed', { message: te(key) ? t(key) : t('auth.oidcErrors.unknown') }) }
  }
  const okMsg = t(testProtocol === 'saml' ? 'admin.sso.samlTestOk' : 'admin.sso.testOk')
  if (testMissing.length === 0) return { type: 'success', msg: okMsg }
  const claims = testMissing.map((k) => form.value[`${testProtocol}_attribute_${k}`] || k).join(', ')
  const missingKey = testProtocol === 'saml' ? 'admin.sso.testMissingAttributes' : 'admin.sso.testMissingClaims'
  return { type: 'warning', msg: `${okMsg} ${t(missingKey, { claims })}` }
})

// endpoint: test (OIDC) oder saml-test.
const startTest = async (endpoint) => {
  try {
    const { url } = await apiFetch(`${ssoUrl()}/${endpoint}`, { method: 'POST' })
    const target = safeIdpRedirect(url)
    if (!target) throw new Error(t('auth.oidcErrors.unsafe_redirect'))
    window.location.assign(target)
  } catch (err) {
    if (isForbidden(err)) forbidden.value = true
    else showAlert(t('admin.sso.testStartError', { message: err.message }), 'error')
  }
}

const showAlert = (msg, type) => {
  alertMsg.value = msg
  alertType.value = type
}

// GET und PUT liefern alle Formularfelder (auch ohne gespeicherte Config).
// Das Secret-Feld startet immer leer.
const applyConfig = (cfg) => {
  form.value = Object.fromEntries(Object.keys(emptyForm()).map((k) => [k, cfg[k]]))
  form.value.oidc_client_secret = ''
  acsUrl.value = cfg.oidc_acs_url
  secretSet.value = cfg.oidc_client_secret_set
  loadedAnchors.value = {
    oidc: oidcIssuerOf(cfg.oidc_discovery_url),
    saml: samlSourceOf(cfg.saml_idp_metadata_url, cfg.saml_idp_metadata_xml),
  }
  samlInfo.value = {
    entityId: cfg.saml_entity_id,
    metadataUrl: cfg.saml_metadata_url,
    acsUrl: cfg.saml_acs_url,
    certificate: cfg.saml_sp_certificate,
  }
}

// 403 -> Berechtigungsmeldung statt Fehler-Alert.
const handleError = (err, prefix) => {
  if (isForbidden(err)) forbidden.value = true
  else showAlert(prefix + err.message, 'error')
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
  // URL vor dem Dialog bilden, damit ein Seitenwechsel währenddessen nicht die falsche Seite trifft.
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

    <!-- Richtung C: je Verfahren eine Karte mit Titel, ein gemeinsames Formular. -->
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

      <ListCard :title="t('admin.sso.samlHeading')" padded>
        <p class="text-muted">{{ t('admin.sso.samlIntro') }}</p>
        <div class="form-group">
          <div class="checkbox-row">
            <input id="sso-saml-enabled" v-model="form.saml_enabled" type="checkbox" />
            <label for="sso-saml-enabled">{{ t('admin.sso.samlEnable') }}</label>
          </div>
        </div>

        <FormField v-slot="f" :label="t('admin.sso.samlMetadataUrl')" field-id="sso-saml-metadata-url" :required="form.saml_enabled && !form.saml_idp_metadata_xml" :hint="t('admin.sso.samlMetadataUrlHelp')">
          <input class="form-input" :required="f.required" :aria-describedby="f.describedby"
            :id="f.id"
            v-model="form.saml_idp_metadata_url"
            type="url"
          />
        </FormField>

        <FormField v-slot="f" :label="t('admin.sso.samlMetadataXml')" field-id="sso-saml-metadata-xml" :hint="t('admin.sso.samlMetadataXmlHelp')">
          <textarea :aria-describedby="f.describedby" :id="f.id" v-model="form.saml_idp_metadata_xml" rows="4" spellcheck="false"></textarea>
        </FormField>

        <div class="form-group">
          <div class="checkbox-row">
            <input id="sso-saml-allow-unspecified" v-model="form.saml_allow_unspecified_nameid" type="checkbox" />
            <label for="sso-saml-allow-unspecified">{{ t('admin.sso.samlAllowUnspecified') }}</label>
          </div>
          <small class="adm-hint">{{ t('admin.sso.samlAllowUnspecifiedHelp') }}</small>
          <div v-if="form.saml_allow_unspecified_nameid" class="alert alert-warning sso-warning" data-test="saml-unspecified-warning">
            {{ t('admin.sso.samlAllowUnspecifiedWarning') }}
          </div>
        </div>

        <FormField v-slot="f" :label="t('admin.sso.samlAttributeEmail')" field-id="sso-saml-attribute-email">
          <input class="form-input" :aria-describedby="f.describedby" :id="f.id" v-model="form.saml_attribute_email" type="text" />
        </FormField>

        <FormField v-slot="f" :label="t('admin.sso.samlAttributeName')" field-id="sso-saml-attribute-name" :hint="t('admin.sso.samlAttributeHelp')">
          <input class="form-input" :aria-describedby="f.describedby" :id="f.id" v-model="form.saml_attribute_name" type="text" />
        </FormField>

        <FormField v-slot="f" :label="t('admin.sso.samlEntityId')" field-id="sso-saml-entity-id">
          <input class="form-input" :aria-describedby="f.describedby" :id="f.id" :value="samlInfo.entityId" type="url" readonly />
        </FormField>

        <FormField v-slot="f" :label="t('admin.sso.samlAcsUrl')" field-id="sso-saml-acs-url" :hint="t('admin.sso.samlSpHelp')">
          <input class="form-input" :aria-describedby="f.describedby" :id="f.id" :value="samlInfo.acsUrl" type="url" readonly />
        </FormField>

        <template v-if="samlInfo.certificate">
          <p class="form-group">
            <a class="sso-saml-metadata-link" :href="samlInfo.metadataUrl" target="_blank" rel="noopener noreferrer">{{ t('admin.sso.samlMetadataLink') }}</a>
          </p>
          <FormField v-slot="f" :label="t('admin.sso.samlCertificate')" field-id="sso-saml-certificate" :hint="t('admin.sso.samlCertificateHelp')">
            <textarea :id="f.id" :aria-describedby="f.describedby" :value="samlInfo.certificate" rows="4" readonly spellcheck="false"></textarea>
          </FormField>
        </template>
        <p v-else class="text-muted">{{ t('admin.sso.samlMetadataPending') }}</p>

      </ListCard>

      <ListCard :title="t('admin.sso.testCardTitle')" padded>
        <div class="form-group">
          <div class="checkbox-row">
            <input id="sso-test-login" v-model="form.test_login_enabled" type="checkbox" />
            <label for="sso-test-login">{{ t('admin.sso.testLogin') }}</label>
          </div>
          <small class="adm-hint">{{ t('admin.sso.testLoginHelp') }}</small>
          <div v-if="form.test_login_enabled" class="sso-test-row">
            <OnyxButton class="sso-test" type="button" mode="outline" :label="t('admin.sso.testButton')" @click="startTest('test')" />
            <OnyxButton class="sso-saml-test" type="button" mode="outline" :label="t('admin.sso.samlTestButton')" @click="startTest('saml-test')" />
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
.admin-sso {
  max-width: 760px;
}

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

.sso-warning {
  margin-top: 0.5rem;
}

textarea {
  width: 100%;
  font-family: monospace;
  font-size: 0.8125rem;
}
</style>
