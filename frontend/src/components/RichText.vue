<script setup lang="ts">
// Shows a message or description as formatted text (restricted Markdown, see
// utils/richText.ts). `plain` shows the text unchanged (foreign texts from
// external sources are not Markdown, line breaks are preserved).
import { computed } from 'vue'
import { renderRichText, plainTextToHtml } from '../utils/richText'

// Stryker disable all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
const props = withDefaults(defineProps<{
  text?: string
  plain?: boolean
}>(), {
  text: '',
  plain: false,
})
// Stryker restore all

// Both branches yield only sanitized or escaped HTML.
const html = computed(() => (props.plain ? plainTextToHtml(props.text) : renderRichText(props.text)))
</script>

<template>
  <!-- eslint-disable-next-line vue/no-v-html -- sanitized by renderRichText (marked without raw HTML + DOMPurify with a fixed allowlist) or escaped -->
  <div class="rich-text" :class="{ 'rich-text--plain': plain }" v-html="html"></div>
</template>

<style scoped>
.rich-text { overflow-wrap: anywhere; }
.rich-text--plain { white-space: pre-wrap; }
.rich-text :deep(p) { margin: 0 0 var(--sp-2, 0.5rem); }
.rich-text > :deep(:last-child) { margin-bottom: 0; }
.rich-text :deep(ul), .rich-text :deep(ol) { margin: 0 0 var(--sp-2, 0.5rem); padding-left: 1.4em; }
.rich-text :deep(blockquote) { margin: 0 0 var(--sp-2, 0.5rem); padding-left: var(--sp-3, 0.75rem); border-left: 3px solid var(--border, currentColor); opacity: 0.9; }
.rich-text :deep(code) { font-family: var(--font-mono, ui-monospace, monospace); font-size: 0.92em; padding: 0 0.25em; border-radius: 3px; background: var(--surface-2, rgba(127, 127, 127, 0.15)); }
.rich-text :deep(a) { color: var(--primary, inherit); text-decoration: underline; }
</style>
