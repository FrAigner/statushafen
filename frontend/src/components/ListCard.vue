<script setup lang="ts">
/*
  Direction C: list card of all admin pages. Card head with the title
  on the left and tools (SearchBox, filters, SegmentControl) on the right in the
  "tools" slot (the component adds the .comp-card__tools wrapper itself); below,
  flush, the table or the empty state (default slot). The styles
  (.comp-card …) come from the components page and live in style.css.
  With `padded` the content gets inner padding (settings and form pages);
  with `overflowVisible` flyouts may extend beyond the card edge
  (e.g. select fields in the card content). Inside a .split the card sits with
  class="split__main" to the left of the FormPanel. The card is a <section>,
  labelled by the card title (h3 below the page title h2).
  This component replaces three versions that grew in parallel (operations,
  administration, system).
*/
import { useId } from 'vue'

// Stryker disable all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
withDefaults(defineProps<{
  title: string
  // Form and settings pages: content with inner padding instead of a flush table.
  padded?: boolean
  // Card without overflow:hidden, so select flyouts are not clipped.
  overflowVisible?: boolean
}>(), {
  padded: false,
  overflowVisible: false,
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
