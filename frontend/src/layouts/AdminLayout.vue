<script setup lang="ts">
import { ref, computed, nextTick, onMounted, onBeforeUnmount, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuth } from '../composables/useAuth'
import { useTheme } from '../composables/useTheme'
import { useApi } from '../composables/useApi'
import { useAutoDismissAlerts } from '../composables/useAutoDismissAlerts'
import { useTenant } from '../composables/useTenant'
import { applyPageOverrides, clearPageOverrides } from '../i18n/overrides'
import AppIcon from '../components/AppIcon.vue'
import AdminForbidden from '../views/admin/AdminForbidden.vue'
import { NAV_GROUPS, firstAllowedItem } from '../utils/adminNav'
import type { NavItem } from '../utils/adminNav'
import { grantsAdminArea } from '../utils/permissions'
import AdminNotificationBell from '../components/AdminNotificationBell.vue'
import LanguageSwitcher from '../components/LanguageSwitcher.vue'
import HelpTooltipsToggle from '../components/HelpTooltipsToggle.vue'
import AdminQuickSearch from '../components/AdminQuickSearch.vue'
import { useDismissable } from '../composables/useDismissable'
import type { TenantId } from '../composables/useTenant'
import type { IsoDateString, MyPage } from '../types/api'

const router = useRouter()
const route  = useRoute()
const { t, locale } = useI18n()
const { currentUser, logout, hasPerm, isGlobalAdmin } = useAuth()
const { theme, toggleTheme } = useTheme()
const { apiFetch } = useApi()
const { activeTenantId, setActiveTenant, setMyPages, activeTenantRoleName } = useTenant()

/** Entry of GET /api/pages/:id/external-outages: an external source that has been failing for too long. */
interface ExternalOutage {
  source_id: number
  source_name: string
  since?: IsoDateString | null
  last_success_at?: IsoDateString | null
  last_error?: string
}

// Shows the role that actually applies to the active status page (Page Admin,
// Editor, ...) instead of the possibly stale global role from the login JWT.
const displayedRole = computed(() => {
  if (isGlobalAdmin.value) return 'Global Admin'
  return activeTenantRoleName.value || currentUser.value?.role || ''
})

const pages = ref<MyPage[]>([])
const pagesLoading = ref(true)
const pagesFailed = ref(false)
// Status page only: memberships whose role grants no admin right. They stay in
// myPages (public header), but the admin area offers only pages with an admin
// right; without any, the member is sent to the status page.
const statusPageOnly = ref<MyPage | null>(null)

onMounted(async () => {
  try {
    const res = await apiFetch<MyPage[] | null>('/api/me/pages')
    const all = res || []
    pages.value = isGlobalAdmin.value ? all : all.filter(p => grantsAdminArea(p.Permissions))
    statusPageOnly.value = !isGlobalAdmin.value && all.length > 0 && pages.value.length === 0
      ? (all.find(p => String(p.ID) === String(activeTenantId.value)) ?? all[0]!)
      : null
    setMyPages(res)
    // Also replace a stored tenant that is not (or no longer)
    // in the list - after a page SSO login the backend returns
    // only the SSO page, localStorage may still contain another one.
    const known = pages.value.some(p => String(p.ID) === String(activeTenantId.value))
    if (pages.value.length > 0 && !known) {
      setActiveTenant(pages.value[0]!.ID)
    }
  } catch (err) {
    pagesFailed.value = true
    console.error('Failed to load status pages', err)
  } finally {
    pagesLoading.value = false
  }
})

// Page overrides are strictly bound to the lifecycle
// of this layout, so they are guaranteed to apply only inside /admin/*
// and never on the public status page of the same tenant -
// even if activeTenantId (persisted in localStorage via useTenant.js)
// outlives leaving the admin area.
watch([activeTenantId, locale], ([pageId, loc]) => {
  if (pageId) {
    applyPageOverrides(pageId, loc)
  } else {
    clearPageOverrides(loc)
  }
}, { immediate: true })

const onTenantChange = () => {
  const currentPath = route.path
  router.push('/').then(() => router.push(currentPath))
}

const handleLogout = () => {
  logout()
  router.push('/')
}

// Redesign stage 2: the sidebar becomes area tabs
// (operations, content, management, system) with a second row for the
// entries of the active area. The split follows the task: operations is
// the daily operation of a status page, content everything visitors read
// (mail templates, languages, UI texts), management the cross-tenant
// maintenance, system the operation of the platform itself.
const navGroups = computed(() => NAV_GROUPS.map(group => ({
  key: group.key,
  label: t(`adminNav.groups.${group.key}`),
  items: group.items.map(item => ({ ...item, label: t(`adminNav.items.${item.labelKey}`) })),
})))

