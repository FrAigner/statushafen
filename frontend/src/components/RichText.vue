<script setup>
// Zeigt eine Nachricht bzw. Beschreibung als formatierten Text
// (eingeschränktes Markdown, siehe utils/richText.js). `plain` zeigt den Text
// unverändert (Fremdtexte externer Quellen sind kein Markdown, Zeilenumbrüche
// bleiben erhalten).
import { computed } from 'vue'
import { renderRichText, plainTextToHtml } from '../utils/richText'

// Stryker disable all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const props = defineProps({
  text: { type: String, default: '' },
  plain: { type: Boolean, default: false },
})
// Stryker restore all

// Beide Zweige liefern ausschließlich bereinigtes bzw. maskiertes HTML.
const html = computed(() => (props.plain ? plainTextToHtml(props.text) : renderRichText(props.text)))
</script>

<template>
  <!-- eslint-disable-next-line vue/no-v-html -- bereinigt durch renderRichText (marked ohne Roh-HTML + DOMPurify mit fester Whitelist) bzw. maskiert -->
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
