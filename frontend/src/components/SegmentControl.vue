<script setup>
/*
  Richtung C, Betriebsseiten: Segment-Umschalter für Listenfilter
  (z. B. Alle / Offen / Behoben). Gruppe aus Knöpfen mit aria-pressed, damit
  er ohne Pfeiltasten-Sonderlogik per Tab und Enter/Leertaste bedienbar ist.
  Optionen: [{ value, label }]; der gewählte Wert ist auch ohne Farbe am
  Rahmen und an der Füllung erkennbar (aria-pressed gibt ihn weiter).
*/
// Stryker disable all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
defineProps({
  modelValue: { type: String, required: true },
  options: { type: Array, required: true },
  label: { type: String, required: true },
})
// Stryker restore all
// Stryker disable next-line all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const emit = defineEmits(['update:modelValue'])
</script>

<template>
  <div class="segment" role="group" :aria-label="label">
    <button
      v-for="opt in options"
      :key="opt.value"
      type="button"
      class="segment__item"
      :class="{ 'segment__item--on': opt.value === modelValue }"
      :aria-pressed="opt.value === modelValue ? 'true' : 'false'"
      data-help-exempt="Filter-Umschalter, selbsterklärend"
      @click="emit('update:modelValue', opt.value)"
    >{{ opt.label }}</button>
  </div>
</template>
