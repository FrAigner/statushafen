<script setup>
/*
  Mehrfachauswahl (OnyxSelect multiple) im FormField-Muster.

  Vorher stand ein OnyxSelect mit hide-label hinter einem FormField-Label, dessen
  `for` ins Leere zeigte, und der Pflichtfeld-Fehler fehlte am Feld. Hier:

  - Die Feld-ID geht als `id` an OnyxSelect; Onyx setzt sie auf das
    Combobox-Eingabefeld, damit zeigt `label for` (FormField) auf ein echtes Feld.
  - OnyxSelect trägt `aria-invalid`/`aria-describedby` nur am Wurzelelement, nicht
    am Eingabefeld (seine eigene Fehlerzeile würde zudem die FormField-Zeile
    doppeln). Auch `required` reicht sie nicht an Onyx durch: Es zeigte beim Fokus
    einen englischen Browser-Tooltip („Please fill out this field.“); stattdessen
    steht aria-required am Eingabefeld. Deshalb setzt diese Komponente beides selbst am Eingabefeld, damit
    Screenreader den Fehler lesen und useFormPanel.focusInvalid() dorthin springt.
*/
import { computed, onMounted, ref, useId, watch } from 'vue'
import { OnyxSelect } from 'sit-onyx'
import FormField from './FormField.vue'

// Stryker disable all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const props = defineProps({
  modelValue: { type: Array, default: () => [] },
  options: { type: Array, default: () => [] },
  label: { type: String, required: true },
  required: { type: Boolean, default: false },
  error: { type: String, default: null },
  hint: { type: String, default: null },
  help: { type: String, default: null },
})
// Stryker restore all
// Stryker disable next-line all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const emit = defineEmits(['update:modelValue'])

const uid = useId()
const id = `ms-${uid}`
const root = ref(null)

const describedby = computed(() => {
  if (props.error) return `${id}-error`
  if (props.hint) return `${id}-hint`
  return null
})

function syncAria() {
  const el = root.value.$el
  // Echtes Onyx: Eingabefeld im Baum; Test-Stub: das Wurzelelement trägt die ID.
  const field = el.id === id ? el : el.querySelector(`[id="${id}"]`)
  if (props.error) field.setAttribute('aria-invalid', 'true')
  else field.removeAttribute('aria-invalid')
  // aria-required ist nur an Eingabe-Rollen erlaubt (Combobox), nicht am Gruppen-Stub der Tests.
  if (props.required && field.tagName === 'INPUT') field.setAttribute('aria-required', 'true')
  else field.removeAttribute('aria-required')
  if (describedby.value) field.setAttribute('aria-describedby', describedby.value)
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
