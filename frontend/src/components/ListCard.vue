<script setup>
/*
  Richtung C: Listenkarte aller Admin-Seiten. Kartenkopf mit Titel
  links und Werkzeugen (SearchBox, Filter, SegmentControl) rechts im Slot
  "tools" (der Baustein setzt den Rahmen .comp-card__tools selbst); darunter
  bündig die Tabelle bzw. der Leerzustand (Standard-Slot). Die Stile
  (.comp-card …) stammen aus der Komponentenseite und stehen in style.css.
  Mit `padded` bekommt der Inhalt Innenabstand (Einstellungs- und
  Formularseiten); mit `overflowVisible` ragen Flyouts über den Kartenrand
  (z. B. Auswahlfelder im Karteninhalt). In einem .split steht die Karte mit
  class="split__main" links neben dem FormPanel. Die Karte ist ein <section>,
  benannt über den Kartentitel (h3 unter dem Seitentitel h2).
  Dieser Baustein ersetzt drei parallel entstandene Fassungen (Betrieb,
  Verwaltung, System).
*/
import { useId } from 'vue'

// Stryker disable all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
defineProps({
  title: { type: String, required: true },
  // Formular- und Einstellungsseiten: Inhalt mit Innenabstand statt bündiger Tabelle.
  padded: { type: Boolean, default: false },
  // Karte ohne overflow:hidden, damit Auswahl-Flyouts nicht abgeschnitten werden.
  overflowVisible: { type: Boolean, default: false },
})
// Stryker restore all

const titleId = `list-card-${useId()}`
</script>

<template>
  <section class="comp-card list-card" :class="{ 'comp-card--visible': overflowVisible }" :aria-labelledby="titleId">
    <div class="comp-card__head">
      <h3 :id="titleId" class="comp-card__title">{{ title }}</h3>
      <div v-if="$slots.tools" class="comp-card__tools"><slot name="tools" /></div>
    </div>
    <div v-if="padded" class="list-card__body"><slot /></div>
    <slot v-else />
  </section>
</template>
