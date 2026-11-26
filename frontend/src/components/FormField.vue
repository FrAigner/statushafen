<script setup lang="ts">
/*
  Redesign stage 3: form field pattern. Label above the field,
  "*" for required or "optional", help on the field, below it a
  hint OR error line with an icon. Replaces .form-group label and the
  mixed Onyx labels on native fields.

  The field itself comes via the default slot and receives everything it
  needs as slot props, so the pattern does not depend on the markup:

    <FormField v-slot="f" label="Name" required :error="errors.name" help="components.name">
      <input :id="f.id" v-model="name" :required="f.required"
             :aria-describedby="f.describedby" :aria-invalid="f.invalid">
    </FormField>

  - The asterisk only comes from the presentation, never from the label text
    (rule, i18n/requiredMarker.test.js); it is hidden from
    screen readers, `required` on the field announces it.
  - An error replaces the hint, sits in a role="alert" line and sets
    aria-invalid; both lines are linked to the field via aria-describedby.
  - `help` is a help key for v-help ("area.field"); the help is attached
    to the field area (hover/focus, Escape closes), without an extra icon.
  - On submit the caller makes sure focus jumps to the first
    invalid field (umsetzung.md, accessibility).
*/
import { computed, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from './AppIcon.vue'
import vHelp from '../directives/vHelp'

// Stryker disable all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
const props = withDefaults(defineProps<{
  label: string
  required?: boolean
  hint?: string | null
  error?: string | null
  help?: string | null
  // Own field ID, if the field already dictates it (label for).
  fieldId?: string | null
}>(), {
  required: false,
  hint: null,
  error: null,
  help: null,
  fieldId: null,
})
// Stryker restore all

const { t } = useI18n()
const uid = useId()
const id = computed(() => props.fieldId ?? `ff-${uid}`)
const hintId = computed(() => `${id.value}-hint`)
const errorId = computed(() => `${id.value}-error`)
const invalid = computed(() => !!props.error)

// An error replaces the hint; without either there is no aria-describedby.
const describedby = computed(() => {
  if (props.error) return errorId.value
  if (props.hint) return hintId.value
  return undefined
})

const slotProps = computed(() => ({
  id: id.value,
  describedby: describedby.value,
  invalid: invalid.value || undefined,
  required: props.required || undefined,
}))
</script>

<template>
  <div class="form-field" :class="{ 'form-field--invalid': invalid }">
    <div class="form-field__labelrow">
      <label class="form-field__label" :for="id">{{ label }}</label>
      <span v-if="required" class="form-field__required" aria-hidden="true">*</span>
      <span v-else class="form-field__optional">{{ t('patterns.optional') }}</span>
    </div>

    <div v-if="help" v-help="help" class="form-field__control"><slot v-bind="slotProps" /></div>
    <div v-else class="form-field__control"><slot v-bind="slotProps" /></div>

    <p v-if="error" :id="errorId" class="form-field__message form-field__message--error" role="alert">
      <AppIcon name="alert-circle" :size="14" />{{ error }}
    </p>
    <p v-else-if="hint" :id="hintId" class="form-field__message">
      <AppIcon name="info" :size="14" />{{ hint }}
    </p>
  </div>
</template>

<style scoped>
.form-field { display: flex; flex-direction: column; gap: var(--sp-1); margin-bottom: var(--sp-4); min-inline-size: 0; }
.form-field__labelrow { display: flex; align-items: baseline; gap: var(--sp-1); }
.form-field__label { font-size: var(--fs-sm); font-weight: 600; color: var(--text); }
.form-field__required { font-size: var(--fs-sm); font-weight: 600; color: var(--major-text); }
.form-field__optional { font-size: var(--fs-xs); color: var(--text-3); }
.form-field__control { display: flex; flex-direction: column; min-inline-size: 0; }
.form-field__message {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  margin: 0;
  font-size: var(--fs-sm);
  color: var(--text-2);
}
.form-field__message :deep(.app-icon) { flex-shrink: 0; margin-top: 2px; }
.form-field__message--error { color: var(--major-text); }
.form-field--invalid .form-field__control :deep(input),
.form-field--invalid .form-field__control :deep(select),
.form-field--invalid .form-field__control :deep(textarea) { border-color: var(--major); }
</style>
