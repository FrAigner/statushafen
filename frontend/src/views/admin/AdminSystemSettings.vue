<script setup lang="ts">
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
import type { SystemSettings } from '../../types/api'

// System settings: switches for self-service ordering and for global email
// sending. Global admins only - the route is guarded by requiresGlobalAdmin,
// and the view additionally hides its content via isGlobalAdmin (like AdminLanguages.vue).

// One entry per switch: field in the API contract and prefix of the i18n keys.
// New switches are only added here (plus i18n), not copied into the template.
const SECTIONS: { field: keyof SystemSettings, i18n: string }[] = [
  { field: 'orders_enabled', i18n: 'orders' },
  { field: 'email_enabled', i18n: 'email' },
  { field: 'selfmonitor_enabled', i18n: 'selfmonitor' },
]

const { settings, loadSettings, saveSettings } = useSystemSettings()
const { isGlobalAdmin } = useAuth()
const { t } = useI18n()
const breadcrumbs = usePageCrumbs(computed(() => t('systemSettings.title')), { global: true })

const form = ref<SystemSettings>({ orders_enabled: true, email_enabled: true, selfmonitor_enabled: false })
const alertMsg = ref('')
const alertType = ref('info')
const loading = ref(true)
const isSaving = ref(false)

const showAlert = (msg: string, type = 'info') => {
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
    showAlert(t('systemSettings.loadError') + (err as Error).message, 'error')
  } finally {
    loading.value = false
  }
})

// save only sends changed switches (the PUT stores only the fields that are
// set), so the audit log contains no phantom changes. Without a change there
// is nothing to save.
const save = async () => {
  const patch: Partial<SystemSettings> = {}
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
    showAlert(t('systemSettings.saveError') + (err as Error).message, 'error')
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

      <!-- Direction C: settings in one card, each switch a section (h4 below the card title). -->
      <ListCard v-else :title="t('systemSettings.cardTitle')" padded>
        <section v-for="section in SECTIONS" :id="`setting-${section.field}`" :key="section.field" :data-setting="section.field" class="setting">
          <h4 class="setting__title">{{ t(`systemSettings.${section.i18n}SectionTitle`) }}</h4>
          <p class="setting__text">{{ t(`systemSettings.${section.i18n}SectionDescription`) }}</p>
          <OnyxCheckbox v-model="form[section.field]" :value="section.field" :label="t(`systemSettings.${section.i18n}EnabledLabel`)" />
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
