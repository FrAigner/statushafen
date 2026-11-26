<script setup lang="ts">
/*
  Multi-select (OnyxSelect multiple) in the FormField pattern.

  Previously an OnyxSelect with hide-label sat behind a FormField label whose
  `for` pointed nowhere, and the required-field error was missing on the field. Here:

  - The field ID goes to OnyxSelect as `id`; Onyx puts it on the combobox
    input, so `label for` (FormField) points to a real field.
  - OnyxSelect only puts `aria-invalid`/`aria-describedby` on its root element,
    not on the input (its own error line would also duplicate the FormField
    line). It does not pass `required` on to Onyx either: that showed an English
    browser tooltip ("Please fill out this field.") on focus; instead
    aria-required sits on the input. So this component sets both on the input itself, so that
    screen readers read the error and useFormPanel.focusInvalid() jumps there.
*/
import { computed, onMounted, ref, useId, watch } from 'vue'
import { OnyxSelect } from 'sit-onyx'
import type { SelectOption, SelectOptionValue } from 'sit-onyx'
import FormField from './FormField.vue'

// Stryker disable all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
const props = withDefaults(defineProps<{
  modelValue?: SelectOptionValue[]
  options?: SelectOption[]
  label: string
  required?: boolean
  error?: string | null
  hint?: string | null
  help?: string | null
}>(), {
  modelValue: () => [],
  options: () => [],
  required: false,
  error: null,
  hint: null,
  help: null,
})
// Stryker restore all
// Stryker disable next-line all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
const emit = defineEmits<{ 'update:modelValue': [value: SelectOptionValue[] | null | undefined] }>()

const uid = useId()
const id = `ms-${uid}`
const root = ref<{ $el: HTMLElement } | null>(null)

const describedby = computed(() => {
  if (props.error) return `${id}-error`
  if (props.hint) return `${id}-hint`
  return null
})

function syncAria() {
  const el = root.value!.$el
  // Real Onyx: input inside the tree; test stub: the root element carries the ID.
  const field = el.id === id ? el : el.querySelector<HTMLElement>(`[id="${id}"]`)!
  if (props.error) field.setAttribute('aria-invalid', 'true')
  else field.removeAttribute('aria-invalid')
  // aria-required is only allowed on input roles (combobox), not on the group stub of the tests.
  if (props.required && field.tagName === 'INPUT') field.setAttribute('aria-required', 'true')
  else field.removeAttribute('aria-required')
  // Keep the help tooltip id set by v-help (FormField `help`); it is managed by the directive.
  const helpIds = (field.getAttribute('aria-describedby') || '').split(/\s+/).filter(token => token.startsWith('help-'))
  const ids = [describedby.value, ...helpIds].filter(Boolean)
  if (ids.length) field.setAttribute('aria-describedby', ids.join(' '))
  else field.removeAttribute('aria-describedby')
}

onMounted(syncAria)
watch([() => props.error, () => props.hint, () => props.required], syncAria, { flush: 'post' })
</script>

<template>
  <FormField
    :label="label"
    :required="required"
    :error="error"
    :hint="hint"
    :help="help"
    :field-id="id"
  >
    <OnyxSelect
      ref="root"
      :id="id"
      :model-value="modelValue"
      :options="options"
      multiple
      hide-label
      :label="label"
      :list-label="label"
      @update:model-value="emit('update:modelValue', $event)"
    />
  </FormField>
</template>
