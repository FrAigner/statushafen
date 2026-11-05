<script setup>
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
import AdminNotificationBell from '../components/AdminNotificationBell.vue'
import LanguageSwitcher from '../components/LanguageSwitcher.vue'
import HelpTooltipsToggle from '../components/HelpTooltipsToggle.vue'

const router = useRouter()
const route  = useRoute()
const { t, locale } = useI18n()
const { currentUser, logout, hasPerm, isGlobalAdmin } = useAuth()
const { theme, toggleTheme } = useTheme()
const { apiFetch } = useApi()
const { activeTenantId, setActiveTenant, setMyPages, activeTenantRoleName } = useTenant()

// Zeigt die tatsächlich für die aktive Statusseite geltende Rolle (Page Admin,
// Editor, ...) statt der u.U. veralteten globalen Rolle aus dem Login-JWT.
const displayedRole = computed(() => {
  if (isGlobalAdmin.value) return 'Global Admin'
  return activeTenantRoleName.value || currentUser.value?.role || ''
})

const pages = ref([])
const pagesLoading = ref(true)
const pagesFailed = ref(false)

onMounted(async () => {
  try {
    const res = await apiFetch('/api/me/pages')
    pages.value = res || []
    setMyPages(res)
    // Auch einen gespeicherten Tenant ersetzen, der nicht (mehr)
    // in der Liste steht - nach einem Seiten-SSO-Login liefert das Backend
    // nur die SSO-Seite, localStorage kann noch eine andere enthalten.
    const known = pages.value.some(p => String(p.ID) === String(activeTenantId.value))
    if (pages.value.length > 0 && !known) {
      setActiveTenant(pages.value[0].ID)
    }
  } catch (err) {
    pagesFailed.value = true
    console.error('Failed to load status pages', err)
  } finally {
    pagesLoading.value = false
  }
})

// Seiten-Overrides sind strikt an den Lebenszyklus
// dieses Layouts gebunden, damit sie garantiert nur innerhalb von /admin/*
// wirken und niemals auf der öffentlichen Statusseite desselben Mandanten -
// auch wenn activeTenantId (persistiert in localStorage via useTenant.js)
// über das Verlassen des Admin-Bereichs hinaus bestehen bleibt.
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

// Redesign Stufe 2: Die Seitenleiste wird zu Bereichs-Reitern
// (Betrieb, Inhalte, Verwaltung, System) mit einer zweiten Zeile für die
// Einträge des aktiven Bereichs. Die Aufteilung folgt der Aufgabe: Betrieb ist
// der Tagesbetrieb einer Statusseite, Inhalte alles, was Besucher lesen
// (Mail-Templates, Sprachen, UI-Texte), Verwaltung die mandantenübergreifende
// Pflege, System der Betrieb der Plattform selbst.
const navGroups = computed(() => NAV_GROUPS.map(group => ({
  key: group.key,
  label: t(`adminNav.groups.${group.key}`),
  items: group.items.map(item => ({ ...item, label: t(`adminNav.items.${item.labelKey}`) })),
})))

// Ein Tab ist sichtbar <=> Leserecht auf seine Ressource (create,
// update und delete schließen read ein; globale Ressourcen nur für Global
// Admins, siehe useAuth.hasPerm). Die Doku braucht kein Recht.
const canSeeNavItem = (item) => !item.resource || ((!item.globalOnly || isGlobalAdmin.value) && hasPerm(`${item.resource}:read`))

// Nur sichtbare Einträge rendern und Bereiche ohne sichtbaren
// Eintrag ganz ausblenden - sonst blieben für Viewer leere Reiter stehen.
const visibleNavGroups = computed(() => navGroups.value
  .map(group => ({ ...group, items: group.items.filter(canSeeNavItem) }))
  .filter(group => group.items.length > 0))

