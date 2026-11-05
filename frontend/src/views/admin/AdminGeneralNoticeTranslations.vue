<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useGeneralNoticeTranslations } from '../../composables/useGeneralNoticeTranslations'
import { useAdminTranslationsPanel } from '../../composables/useAdminTranslationsPanel'
import { usePageCrumbs } from '../../composables/usePageCrumbs'
import TranslationsView from '../../components/TranslationsView.vue'

// Stryker disable next-line all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const props = defineProps({ noticeId: String })

const { t } = useI18n()

const {
  entity: notice, canCreate, canUpdate, canDelete, alertMsg, alertType, isEditing, formData, isSaving, confirmingId, deletingId,
  translations, availableLanguages,
  openNew, cancelEdit, saveTranslation, handleConfirm, handleDelete,
} = useAdminTranslationsPanel({
  entityId: computed(() => props.noticeId),
  apiSegment: 'notices/all',
  resource: 'notices',
  findInList: true,
  contentField: 'message',
  translationsApi: useGeneralNoticeTranslations(),
})

// Richtung C: Brotkrumen „Seite › Ankündigungen › Übersetzungen“.
const breadcrumbs = usePageCrumbs(computed(() => t('noticeTranslations.heading')), {
  extra: [{ label: t('notices.title'), to: '/admin/notices' }],
})
</script>

<template>
  <TranslationsView
    :form="formData"
    :title="t('noticeTranslations.heading')"
    :subtitle="notice ? `${t('noticeTranslations.entityLabel')} ${notice.Title}` : null"
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
    content-key="Message"
    content-form-key="message"
    rich-text
    :content-column-label="t('noticeTranslations.tableMessage')"
    :content-field-label="t('noticeTranslations.fieldMessage')"
    :content-placeholder="t('noticeTranslations.messagePlaceholder')"
    @new="openNew"
    @cancel="cancelEdit"
    @save="saveTranslation"
    @confirm="handleConfirm"
    @delete="handleDelete"
    @dismiss="alertMsg = ''"
  />
</template>
