<script setup>
/*
  Datenexport (DSGVO Art. 15/20): Knopf auf der Profilseite, der
  GET /api/me/export abruft (Profil, Seitenrollen, Abos, Audit-Einträge als
  JSON) und die Antwort als Datei me-export.json speichert. Eigener Baustein
  wie AccountDeletion.vue, damit die Profilseite nur eine Zeile Einbindung teilt.
*/
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useApi } from '../composables/useApi'

const { t } = useI18n()
const { apiFetch } = useApi()

const isLoading = ref(false)
const errorMsg = ref('')

const download = async () => {
  isLoading.value = true
  errorMsg.value = ''
  try {
    const data = await apiFetch('/api/me/export')
    const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = 'me-export.json'
    link.click()
    URL.revokeObjectURL(url)
  } catch {
    errorMsg.value = t('dataExport.error')
  } finally {
    isLoading.value = false
  }
}
</script>

<template>
  <section class="edit-card data-export">
    <h3>{{ t('dataExport.title') }}</h3>
    <p class="data-export__text">{{ t('dataExport.description') }}</p>
    <div v-if="errorMsg" class="alert alert-warning mb-16" role="alert">
      <span>{{ errorMsg }}</span>
    </div>
    <button type="button" class="btn btn-secondary" data-test="data-export-button" :disabled="isLoading" @click="download">
      {{ t('dataExport.button') }}
    </button>
  </section>
</template>

<style scoped>
.data-export { margin-top: 24px; }
.data-export__text { color: var(--text-3); margin-bottom: 16px; }
</style>
