<script setup lang="ts">
// App.vue now only serves as the root container for the vue-router
import { onBeforeUnmount } from 'vue'
import { installFormValidationGuard } from './utils/formValidation'
import ConfirmDialog from './components/ConfirmDialog.vue'

// All forms carry `novalidate` (no native hover hint
// "Please fill out this field." over the help tooltips); the guard checks the
// required fields on submit instead. Here rather than in main.ts, so that it
// lives with the app and is testable.
const uninstallFormValidationGuard = installFormValidationGuard()
onBeforeUnmount(uninstallFormValidationGuard)
</script>

<template>
  <router-view />
  <!-- The app's only confirmation dialog (useConfirm) -->
  <ConfirmDialog />
</template>

<style>
/* Global styles are maintained in style.css */
</style>
