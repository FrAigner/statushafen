<script setup>
import { computed } from 'vue'
import { OnyxButton } from 'sit-onyx'
import { useI18n } from 'vue-i18n'
import { useIncidentTranslations } from '../../composables/useIncidentTranslations'
import { useAdminTranslationsPanel } from '../../composables/useAdminTranslationsPanel'
import { useFormPanel } from '../../composables/useFormPanel'
import { usePageCrumbs } from '../../composables/usePageCrumbs'
import AppIcon from '../../components/AppIcon.vue'
import PageHeader from '../../components/PageHeader.vue'
import ListCard from '../../components/ListCard.vue'
import FormPanel from '../../components/FormPanel.vue'
import FormField from '../../components/FormField.vue'
import RichTextEditor from '../../components/RichTextEditor.vue'
import ResponsiveTable from '../../components/ResponsiveTable.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import RowMenu from '../../components/RowMenu.vue'
import EmptyState from '../../components/EmptyState.vue'

// Richtung C: Seitenkopf mit Brotkrumen „Seite › Vorfälle › Vorfall“,
// Übersetzungen in einer Karte mit Zeilenmenü, neue Übersetzung als Seitenkarte.

// Stryker disable next-line all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const props = defineProps({ incidentId: String })

const { t } = useI18n()
const { formEl, errors, focusForm, clearErrors, clearError, validateRequired } = useFormPanel()

const {
  entity: incident, canCreate, canUpdate, canDelete, alertMsg, alertType, isEditing, formData, isSaving, confirmingId, deletingId,
  translations, availableLanguages,
  openNew: openNewPanel, cancelEdit: cancelPanel, saveTranslation, handleConfirm, handleDelete,
} = useAdminTranslationsPanel({
  entityId: computed(() => props.incidentId),
  apiSegment: 'incidents',
  resource: 'incidents',
  contentField: 'message',
  translationsApi: useIncidentTranslations(),
})

const incidentTitle = computed(() => incident.value?.Title ?? `#${props.incidentId}`)
const breadcrumbs = usePageCrumbs(() => incidentTitle.value, {
  extra: () => [{ label: t('incidentTranslations.breadcrumbList'), to: '/admin/incidents' }],
})
const subtitle = computed(() => incident.value ? t('incidentTranslations.subtitle', { name: incident.value.Title }) : null)

const columns = computed(() => [
  { key: 'LanguageCode', label: t('translations.tableLanguage') },
  { key: 'Title', label: t('translations.tableTitle') },
  { key: 'Message', label: t('incidentTranslations.tableMessage') },
  { key: 'Status', label: t('translations.tableStatus') },
])

const openNew = () => { clearErrors(); openNewPanel(); focusForm() }
const cancelEdit = () => { clearErrors(); cancelPanel() }

// Pflichtfelder: Fehler am Feld, Fokus aufs erste Feld.
const submit = async () => {
  const required = (key, value, labelKey) =>
    ({ key, value, message: t('patterns.fieldRequired', { field: t(labelKey) }) })
  const valid = await validateRequired([
    required('language_code', formData.value.language_code, 'translations.fieldLanguage'),
    required('title', formData.value.title, 'translations.fieldTitle'),
    required('message', formData.value.message, 'incidentTranslations.fieldMessage'),
  ])
  if (valid) await saveTranslation()
}

const isConfirmed = (trans) => trans.Status === 'confirmed'
const statusLabel = (trans) => isConfirmed(trans) ? t('translations.statusConfirmed') : t('translations.statusDraft')

// Zeilenmenü: Bestätigen nur für Entwürfe, Löschen immer.
const menuItems = (trans) => [
  ...(isConfirmed(trans) || !canUpdate.value ? [] : [{
    key: 'confirm', icon: 'check', disabled: confirmingId.value === trans.ID,
    label: confirmingId.value === trans.ID ? t('translations.confirming') : t('translations.confirm'),
  }]),
  ...(canDelete.value ? [{ key: 'delete', label: deletingId.value === trans.ID ? t('common.deleting') : t('common.delete'), icon: 'trash', danger: true, disabled: deletingId.value === trans.ID }] : []),
]
const onMenuSelect = (trans, key) => {
  if (key === 'confirm') handleConfirm(trans.ID)
  else handleDelete(trans.ID)
}
</script>