// A tab is visible <=> read permission on its resource (create,
// update and delete include read; global resources only for global
// admins, see useAuth.hasPerm). The docs need no permission.
const canSeeNavItem = (item: NavItem) => !item.resource || ((!item.globalOnly || isGlobalAdmin.value) && hasPerm(`${item.resource}:read`))

// Render only visible entries and hide areas without a visible
// entry entirely - otherwise empty tabs would remain for viewers.
const visibleNavGroups = computed(() => navGroups.value
  .map(group => ({ ...group, items: group.items.filter(canSeeNavItem) }))
  .filter(group => group.items.length > 0))

// Active area: the area of the entry with the longest matching path
// (so /admin/incidents/5/translations belongs to incidents). If the route belongs to
// no entry (e.g. /admin/profile), no tab is active; the second
// row then shows the first area.
const activeGroupKey = computed(() => {
  let best: string | null = null
  let bestLength = 0
  for (const group of visibleNavGroups.value) {
    for (const item of group.items) {
      const matches = route.path === item.to || route.path.startsWith(item.to + '/')
      if (matches && item.to.length > bestLength) {
        best = group.key
        bestLength = item.to.length
      }
    }
  }
  return best
})
const shownGroupKey = computed(() => activeGroupKey.value ?? visibleNavGroups.value[0]?.key)

// Outage banner. If an external source fails permanently
// (deadline per source, default 60 minutes without success), the backend reports it
// under external-outages; the banner stays until the next fetch succeeds. Like
// the outage mail, only for editors (external_sources:update). A load error
// only hides the banner, the page keeps running.
const outages = ref<ExternalOutage[]>([])
const loadOutages = async (pageId: TenantId | null) => {
  if (!pageId || !hasPerm('external_sources:update')) {
    outages.value = []
    return
  }
  try {
    const res = await apiFetch<ExternalOutage[] | null>(`/api/pages/${pageId}/external-outages`)
    // Answer discarded if another status page has become active meanwhile.
    if (String(pageId) === String(activeTenantId.value)) outages.value = Array.isArray(res) ? res : []
  } catch (err) {
    outages.value = []
    console.error('Failed to load external outages', err)
  }
}
watch([activeTenantId, () => route.path], ([pageId]) => loadOutages(pageId), { immediate: true })
const outageTime = (iso: IsoDateString | null | undefined) => (iso ? new Date(iso).toLocaleString(locale.value) : '')

// Access to the current tab. The route names the required permission
// (meta.permission, usually `<resource>:read`); if it is missing, the "no
// permission" page appears instead of the view. The view is then not loaded,
// so it makes no API calls, and there is no redirect into nowhere.
// While the status page roles are still loading, the permission is unknown
// ("loading"); if loading fails, the backend decides (403 in the view).
// The start page (/admin) leads to the first readable tab.
const homeTarget = computed(() => firstAllowedItem(hasPerm, isGlobalAdmin.value)?.to ?? null)
const accessState = computed(() => {
  const required = route.meta.permission as string
  const isHome = route.meta.home === true
  if (!required && !isHome) return 'ok'
  if (pagesLoading.value && !isGlobalAdmin.value) return 'loading'
  if (statusPageOnly.value) return 'statusPage'
  if (isHome) return homeTarget.value ? 'redirect' : 'forbidden'
  if (pagesFailed.value) return 'ok'
  if (route.meta.globalOnly && !isGlobalAdmin.value) return 'forbidden'
  return hasPerm(required) ? 'ok' : 'forbidden'
})
watch(accessState, (state) => {
  if (state === 'redirect') router.replace(homeTarget.value as string)
  else if (state === 'statusPage') router.replace(`/status/${encodeURIComponent(statusPageOnly.value!.Slug)}`)
}, { immediate: true })

const menuOpen = ref(false)
const userMenuOpen = ref(false)
const burgerRef = ref<HTMLButtonElement | null>(null)
const navRef = ref<HTMLElement | null>(null)
watch(route, () => { menuOpen.value = false; userMenuOpen.value = false })

// Phase 7b: lock body scrolling while the mobile menu is open as an overlay,
// so the background does not scroll along.
watch(menuOpen, (isOpen) => {
  document.body.style.overflow = isOpen ? 'hidden' : ''
  if (isOpen) {
    // Focus the first entry, so keyboard users land in the menu.
    nextTick(() => navRef.value?.querySelector<HTMLElement>('.area-items a')?.focus())
  }
})

// User menu: disclosure button with a panel. No role="menu", because
// the panel also contains the help switch (not a menu item). Arrow keys
// still move through the entries, Escape closes and returns the focus
// to the button, tabbing out of the panel closes it.
const userMenuRef = ref<HTMLElement | null>(null)
const userButtonRef = ref<HTMLButtonElement | null>(null)

const userInitials = computed(() => currentUser.value!.email!.slice(0, 2).toUpperCase())

