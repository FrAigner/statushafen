<script setup lang="ts">
/*
  Direction C: form as a side card to the right of the list card
  (child of .split.split--open, top-aligned, sticky while scrolling; below 860 px
  it sits below the list, see style.css). Head: title with the name instead of
  the ID, required-field hint, close button ×. The content (usually a
  <form class="edit-form">) comes via the slot; `close` reports closing
  (like "Cancel"). `hint` replaces the default hint, `showRequiredHint`
  false hides it for forms without any required fields.
  This component replaces three versions that grew in parallel (operations,
  administration, system).
*/
import { useI18n } from 'vue-i18n'
import AppIcon from './AppIcon.vue'

// Stryker disable all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
withDefaults(defineProps<{
  title: string
  hint?: string | null
  showRequiredHint?: boolean
}>(), {
  hint: null,
  showRequiredHint: true,
})
// Stryker restore all
// Stryker disable next-line all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
const emit = defineEmits<{ close: [] }>()

const { t } = useI18n()
</script>

<template>
  <aside class="edit-card form-panel fade-in" :aria-label="title">
    <div class="form-panel__head">
      <div>
        <h3>{{ title }}</h3>
        <p v-if="showRequiredHint || hint" class="form-panel__hint">{{ hint ?? t('patterns.requiredHint') }}</p>
      </div>
      <button type="button" class="form-panel__close" :aria-label="t('patterns.closeForm')" data-help-exempt="Formular schließen, selbsterklärend" @click="emit('close')">
        <AppIcon name="x" :size="16" />
      </button>
    </div>
    <slot />
  </aside>
</template>

<style scoped>
/* The title in the head has no divider: ".edit-card h3" (admin.css, loaded
   later) would otherwise add a line and spacing below the title. The scoped rule
   wins by specificity, independent of load order. */
.form-panel__head h3 {
  margin: 0;
  padding: 0;
  border: 0;
}
</style>
