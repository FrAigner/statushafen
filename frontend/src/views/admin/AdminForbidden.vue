<script setup>
/*
  Seite „Keine Berechtigung“. AdminLayout zeigt sie anstelle einer
  Admin-Seite, wenn der Nutzer das Leserecht dieses Tabs nicht hat (direkter
  Aufruf per Adresse, Lesezeichen, alter Link). Die Seite der eigentlichen View
  wird dann gar nicht erst geladen, es gibt also keine API-Aufrufe und keine
  Weiterleitung ins Leere. Der Link führt zum ersten Tab, den der Nutzer lesen
  darf; gibt es keinen, erklärt die Seite, dass noch keine Rolle vergeben ist.
*/
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuth } from '../../composables/useAuth'
import { firstAllowedItem } from '../../utils/adminNav'
import PageHeader from '../../components/PageHeader.vue'

const { t } = useI18n()
const { hasPerm, isGlobalAdmin } = useAuth()

const target = computed(() => firstAllowedItem(hasPerm, isGlobalAdmin.value))
</script>

<template>
  <section class="forbidden" data-status="forbidden">
    <PageHeader :level="2" :title="t('adminForbidden.title')" />
    <!-- Für Screenreader: Status der Seite (wie ein 403), die Überschrift allein sagt es nicht. -->
    <p class="sr-only" role="status">{{ t('adminForbidden.status') }}</p>
    <p class="forbidden__text">{{ target ? t('adminForbidden.text') : t('adminForbidden.noAccess') }}</p>
    <p v-if="target">
      <router-link class="forbidden__link" :to="target.to">{{ t('adminForbidden.goTo', { name: t(`adminNav.items.${target.labelKey}`) }) }}</router-link>
    </p>
  </section>
</template>

<style scoped>
.forbidden__text {
  max-width: 60ch;
  color: var(--text-2);
}
.forbidden__link {
  font-weight: 600;
}
</style>