const userMenuFocusables = () => Array.from(
  userMenuRef.value!.querySelectorAll<HTMLElement>('.user-menu-panel a[href], .user-menu-panel button, .user-menu-panel input')
)

const closeUserMenu = ({ restoreFocus = false }: { restoreFocus?: boolean } = {}) => {
  userMenuOpen.value = false
  if (restoreFocus) userButtonRef.value?.focus()
}

const openUserMenu = () => {
  userMenuOpen.value = true
  nextTick(() => userMenuFocusables()[0]?.focus())
}

const toggleUserMenu = () => (userMenuOpen.value ? closeUserMenu() : openUserMenu())

const onUserButtonKeydown = (e: KeyboardEvent) => {
  if (e.key === 'ArrowDown' && !userMenuOpen.value) {
    e.preventDefault()
    openUserMenu()
  }
}

const onUserMenuKeydown = (e: KeyboardEvent) => {
  const items = userMenuFocusables()
  const index = items.indexOf(document.activeElement as HTMLElement)
  const moves: Record<string, number> = { ArrowDown: index + 1, ArrowUp: index - 1, Home: 0, End: items.length - 1 }
  if (e.key in moves && userMenuOpen.value) {
    e.preventDefault()
    items[(moves[e.key]! + items.length) % items.length]?.focus()
  }
}

const onUserMenuFocusout = (e: FocusEvent) => {
  if (e.relatedTarget && !userMenuRef.value?.contains(e.relatedTarget as Node)) closeUserMenu()
}

// Click/tap outside closes; Escape stays with onKeydown below (it decides
// between the user menu and the mobile navigation).
useDismissable(userMenuOpen, [userMenuRef], () => closeUserMenu(), { escape: false })

const goPublicView = () => {
  closeUserMenu()
  router.push('/')
}

const onLogoutClick = () => {
  closeUserMenu()
  handleLogout()
}

const onKeydown = (e: KeyboardEvent) => {
  if (e.key !== 'Escape') return
  if (userMenuOpen.value) {
    closeUserMenu({ restoreFocus: true })
  } else if (menuOpen.value) {
    menuOpen.value = false
    burgerRef.value?.focus()
  }
}
// Success messages of the views fade out after a few seconds (see the composable).
const contentRef = ref<HTMLElement | null>(null)
useAutoDismissAlerts(contentRef)
onMounted(() => {
  window.addEventListener('keydown', onKeydown)
})
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeydown)
  document.body.style.overflow = ''
  clearPageOverrides(locale.value)
})
</script>

