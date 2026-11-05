<script setup>
import { ref, computed, onMounted } from 'vue'
import { OnyxButton, OnyxInput, OnyxSelect } from 'sit-onyx'
import { useI18n } from 'vue-i18n'
import { useTranslationSettings } from '../composables/useTranslationSettings'

// Abschnitt „Automatische Übersetzung“ in den Systemeinstellungen.
// Dienst (Standard des Betreibers, eigener LibreTranslate-Server, DeepL),
// Adresse und Schlüssel. Der Schlüssel ist nur schreibbar: leer lassen = behalten,
// Haken „entfernen“ = löschen. Die Meldungen zeigt die umgebende Ansicht (emit message).
// Stryker disable next-line all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const emit = defineEmits(['message'])
const { t } = useI18n()
const { settings, load, save, test } = useTranslationSettings()

const form = ref({ provider: '', url: '', apiKey: '', clearKey: false })
const busy = ref(false)
const testResult = ref('')

const options = computed(() => [
  { value: '', label: t('translationSettings.providerDefault') },
  { value: 'libretranslate', label: t('translationSettings.providerLibre') },
  { value: 'deepl', label: t('translationSettings.providerDeepl') },
])
const showUrl = computed(() => form.value.provider === 'libretranslate')

const sync = () => {
  form.value = { provider: settings.value.provider, url: settings.value.url, apiKey: '', clearKey: false }
}

// Zeile unter dem Titel: welcher Dienst gerade wirkt und ob er eingerichtet ist.
const statusText = computed(() => {
  if (!settings.value.configured) return t('translationSettings.statusNotConfigured')
  return t('translationSettings.statusActive', { provider: t(`translationSettings.name.${settings.value.effective_provider}`) })
})

onMounted(async () => {
  try {
    await load()
    sync()
  } catch (err) {
    emit('message', t('translationSettings.loadError') + err.message, 'error')
  }
})

const onSave = async () => {
  const patch = { provider: form.value.provider }
  if (showUrl.value) patch.url = form.value.url
  if (form.value.clearKey) patch.api_key = ''
  else if (form.value.apiKey) patch.api_key = form.value.apiKey
  busy.value = true
  try {
    await save(patch)
    sync()
    emit('message', t('translationSettings.saved'), 'success')
  } catch (err) {
    emit('message', t('translationSettings.saveError') + err.message, 'error')
  } finally {
    busy.value = false
  }
}

const onTest = async () => {
  busy.value = true
  testResult.value = ''
  try {
    const res = await test()
    testResult.value = t('translationSettings.testOk', { provider: t(`translationSettings.name.${res.provider}`), sample: res.sample })
  } catch (err) {
    emit('message', t('translationSettings.testError') + err.message, 'error')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <section class="setting" data-setting="translation">
    <h4 class="setting__title">{{ t('translationSettings.title') }}</h4>
    <p class="setting__text">{{ t('translationSettings.description') }}</p>
    <p class="setting__text" data-test="translation-status">{{ statusText }}</p>

    <OnyxSelect v-model="form.provider" :label="t('translationSettings.providerLabel')" :options="options" />
    <p v-if="form.provider === 'deepl'" class="setting__text" data-test="deepl-info">{{ t('translationSettings.deeplInfo') }}</p>
    <p v-if="form.provider === ''" class="setting__text">{{ settings.env_url_set ? t('translationSettings.defaultInfo') : t('translationSettings.defaultMissing') }}</p>

    <OnyxInput v-if="showUrl" v-model="form.url" :label="t('translationSettings.urlLabel')" :placeholder="t('translationSettings.urlPlaceholder')" />
    <template v-if="form.provider !== ''">
      <OnyxInput v-model="form.apiKey" type="password" autocomplete="off" :label="t('translationSettings.keyLabel')" :placeholder="settings.api_key_set ? t('translationSettings.keyKeep') : ''" />
      <label v-if="settings.api_key_set" class="setting__clear">
        <input v-model="form.clearKey" type="checkbox" data-test="clear-key">
        <span>{{ t('translationSettings.keyClear') }}</span>
      </label>
    </template>

    <div class="form-actions adm-actions-spaced">
      <OnyxButton class="save-translation" :label="t('translationSettings.saveButton')" :disabled="busy" @click="onSave" />
      <OnyxButton class="test-translation" mode="outline" :label="t('translationSettings.testButton')" :disabled="busy" @click="onTest" />
    </div>
    <p v-if="testResult" class="setting__text" role="status" data-test="translation-test-result">{{ testResult }}</p>
  </section>
</template>

<style scoped>
.setting { margin-bottom: var(--sp-4); }
.setting__title { margin: 0 0 var(--sp-1); font-size: var(--fs-md); font-weight: 600; border: 0; padding: 0; }
.setting__text { margin: 0 0 var(--sp-3); font-size: var(--fs-base); color: var(--text-2); }
.setting__clear { display: flex; align-items: center; gap: var(--sp-2); margin-top: var(--sp-2); font-size: var(--fs-sm); }
</style>
