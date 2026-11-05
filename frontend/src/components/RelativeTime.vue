<script setup>
/*
  Richtung C, Betriebsseiten: Zeitangabe relativ („vor 3 Tagen“),
  der genaue Zeitpunkt steht im title und im datetime-Attribut. Ohne Wert
  erscheint ein Strich. Wortlaut und Einheiten: utils/relativeTime.js.
*/
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatRelative } from '../utils/relativeTime'
import { formatDateTimeShort } from '../utils/datetime'

// Stryker disable all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const props = defineProps({
  value: { type: String, default: null },
})
// Stryker restore all
const { locale } = useI18n()
const relative = computed(() => formatRelative(props.value, locale.value))
const absolute = computed(() => formatDateTimeShort(props.value, locale.value))
</script>

<template>
  <time v-if="value" class="cell-changed" :datetime="value" :title="absolute">{{ relative }}</time>
  <span v-else class="cell-changed text-muted">—</span>
</template>