// Aktiver Bereich: der Bereich des Eintrags mit dem längsten passenden Pfad
// (/admin/incidents/5/translations gehört so zu Vorfälle). Gehört die Route zu
// keinem Eintrag (z. B. /admin/profile), ist kein Reiter aktiv, die zweite
// Zeile zeigt dann den ersten Bereich.
const activeGroupKey = computed(() => {
  let best = null
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

// Ausfall-Banner. Fällt eine externe Quelle dauerhaft
// aus (Frist je Quelle, Standard 60 Minuten ohne Erfolg), meldet das Backend sie
// unter external-outages; das Banner bleibt, bis der nächste Abruf klappt. Wie
// die Ausfall-Mail nur für Bearbeiter (external_sources:update). Ein Ladefehler
// blendet das Banner nur aus, die Seite läuft weiter.
const outages = ref([])
const loadOutages = async (pageId) => {
  if (!pageId || !hasPerm('external_sources:update')) {
    outages.value = []
    return
  }
  try {
    const res = await apiFetch(`/api/pages/${pageId}/external-outages`)
    // Antwort verworfen, falls inzwischen eine andere Statusseite aktiv ist.
    if (String(pageId) === String(activeTenantId.value)) outages.value = Array.isArray(res) ? res : []
  } catch (err) {
    outages.value = []
    console.error('Failed to load external outages', err)
  }
}
watch([activeTenantId, () => route.path], ([pageId]) => loadOutages(pageId), { immediate: true })
const outageTime = (iso) => (iso ? new Date(iso).toLocaleString(locale.value) : '')

// Zugriff auf den aktuellen Tab. Die Route nennt das nötige Recht
// (meta.permission, meist `<ressource>:read`); fehlt es, erscheint die Seite
// „Keine Berechtigung“ anstelle der View. Die View wird dann nicht geladen,
// macht also keine API-Aufrufe, und es gibt keine Weiterleitung ins Leere.
// Solange die Rollen der Statusseiten noch laden, ist das Recht unbekannt
// („loading“); schlägt das Laden fehl, entscheidet das Backend (403 in der View).
// Die Startseite (/admin) führt zum ersten lesbaren Tab.
const homeTarget = computed(() => firstAllowedItem(hasPerm, isGlobalAdmin.value)?.to ?? null)
const accessState = computed(() => {
  const required = route.meta.permission
  const isHome = route.meta.home === true
  if (!required && !isHome) return 'ok'
  if (pagesLoading.value && !isGlobalAdmin.value) return 'loading'
  if (isHome) return homeTarget.value ? 'redirect' : 'forbidden'
  if (pagesFailed.value) return 'ok'
  if (route.meta.globalOnly && !isGlobalAdmin.value) return 'forbidden'
  return hasPerm(required) ? 'ok' : 'forbidden'
})
watch(accessState, (state) => {
  if (state === 'redirect') router.replace(homeTarget.value)
}, { immediate: true })

const menuOpen = ref(false)
const userMenuOpen = ref(false)
const burgerRef = ref(null)
const navRef = ref(null)
watch(route, () => { menuOpen.value = false; userMenuOpen.value = false })

// Phase 7b: Body-Scroll sperren, solange das mobile Menü als Overlay offen ist,
// damit der Hintergrund nicht gleichzeitig mitscrollt.
watch(menuOpen, (isOpen) => {
  document.body.style.overflow = isOpen ? 'hidden' : ''
  if (isOpen) {
    // Fokus auf den ersten Eintrag, damit Tastaturnutzer im Menü landen.
    nextTick(() => navRef.value?.querySelector('.area-items a')?.focus())
  }
})

// Benutzermenü: Disclosure-Knopf mit Panel. Kein role="menu", weil
// das Panel auch den Hilfe-Schalter enthält (kein Menüeintrag). Pfeiltasten
// wandern trotzdem durch die Einträge, Escape schließt und gibt den Fokus
// zurück an den Knopf, Tab aus dem Panel heraus schließt es.
const userMenuRef = ref(null)
const userButtonRef = ref(null)

const userInitials = computed(() => currentUser.value.email.slice(0, 2).toUpperCase())

const userMenuFocusables = () => Array.from(
  userMenuRef.value.querySelectorAll('.user-menu-panel a[href], .user-menu-panel button, .user-menu-panel input')
)

const closeUserMenu = ({ restoreFocus = false } = {}) => {
  userMenuOpen.value = false
  if (restoreFocus) userButtonRef.value?.focus()
}

const openUserMenu = () => {
  userMenuOpen.value = true
  nextTick(() => userMenuFocusables()[0]?.focus())
}

const toggleUserMenu = () => (userMenuOpen.value ? closeUserMenu() : openUserMenu())

const onUserButtonKeydown = (e) => {
  if (e.key === 'ArrowDown' && !userMenuOpen.value) {
    e.preventDefault()
    openUserMenu()
  }
}

const onUserMenuKeydown = (e) => {
  const items = userMenuFocusables()
  const index = items.indexOf(document.activeElement)
  const moves = { ArrowDown: index + 1, ArrowUp: index - 1, Home: 0, End: items.length - 1 }
  if (e.key in moves && userMenuOpen.value) {
    e.preventDefault()
    items[(moves[e.key] + items.length) % items.length]?.focus()
  }
}

const onUserMenuFocusout = (e) => {
  if (e.relatedTarget && !userMenuRef.value?.contains(e.relatedTarget)) closeUserMenu()
}

const onDocumentClick = (e) => {
  if (userMenuOpen.value && !userMenuRef.value?.contains(e.target)) closeUserMenu()
}

const goPublicView = () => {
  closeUserMenu()
  router.push('/')
}

const onLogoutClick = () => {
  closeUserMenu()
  handleLogout()
}

const onKeydown = (e) => {
  if (e.key !== 'Escape') return
  if (userMenuOpen.value) {
    closeUserMenu({ restoreFocus: true })
  } else if (menuOpen.value) {
    menuOpen.value = false
    burgerRef.value?.focus()
  }
}
// Erfolgsmeldungen der Ansichten blenden sich nach ein paar Sekunden aus (siehe Composable).
const contentRef = ref(null)
useAutoDismissAlerts(contentRef)
onMounted(() => {
  window.addEventListener('keydown', onKeydown)
  document.addEventListener('click', onDocumentClick)
})
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeydown)
  document.removeEventListener('click', onDocumentClick)
  document.body.style.overflow = ''
  clearPageOverrides(locale.value)
})
</script>

