<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useComponentTranslations } from '../../composables/useComponentTranslations'
import { useAdminTranslationsPanel } from '../../composables/useAdminTranslationsPanel'
import { usePageCrumbs } from '../../composables/usePageCrumbs'
import TranslationsView from '../../components/TranslationsView.vue'

// Stryker disable next-line all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const props = defineProps({ componentId: String })

const { t } = useI18n()

const {
  entity: component, canCreate, canUpdate, canDelete, alertMsg, alertType, isEditing, formData, isSaving, confirmingId, deletingId,
  translations, availableLanguages,
  openNew, cancelEdit, saveTranslation, handleConfirm, handleDelete,
} = useAdminTranslationsPanel({
  entityId: computed(() => props.componentId),
  apiSegment: 'components',
  resource: 'components',
  findInList: true,
  contentField: 'description',
  translationsApi: useComponentTranslations(),
})

// Richtung C: Brotkrumen „Seite › Komponenten › Übersetzungen“.
const breadcrumbs = usePageCrumbs(computed(() => t('componentTranslations.heading')), {
  extra: [{ label: t('components.title'), to: '/admin/components' }],
})
</script>

<template>
  <TranslationsView
    :form="formData"
    :title="t('componentTranslations.heading')"
    :subtitle="component ? `${t('componentTranslations.entityLabel')} ${component.Name}` : null"
    :breadcrumbs="breadcrumbs"
    :alert-msg="alertMsg"
    :alert-type="alertType"
    :can-create="canCreate"
    :can-update="canUpdate"
    :can-delete="canDelete"
    :is-editing="isEditing"
    :is-saving="isSaving"
    :translations="translations"
    :languages="availableLanguages()"
    :confirming-id="confirmingId"
    :deleting-id="deletingId"
    content-key="Description"
    content-form-key="description"
    :content-column-label="t('componentTranslations.tableDescription')"
    :content-field-label="t('componentTranslations.fieldDescription')"
    :content-placeholder="t('componentTranslations.descriptionPlaceholder')"
    @new="openNew"
    @cancel="cancelEdit"
    @save="saveTranslation"
    @confirm="handleConfirm"
    @delete="handleDelete"
    @dismiss="alertMsg = ''"
  />
</template>
