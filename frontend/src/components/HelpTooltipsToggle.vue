<script setup lang="ts">
import { OnyxSwitch } from 'sit-onyx'
import { useI18n } from 'vue-i18n'
import { useHelpTooltips } from '../composables/useHelpTooltips'

// "Show help tooltips" switch, in the sidebar footer of the
// AdminLayout and in the profile. Takes effect immediately, saved on the user
// (useHelpTooltips.ts).

const { t } = useI18n()
const { enabled, setHelpTooltips, saveError } = useHelpTooltips()
</script>

<template>
  <div class="help-tooltips-toggle">
    <OnyxSwitch
      :model-value="enabled"
      :label="t('helpTooltips.toggle')"
      @update:model-value="setHelpTooltips"
    />
    <p v-if="saveError" role="alert" class="help-tooltips-toggle__error">{{ saveError }}</p>
  </div>
</template>

<style scoped>
.help-tooltips-toggle__error {
  margin: 4px 0 0;
  font-size: 0.78rem;
  color: var(--major);
}
</style>
