<script setup>
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
</script>

<template>
  <div class="alert alert-warning fade-in mb-16" role="alert">
    <span>{{ t('common.forbidden') }}</span>
  </div>
</template>
