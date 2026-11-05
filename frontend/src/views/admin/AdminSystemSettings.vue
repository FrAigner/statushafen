<script setup>
import { ref, computed, onMounted } from 'vue'
import { OnyxButton, OnyxCheckbox } from 'sit-onyx'
import { useI18n } from 'vue-i18n'
import { useSystemSettings } from '../../composables/useSystemSettings'
import { useAuth } from '../../composables/useAuth'
import AppIcon from '../../components/AppIcon.vue'
import TranslationSettings from '../../components/TranslationSettings.vue'
import PageHeader from '../../components/PageHeader.vue'
import ListCard from '../../components/ListCard.vue'
import { usePageCrumbs } from '../../composables/usePageCrumbs'

// Systemeinstellungen: Schalter für die Self-Service-Bestellung
// und für den globalen E-Mail-Versand. Nur für Global Admins -
// die Route ist mit requiresGlobalAdmin geschützt, zusätzlich blendet die View
// den Inhalt per isGlobalAdmin aus (analog zu AdminLanguages.vue).

// Ein Eintrag pro Schalter: Feld im API-Vertrag und Präfix der i18n-Keys.
// Neue Schalter werden nur hier ergänzt (plus i18n), nicht als Kopie im Template.
const SECTIONS = [
  { field: 'orders_enabled', i18n: 'orders' },
  { field: 'email_enabled', i18n: 'email' },
  { field: 'selfmonitor_enabled', i18n: 'selfmonitor' },
]

const { settings, loadSettings, saveSettings } = useSystemSettings()
const { isGlobalAdmin } = useAuth()
const { t } = useI18n()
const breadcrumbs = usePageCrumbs(computed(() => t('systemSettings.title')), { global: true })

const form = ref({ orders_enabled: true, email_enabled: true, selfmonitor_enabled: false })
const alertMsg = ref('')
const alertType = ref('info')
const loading = ref(true)
const isSaving = ref(false)

const showAlert = (msg, type = 'info') => {
  alertMsg.value = msg
  alertType.value = type
}

const syncForm = () => {
  form.value = { ...settings.value }
}

onMounted(async () => {
  if (!isGlobalAdmin.value) {
    loading.value = false
    return
  }
  try {
    await loadSettings()
    syncForm()
  } catch (err) {
    showAlert(t('systemSettings.loadError') + err.message, 'error')
  } finally {
    loading.value = false
  }
})

// save schickt nur geänderte Schalter (der PUT speichert nur
// gesetzte Felder), damit das Audit-Log keine Scheinänderungen enthält. Ohne
// Änderung ist nichts zu speichern.
const save = async () => {
  const patch = {}
  for (const { field } of SECTIONS) {
    if (form.value[field] !== settings.value[field]) patch[field] = form.value[field]
  }
  if (Object.keys(patch).length === 0) {
    showAlert(t('systemSettings.saved'), 'success')
    return
  }
  isSaving.value = true
  try {
    await saveSettings(patch)
    syncForm()
    showAlert(t('systemSettings.saved'), 'success')
  } catch (err) {
    showAlert(t('systemSettings.saveError') + err.message, 'error')
  } finally {
    isSaving.value = false
  }
}
</script>

<template>
  <div>
    <div v-if="alertMsg" :class="`alert alert-${alertType} fade-in mb-16`">
      <span>{{ alertMsg }}</span>
      <button class="alert-close" @click="alertMsg = ''" :aria-label="t('common.dismissMessage')"><AppIcon name="x" :size="14" /></button>
    </div>

    <PageHeader :level="2" :title="t('systemSettings.title')" :subtitle="isGlobalAdmin ? t('systemSettings.description') : null" :breadcrumbs="breadcrumbs" />

    <p v-if="!isGlobalAdmin" class="alert alert-warning">{{ t('systemSettings.forbidden') }}</p>

    <template v-else>
      <p v-if="loading" role="status" aria-busy="true" class="text-muted">{{ t('common.loading') }}</p>

      <!-- Richtung C: Einstellungen in einer Karte, jeder Schalter ein Abschnitt (h4 unter dem Kartentitel). -->
      <ListCard v-else :title="t('systemSettings.cardTitle')" padded>
        <section v-for="section in SECTIONS" :key="section.field" :data-setting="section.field" class="setting">
          <h4 class="setting__title">{{ t(`systemSettings.${section.i18n}SectionTitle`) }}</h4>
          <p class="setting__text">{{ t(`systemSettings.${section.i18n}SectionDescription`) }}</p>
          <OnyxCheckbox v-model="form[section.field]" :label="t(`systemSettings.${section.i18n}EnabledLabel`)" />
        </section>
        <TranslationSettings @message="showAlert" />
        <div class="form-actions adm-actions-spaced">
          <OnyxButton class="save-settings" :label="isSaving ? t('common.saving') : t('systemSettings.saveButton')" @click="save" :disabled="isSaving" />
        </div>
      </ListCard>
    </template>
  </div>
</template>

<style scoped>
.setting { margin-bottom: var(--sp-4); }
.setting__title { margin: 0 0 var(--sp-1); font-size: var(--fs-md); font-weight: 600; border: 0; padding: 0; }
.setting__text { margin: 0 0 var(--sp-3); font-size: var(--fs-base); color: var(--text-2); }
</style>