<template>
  <div class="admin-shell theme-admin">

    <!-- Kopfzeile (dunkel) mit Marke, Mandanten-Umschalter und
         Benutzermenü, darunter Bereichs-Reiter und die Einträge des aktiven
         Bereichs. Alles zusammen ist sticky (.admin-topbar). -->
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

        <!-- Die Admin-Views führen ihren Titel als <h2>; die h1 der Seite ist
             der Anwendungsname (axe page-has-heading-one), Größe bleibt über .topbar-brand. -->
        <h1 class="topbar-brand">
          <img class="brand-logo" src="/favicon.svg" alt="" width="24" height="24">
          <span class="brand-name">Statushafen</span>
          <span class="brand-sub">Admin</span>
        </h1>

        <!-- Mandanten-Umschalter: kompakt, das Label bleibt für Screenreader. -->
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

        <div class="topbar-right">
          <button
            class="icon-btn"
            @click="toggleTheme"
            :aria-label="theme === 'light' ? t('adminLayout.darkModeEnable') : t('adminLayout.lightModeEnable')"
            :title="theme === 'light' ? t('adminLayout.darkMode') : t('adminLayout.lightMode')"
          >
            <AppIcon :name="theme === 'light' ? 'moon' : 'sun'" :size="16" />
          </button>

          <!-- Nachrichtencenter (Glocke) -->
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
              <!-- Hilfe-Tooltips pro Benutzer an/aus, auch im Profil. -->
              <HelpTooltipsToggle class="user-menu-toggle" />
              <div class="user-menu-actions">
                <!-- Profil-Link für alle eingeloggten User sichtbar, nicht nur Global Admins. -->
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
              :to="group.items[0].to"
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
        <!-- Quellen-Ausfall, Link auf die Quellenseite. -->
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
