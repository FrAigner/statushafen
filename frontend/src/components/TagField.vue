<script setup lang="ts">
/*
  Direction C, operations pages: field group "Tags" with TagChips, label and
  "optional" like the other fields (style .form-field--manual). Replaces the
  checkbox list in the forms. As with the chips, `toggle` reports the tag ID
  (fits useTagToggle).
*/
import { useId } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Tag } from '../types/api'
import TagChips from './TagChips.vue'

// Stryker disable all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
defineProps<{
  label: string
  tags: Tag[]
  selectedIds: number[]
}>()
// Stryker restore all
// Stryker disable next-line all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
const emit = defineEmits<{ toggle: [tagId: number] }>()
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
