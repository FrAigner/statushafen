<script setup>
// Editor für formatierte Texte (Nachrichten/Beschreibungen).
// Gespeichert wird weiterhin der Markdown-String (eingeschränktes Markdown,
// utils/richText.js); der Baustein ist ein natives <textarea> mit schlanker
// Werkzeugleiste (fett, kursiv, Liste, Zitat, Code, Link), Vorschau über
// RichText.vue (dieselbe Bereinigung wie die Statusseite) und Zeichenzähler
// gegen RICH_TEXT_MAX_LENGTH. Attribute (id, required, aria-*, placeholder,
// class, @input …) landen am <textarea>, damit FormField-Slot-Props wie bei
// einem normalen Textfeld funktionieren. Absichtlich ohne maxlength: Ein
// zu langer Text (Altdaten, Einfügen) wird nicht stillschweigend gekürzt,
// sondern der Zähler schlägt an.
import { computed, nextTick, ref, useAttrs, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import RichText from './RichText.vue'
import { RICH_TEXT_MAX_LENGTH } from '../utils/richText'
import { applyFormat, minimalReplace } from '../utils/richTextEditing'

defineOptions({ inheritAttrs: false })

// Stryker disable all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const props = defineProps({
  modelValue: { type: String, default: '' },
  maxLength: { type: Number, default: RICH_TEXT_MAX_LENGTH },
  readonly: { type: Boolean, default: false },
  rows: { type: Number, default: 6 },
  // Fehlerzustand des Formulars (FormField-Slot-Prop). Als Prop, damit der Editor beim Fehler
  // aus der Vorschau zurückschaltet und das Feld fokussierbar ist.
  ariaInvalid: { type: null, default: undefined },
})
// Stryker disable next-line all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const emit = defineEmits(['update:modelValue'])
// Stryker restore all

const { t, locale } = useI18n()
const attrs = useAttrs()
const uid = useId()
const area = ref(null)
const preview = ref(false)
const counterId = `rte-count-${uid}`
const previewId = `rte-preview-${uid}`

// Zeichen wie das Backend zählen (Runen), nicht UTF-16-Einheiten.
const length = computed(() => Array.from(props.modelValue ?? '').length)
const over = computed(() => length.value > props.maxLength)
// Zahlen nach UI-Sprache, nicht nach Locale des Browsers/Prozesses.
const fmt = (n) => n.toLocaleString(locale.value)
const counterText = computed(() => t('richTextEditor.counter', { count: fmt(length.value), max: fmt(props.maxLength) }))
const tooLongText = computed(() => t('richTextEditor.tooLong', { max: fmt(props.maxLength) }))
// Der Fehlerzustand kommt nur vom Formular; ein zu langer Text meldet sich über den Zähler
// (sonst würde focusInvalid ein früheres, nur zu langes Feld statt des fehlerhaften wählen).
watch(() => props.ariaInvalid, (v) => { if (v) preview.value = false }, { flush: 'pre' })
const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform ?? '')
const mod = computed(() => (isMac ? '⌘' : t('richTextEditor.modKey')))
const describedby = computed(() => [attrs['aria-describedby'], counterId].filter(Boolean).join(' '))

const tools = [
  { kind: 'bold', glyph: 'B', key: 'b' },
  { kind: 'italic', glyph: 'I', key: 'i' },
  { kind: 'list', glyph: '•' },
  { kind: 'quote', glyph: '❞' },
  { kind: 'code', glyph: '</>' },
  { kind: 'link', glyph: '↗' },
]

function onInput(e) {
  emit('update:modelValue', e.target.value)
}

async function format(kind) {
  const el = area.value
  if (!el || props.readonly) return
  // Das Feld normalisiert Zeilenumbrüche auf \n; Auswahl und Text müssen vom selben String stammen.
  const before = el.value
  const res = applyFormat(before, el.selectionStart, el.selectionEnd, kind, t(`richTextEditor.placeholder.${kind}`))
  el.focus()
  // execCommand hält die Undo-Historie (Strg+Z) des Feldes; es löst das input-Ereignis selbst aus.
  const rep = minimalReplace(before, res.value)
  el.setSelectionRange(rep.start, rep.end)
  const done = typeof document.execCommand === 'function' && document.execCommand('insertText', false, rep.text)
  if (!done) emit('update:modelValue', res.value)
  await nextTick()
  el.setSelectionRange(res.start, res.end)
}

