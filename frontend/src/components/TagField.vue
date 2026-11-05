<script setup>
/*
  Richtung C, Betriebsseiten: Feldgruppe „Tags“ mit TagChips,
  Beschriftung und „optional“ wie die übrigen Felder (Stil .form-field--manual).
  Ersetzt die Checkbox-Liste in den Formularen. Wie bei den Chips meldet
  `toggle` die Tag-ID (passt zu useTagToggle).
*/
import { useId } from 'vue'
import { useI18n } from 'vue-i18n'
import TagChips from './TagChips.vue'

// Stryker disable all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
defineProps({
  label: { type: String, required: true },
  tags: { type: Array, required: true },
  selectedIds: { type: Array, required: true },
})
// Stryker restore all
// Stryker disable next-line all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const emit = defineEmits(['toggle'])
const { t } = useI18n()
const labelId = `tag-field-${useId()}`
</script>

<template>
  <div class="form-field form-field--manual" role="group" :aria-labelledby="labelId">
    <div class="form-field__labelrow">
      <span :id="labelId" class="form-field__label">{{ label }}</span>
      <span class="form-field__optional">{{ t('patterns.optional') }}</span>
    </div>
    <TagChips :tags="tags" :selected-ids="selectedIds" @toggle="id => emit('toggle', id)" />
  </div>
</template>