<template>
  <div class="admin-shell theme-admin">

    <!-- Header (dark) with brand, tenant switcher and
         user menu, below it the area tabs and the entries of the active
         area. All of it together is sticky (.admin-topbar). -->
    <header class="admin-topbar">
      <div class="topbar-main">
        <button
          ref="burgerRef"
          class="icon-btn burger-btn"
          :aria-label="menuOpen ? t('adminLayout.menuClose') : t('adminLayout.menuOpen')"
          :aria-expanded="menuOpen"
          @click="menuOpen = !menuOpen"
        >
          <Transition name="burger-icon-fade" mode="out-in">
            <svg v-if="!menuOpen" key="burger" width="18" height="18" viewBox="0 0 24 24"
                 fill="none" stroke="currentColor" stroke-width="1.8"
                 stroke-linecap="round" aria-hidden="true">
              <line x1="3" y1="6"  x2="21" y2="6"/>
              <line x1="3" y1="12" x2="21" y2="12"/>
              <line x1="3" y1="18" x2="21" y2="18"/>
            </svg>
            <AppIcon v-else key="close" name="x" :size="18" />
          </Transition>
        </button>

        <!-- The admin views carry their title as <h2>; the page's h1 is
             the application name (axe page-has-heading-one), the size stays via .topbar-brand. -->
        <h1 class="topbar-brand">
          <img class="brand-logo" src="/favicon.svg" alt="" width="24" height="24">
          <span class="brand-name">Statushafen</span>
          <span class="brand-sub">Admin</span>
        </h1>

        <!-- Tenant switcher: compact, the label stays for screen readers. -->
        <div class="tenant-switch">
          <template v-if="pages.length > 0">
            <label for="tenant-select" class="tenant-select-label">{{ t('adminLayout.tenantLabel') }}</label>
            <select
              id="tenant-select"
              v-model="activeTenantId"
              class="tenant-select"
              @change="onTenantChange"
            >
              <option v-for="p in pages" :key="p.ID" :value="p.ID">{{ p.Name }}</option>
            </select>
          </template>
          <div v-else-if="pagesLoading" class="tenant-status" role="status" aria-busy="true">{{ t('common.loading') }}</div>
          <div v-else-if="pagesFailed" class="tenant-status" role="alert">{{ t('adminLayout.statusPagesLoadError') }}</div>
          <div v-else class="tenant-status">{{ t('adminLayout.noStatusPages') }}</div>
        </div>

        <!-- Quick search over pages, settings and docs (Ctrl/Cmd+K, "/"). -->
        <AdminQuickSearch :can-see="canSeeNavItem" />

        <div class="topbar-right">
          <button
            class="icon-btn"
            @click="toggleTheme"
            :aria-label="theme === 'light' ? t('adminLayout.darkModeEnable') : t('adminLayout.lightModeEnable')"
            :title="theme === 'light' ? t('adminLayout.darkMode') : t('adminLayout.lightMode')"
          >
            <AppIcon :name="theme === 'light' ? 'moon' : 'sun'" :size="16" />
          </button>

          <!-- Notification center (bell) -->
          <AdminNotificationBell />

          <LanguageSwitcher />

          <div v-if="currentUser" ref="userMenuRef" class="user-menu" @keydown="onUserMenuKeydown" @focusout="onUserMenuFocusout">
            <button
              ref="userButtonRef"
              class="user-menu-button"
              type="button"
              :aria-label="`${userInitials} ${t('adminLayout.userMenu')}: ${currentUser.email}`"
              aria-haspopup="true"
              aria-controls="admin-user-menu"
              :aria-expanded="userMenuOpen"
              @click="toggleUserMenu"
              @keydown="onUserButtonKeydown"
            >
              <span class="user-avatar" aria-hidden="true">{{ userInitials }}</span>
            </button>
            <div v-if="userMenuOpen" id="admin-user-menu" class="user-menu-panel">
              <div class="user-menu-info">
                <strong>{{ currentUser.email }}</strong>
                <small>{{ displayedRole }}</small>
              </div>
              <!-- Help tooltips on/off per user, also in the profile. -->
              <HelpTooltipsToggle class="user-menu-toggle" />
              <div class="user-menu-actions">
                <!-- Profile link visible to all logged-in users, not only global admins. -->
                <router-link class="user-menu-link" to="/admin/profile" @click="closeUserMenu()">{{ t('nav.profile') }}</router-link>
                <button class="user-menu-link" type="button" @click="goPublicView">{{ t('nav.publicView') }}</button>
                <button class="user-menu-link is-danger" type="button" @click="onLogoutClick">{{ t('nav.logout') }}</button>
              </div>
            </div>
          </div>
        </div>
      </div>

      <div v-if="menuOpen" class="mobile-overlay" aria-hidden="true" @click="menuOpen = false" />
      <nav ref="navRef" class="admin-nav" :class="{ 'is-open': menuOpen }" :aria-label="t('adminLayout.navLabel')">
        <ul class="area-tabs">
          <li v-for="group in visibleNavGroups" :key="group.key">
            <router-link
              :to="group.items[0]!.to"
              class="area-tab"
              :class="{ active: group.key === activeGroupKey }"
              :aria-current="group.key === activeGroupKey ? 'true' : undefined"
            >{{ group.label }}</router-link>
          </li>
        </ul>

        <div class="area-items-row">
          <div
            v-for="group in visibleNavGroups"
            :key="group.key"
            class="area-items"
            :class="{ 'is-shown': group.key === shownGroupKey }"
          >
            <div class="area-items-label">{{ group.label }}</div>
            <ul>
              <li v-for="item in group.items" :key="item.to">
                <router-link :to="item.to" active-class="active">
                  <AppIcon :name="item.icon" :size="16" />
                  {{ item.label }}
                </router-link>
              </li>
            </ul>
          </div>
        </div>
      </nav>
    </header>

    <!-- Content -->
    <div class="admin-body">
      <main ref="contentRef" class="admin-content">
        <!-- Source outage, link to the sources page. -->
        <div v-if="outages.length" class="alert alert-error mb-16 outage-banner" role="alert">
          <div>
            <strong>{{ t('adminLayout.outageBannerTitle') }}</strong>
            <ul class="outage-banner__list">
              <li v-for="o in outages" :key="o.source_id">
                {{ t('adminLayout.outageBannerText', { name: o.source_name, since: outageTime(o.since || o.last_success_at), error: o.last_error || t('adminLayout.outageBannerNoError') }) }}
              </li>
            </ul>
          </div>
          <router-link to="/admin/external-sources" class="outage-banner__link">{{ t('adminLayout.outageBannerLink') }}</router-link>
        </div>
        <router-view v-if="accessState === 'ok'" />
        <AdminForbidden v-else-if="accessState === 'forbidden'" />
        <p v-else-if="accessState === 'loading'" class="tenant-status" role="status" aria-busy="true">{{ t('adminForbidden.loading') }}</p>
      </main>
    </div>
  </div>
</template>
