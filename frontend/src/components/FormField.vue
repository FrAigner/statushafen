<script setup>
/*
  Redesign Stufe 3: Formularfeld-Muster. Label über dem Feld,
  „*“ für Pflicht bzw. „optional“, Hilfe am Feld, darunter eine
  Hinweis- ODER Fehlerzeile mit Symbol. ersetzt .form-group label und die
  gemischten Onyx-Labels bei nativen Feldern.

  Das Feld selbst kommt über den Standard-Slot und bekommt per Slot-Props alles
  Nötige, damit das Muster nicht am Markup hängt:

    <FormField v-slot="f" label="Name" required :error="errors.name" help="components.name">
      <input :id="f.id" v-model="name" :required="f.required"
             :aria-describedby="f.describedby" :aria-invalid="f.invalid">
    </FormField>

  - Der Stern kommt nur aus der Darstellung, nie aus dem Label-Text
    (Regel, i18n/requiredMarker.test.js); er ist für
    Screenreader versteckt, `required` am Feld sagt es an.
  - Fehler ersetzt den Hinweis, steht in einer role="alert"-Zeile und setzt
    aria-invalid; beide Zeilen sind per aria-describedby mit dem Feld verbunden.
  - `help` ist ein Hilfeschlüssel für v-help ("bereich.feld"); die Hilfe hängt
    am Feldbereich (Hover/Fokus, Escape schließt), ohne zusätzliches Symbol.
  - Der Aufrufer sorgt beim Absenden dafür, dass der Fokus auf das erste
    fehlerhafte Feld springt (umsetzung.md, Barrierefreiheit).
*/
import { computed, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from './AppIcon.vue'
import vHelp from '../directives/vHelp'

// Stryker disable all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const props = defineProps({
  label: { type: String, required: true },
  required: { type: Boolean, default: false },
  hint: { type: String, default: null },
  error: { type: String, default: null },
  help: { type: String, default: null },
  // Eigene Feld-ID, falls das Feld sie schon vorgibt (label for).
  fieldId: { type: String, default: null },
})
// Stryker restore all

const { t } = useI18n()
const uid = useId()
const id = computed(() => props.fieldId ?? `ff-${uid}`)
const hintId = computed(() => `${id.value}-hint`)
const errorId = computed(() => `${id.value}-error`)
const invalid = computed(() => !!props.error)

// Fehler ersetzt den Hinweis; ohne beides kein aria-describedby.
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
