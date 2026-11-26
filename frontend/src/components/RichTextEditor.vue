<script setup lang="ts">
// Editor for formatted texts (messages/descriptions).
// What is stored is still the Markdown string (restricted Markdown,
// utils/richText.ts); the component is a native <textarea> with a slim
// toolbar (bold, italic, list, quote, code, link), a preview via RichText.vue
// (the same sanitizing as the status page) and a character counter against
// RICH_TEXT_MAX_LENGTH. Attributes (id, required, aria-*, placeholder, class,
// @input …) land on the <textarea>, so FormField slot props work as with a
// normal text field. Deliberately without maxlength: an overly long text
// (legacy data, pasting) is not silently truncated, the counter flags it
// instead.
import { computed, nextTick, ref, useAttrs, useId, watch } from 'vue'
import type { PropType } from 'vue'
import { useI18n } from 'vue-i18n'
import RichText from './RichText.vue'
import { RICH_TEXT_MAX_LENGTH } from '../utils/richText'
import { applyFormat, minimalReplace } from '../utils/richTextEditing'

defineOptions({ inheritAttrs: false })

/** Toolbar actions, as understood by applyFormat. */
type FormatKind = 'bold' | 'italic' | 'list' | 'quote' | 'code' | 'link'

// Runtime declaration: `ariaInvalid` must keep `type: null` (any value, no boolean casting).
// Stryker disable all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
const props = defineProps({
  modelValue: { type: String, default: '' },
  maxLength: { type: Number, default: RICH_TEXT_MAX_LENGTH },
  readonly: { type: Boolean, default: false },
  rows: { type: Number, default: 6 },
  // Error state of the form (FormField slot prop). A prop, so that the editor switches back from
  // the preview on an error and the field is focusable.
  ariaInvalid: { type: null as unknown as PropType<boolean | 'true' | 'false' | undefined>, default: undefined },
})
// Stryker disable next-line all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
// Stryker restore all

const { t, locale } = useI18n()
const attrs = useAttrs()
const uid = useId()
const area = ref<HTMLTextAreaElement | null>(null)
const preview = ref(false)
const counterId = `rte-count-${uid}`
const previewId = `rte-preview-${uid}`

// Count characters like the backend (runes), not UTF-16 units.
const length = computed(() => Array.from(props.modelValue ?? '').length)
const over = computed(() => length.value > props.maxLength)
// Numbers by UI language, not by the locale of the browser/process.
const fmt = (n: number) => n.toLocaleString(locale.value)
const counterText = computed(() => t('richTextEditor.counter', { count: fmt(length.value), max: fmt(props.maxLength) }))
const tooLongText = computed(() => t('richTextEditor.tooLong', { max: fmt(props.maxLength) }))
// The error state comes only from the form; an overly long text reports via the counter
// (otherwise focusInvalid would pick an earlier, merely too long field instead of the invalid one).
watch(() => props.ariaInvalid, (v) => { if (v) preview.value = false }, { flush: 'pre' })
const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform ?? '')
const mod = computed(() => (isMac ? '⌘' : t('richTextEditor.modKey')))
const describedby = computed(() => [attrs['aria-describedby'], counterId].filter(Boolean).join(' '))

const tools: { kind: FormatKind, glyph: string, key?: string }[] = [
  { kind: 'bold', glyph: 'B', key: 'b' },
  { kind: 'italic', glyph: 'I', key: 'i' },
  { kind: 'list', glyph: '•' },
  { kind: 'quote', glyph: '❞' },
  { kind: 'code', glyph: '</>' },
  { kind: 'link', glyph: '↗' },
]

function onInput(e: Event) {
  emit('update:modelValue', (e.target as HTMLTextAreaElement).value)
}

async function format(kind: FormatKind) {
  const el = area.value
  if (!el || props.readonly) return
  // The field normalizes line breaks to \n; selection and text must come from the same string.
  const before = el.value
  const res = applyFormat(before, el.selectionStart, el.selectionEnd, kind, t(`richTextEditor.placeholder.${kind}`))
  el.focus()
  // execCommand keeps the field's undo history (Ctrl+Z); it fires the input event itself.
  const rep = minimalReplace(before, res.value)
  el.setSelectionRange(rep.start, rep.end)
  const done = typeof document.execCommand === 'function' && document.execCommand('insertText', false, rep.text)
  if (!done) emit('update:modelValue', res.value)
  await nextTick()
  el.setSelectionRange(res.start, res.end)
}

function onKeydown(e: KeyboardEvent) {
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
    <!-- The warning sits in a polite live region and only changes when the limit is crossed, not on every keystroke. -->
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
/* hidden instead of v-show: no style attribute in the DOM (inline style protection); overrides the fields' display rules */
.rte [hidden] { display: none !important; }
.rte__empty { margin: 0; color: var(--text-2); }
.rte__counter { margin: 0; font-size: var(--fs-xs); color: var(--text-2); text-align: end; }
.rte__counter--over { color: var(--major-text); font-weight: 600; }
.rte--over textarea { border-color: var(--major); }
</style>
