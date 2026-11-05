<script setup>
// App.vue now only serves as the root container for the vue-router
import { onBeforeUnmount } from 'vue'
import { installFormValidationGuard } from './utils/formValidation'
import ConfirmDialog from './components/ConfirmDialog.vue'

// Alle Formulare tragen `novalidate` (kein nativer Hover-Hinweis
// „Fülle dieses Feld aus.“ über den Hilfe-Tooltips); der Wächter prüft die
// Pflichtfelder stattdessen beim Absenden. Hier statt in main.js, damit er
// mit der App lebt und testbar ist.
const uninstallFormValidationGuard = installFormValidationGuard()
onBeforeUnmount(uninstallFormValidationGuard)
</script>

<template>
  <router-view />
  <!-- einziger Bestätigungsdialog der App (useConfirm) -->
  <ConfirmDialog />
</template>

<style>
/* Global styles are maintained in style.css */
</style>
