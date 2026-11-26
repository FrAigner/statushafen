<script setup lang="ts">
/*
  "No permission" page. AdminLayout shows it instead of an admin page when the
  user lacks read permission for that tab (direct URL, bookmark, old link). The
  actual view is never loaded, so there are no API calls and no redirect into
  nowhere. The link leads to the first tab the user may read; if there is none,
  the page explains that no role has been assigned yet.
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
    <!-- For screen readers: the page status (like a 403); the heading alone does not say it. -->
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
