<script setup lang="ts">
/*
  Redesign stage 3: page header with breadcrumbs, title (h1, h2 via `level`),
  subtitle and actions (slot "actions"). Replaces the per-view .page-header
  variants. The page title is the only h1 of the page (umsetzung.md,
  accessibility); views that use h2 today switch over in stage 4.
  Breadcrumbs: entries with `to` are links, the last entry (without `to`) is
  the current page (aria-current="page").
*/
import type { PropType } from 'vue'
import type { RouteLocationRaw } from 'vue-router'
import { useI18n } from 'vue-i18n'

interface Breadcrumb {
  label: string
  to?: RouteLocationRaw
}

// Runtime declaration (not type-based) to keep the `level` validator.
// Stryker disable all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
defineProps({
  title: { type: String, required: true },
  subtitle: { type: String as PropType<string | null>, default: null },
  breadcrumbs: { type: Array as PropType<Breadcrumb[]>, default: () => [] },
  // Heading level: 1 (default, public pages) or 2 (admin views, where the
  // topbar carries the h1, the brand).
  level: { type: Number as PropType<1 | 2>, default: 1, validator: (v: number) => v === 1 || v === 2 },
})
// Stryker restore all

const { t } = useI18n()
</script>

<template>
  <header class="page-head">
    <nav v-if="breadcrumbs.length" class="page-head__crumbs" :aria-label="t('patterns.breadcrumb')">
      <ol>
        <li v-for="(crumb, index) in breadcrumbs" :key="index">
          <router-link v-if="crumb.to" :to="crumb.to">{{ crumb.label }}</router-link>
          <span v-else aria-current="page">{{ crumb.label }}</span>
        </li>
      </ol>
    </nav>
    <div class="page-head__row">
      <div class="page-head__titles">
        <component :is="`h${level}`" class="page-head__title">{{ title }}</component>
        <p v-if="subtitle" class="page-head__subtitle">{{ subtitle }}</p>
      </div>
      <div v-if="$slots.actions" class="page-head__actions"><slot name="actions" /></div>
    </div>
  </header>
</template>

<style scoped>
.page-head { margin-bottom: var(--sp-5); }
.page-head__crumbs ol {
  display: flex;
  flex-wrap: wrap;
  gap: var(--sp-1);
  margin: 0 0 var(--sp-2);
  padding: 0;
  list-style: none;
  font-size: var(--fs-sm);
  color: var(--text-2);
}
.page-head__crumbs li + li::before { content: '›'; margin-inline-end: var(--sp-1); color: var(--text-3); }
.page-head__crumbs a { color: var(--primary-text); text-decoration: none; }
.page-head__crumbs a:hover { text-decoration: underline; }
.page-head__crumbs a:focus-visible { outline: 2px solid var(--focus); outline-offset: 2px; border-radius: var(--r-sm); }
.page-head__row { display: flex; flex-wrap: wrap; align-items: flex-start; justify-content: space-between; gap: var(--sp-4); }
.page-head__titles { min-inline-size: 0; }
.page-head__title { margin: 0; font-size: var(--fs-2xl); font-weight: 600; line-height: 1.25; color: var(--text); }
.page-head__subtitle { margin: var(--sp-1) 0 0; font-size: var(--fs-base); color: var(--text-2); }
.page-head__actions { display: flex; flex-wrap: wrap; gap: var(--sp-2); align-items: center; }
</style>