<template>
  <div>
    <div v-if="alertMsg" :class="`alert alert-${alertType} fade-in mb-16`">
      <span>{{ alertMsg }}</span>
      <button class="alert-close" @click="alertMsg = ''" :aria-label="t('common.dismissMessage')"><AppIcon name="x" :size="14" /></button>
    </div>

    <PageHeader :level="2" :title="t('incidentTranslations.heading')" :subtitle="subtitle" :breadcrumbs="breadcrumbs">
      <template v-if="canCreate && !isEditing" #actions>
        <OnyxButton :label="t('translations.newButton')" @click="openNew" />
      </template>
    </PageHeader>

    <div class="split" :class="{ 'split--open': isEditing }">
      <ListCard :title="t('incidentTranslations.cardTitle')" class="split__main">
        <ResponsiveTable v-if="translations.length" data-table :columns="columns" :items="translations" row-key="ID">
          <template #cell-LanguageCode="{ item }"><code>{{ item.LanguageCode }}</code></template>
          <template #cell-Title="{ item }"><strong class="component-name__title">{{ item.Title }}</strong></template>
          <template #cell-Status="{ item }">
            <StatusBadge :status="isConfirmed(item) ? 'operational' : null" :label="statusLabel(item)" />
          </template>
          <template v-if="canUpdate || canDelete" #actions="{ item }">
            <RowMenu :label="t('patterns.menuLabel', { name: item.LanguageCode })" :items="menuItems(item)" @select="key => onMenuSelect(item, key)" />
          </template>
        </ResponsiveTable>
        <EmptyState v-else :title="t('translations.empty')" icon="globe" />
      </ListCard>

      <FormPanel v-if="isEditing" :title="t('translations.newFormTitle')" @close="cancelEdit">
        <form ref="formEl" novalidate class="edit-form" @submit.prevent="submit">
          <FormField v-slot="f" required field-id="lang-select" :label="t('translations.fieldLanguage')" :error="errors.language_code || null">
            <select :id="f.id" v-model="formData.language_code" :required="f.required" :aria-invalid="f.invalid" :aria-describedby="f.describedby" @change="clearError('language_code')">
              <option value="">{{ t('translations.selectLanguagePlaceholder') }}</option>
              <option v-for="l in availableLanguages()" :key="l.Code" :value="l.Code">{{ l.Name }} ({{ l.Code }})</option>
            </select>
          </FormField>
          <FormField v-slot="f" required field-id="title-input" :label="t('translations.fieldTitle')" :error="errors.title || null">
            <input :id="f.id" v-model="formData.title" type="text" :required="f.required" :aria-invalid="f.invalid" :aria-describedby="f.describedby" :placeholder="t('translations.titlePlaceholder')" @input="clearError('title')">
          </FormField>
          <FormField v-slot="f" required field-id="message-input" :label="t('incidentTranslations.fieldMessage')" :error="errors.message || null">
            <RichTextEditor :id="f.id" v-model="formData.message" :required="f.required" :aria-invalid="f.invalid" :aria-describedby="f.describedby" :placeholder="t('incidentTranslations.messagePlaceholder')" @input="clearError('message')" />
          </FormField>
          <div class="form-actions">
            <OnyxButton :label="t('common.cancel')" mode="outline" type="button" @click="cancelEdit" :disabled="isSaving" />
            <OnyxButton :label="isSaving ? t('common.saving') : t('common.save')" type="submit" :disabled="isSaving" />
          </div>
        </form>
      </FormPanel>
    </div>
  </div>
</template>