function onKeydown(e) {
  if (!(e.ctrlKey || e.metaKey) || e.altKey || e.shiftKey) return
  const tool = tools.find((x) => x.key === e.key.toLowerCase())
  if (!tool) return
  e.preventDefault()
  format(tool.kind)
}
</script>

<template>
  <div class="rte" :class="{ 'rte--over': over }">
    <div class="rte__bar">
      <div role="toolbar" :aria-label="t('richTextEditor.toolbar')" class="rte__tools">
        <button
          v-for="tool in tools"
          :key="tool.kind"
          type="button"
          class="rte__btn"
          :class="`rte__btn--${tool.kind}`"
          :aria-label="t(`richTextEditor.tools.${tool.kind}`, { mod })"
          :title="t(`richTextEditor.tools.${tool.kind}`, { mod })"
          :disabled="readonly || preview"
          @click="format(tool.kind)"
        >
          <span aria-hidden="true">{{ tool.glyph }}</span>
        </button>
      </div>
      <button
        type="button"
        class="rte__btn rte__btn--preview"
        :aria-pressed="preview"
        :aria-controls="previewId"
        @click="preview = !preview"
      >
        {{ preview ? t('richTextEditor.edit') : t('richTextEditor.preview') }}
      </button>
    </div>

    <textarea
      :hidden="preview"
      v-bind="$attrs"
      ref="area"
      :value="modelValue"
      :readonly="readonly"
      :rows="rows"
      :aria-describedby="describedby"
      :aria-invalid="ariaInvalid || undefined"
      @input="onInput"
      @keydown="onKeydown"
    ></textarea>

    <div :hidden="!preview" :id="previewId" class="rte__preview" role="region" :aria-label="t('richTextEditor.previewLabel')">
      <RichText v-if="modelValue" :text="modelValue" />
      <p v-else class="rte__empty">{{ t('richTextEditor.previewEmpty') }}</p>
    </div>

    <p :id="counterId" class="rte__counter" :class="{ 'rte__counter--over': over }">{{ counterText }}</p>
    <!-- Die Warnung steht in einer höflichen Live-Region und ändert sich nur beim Überschreiten, nicht bei jedem Tastendruck. -->
    <p class="rte__counter rte__counter--over rte__warn" role="status" aria-live="polite">{{ over ? tooLongText : '' }}</p>
  </div>
</template>

<style scoped>
.rte { display: flex; flex-direction: column; gap: var(--sp-1, 0.25rem); inline-size: 100%; }
.rte__bar { display: flex; justify-content: space-between; align-items: center; gap: var(--sp-2, 0.5rem); flex-wrap: wrap; }
.rte__tools { display: flex; gap: var(--sp-1, 0.25rem); flex-wrap: wrap; }
.rte__btn {
  min-inline-size: 2rem; min-block-size: 2rem; padding: 0 var(--sp-2, 0.5rem);
  border: 1px solid var(--border); border-radius: var(--r-sm, 4px);
  background: var(--surface); color: var(--text); font: inherit; font-size: var(--fs-sm); cursor: pointer;
}
.rte__btn:hover:not(:disabled) { background: var(--primary-soft); }
.rte__btn:focus-visible { outline: 2px solid var(--focus); outline-offset: 1px; }
.rte__btn:disabled { opacity: .5; cursor: not-allowed; }
.rte__btn[aria-pressed="true"] { background: var(--primary-soft); color: var(--primary-text); border-color: var(--primary); }
.rte__btn--bold { font-weight: 700; }
.rte__btn--italic { font-style: italic; }
.rte__btn--code { font-family: var(--font-mono, monospace); }
.rte__preview {
  min-block-size: 5rem; padding: var(--sp-2, 0.5rem) var(--sp-3, 0.75rem);
  border: 1px dashed var(--border-strong, var(--border)); border-radius: var(--r-sm, 4px); background: var(--sunken, var(--surface));
}
/* hidden statt v-show: kein style-Attribut im DOM (Inline-Style-Schutz); überstimmt display-Regeln der Felder */
.rte [hidden] { display: none !important; }
.rte__empty { margin: 0; color: var(--text-2); }
.rte__counter { margin: 0; font-size: var(--fs-xs); color: var(--text-2); text-align: end; }
.rte__counter--over { color: var(--major-text); font-weight: 600; }
.rte--over textarea { border-color: var(--major); }
</style>
