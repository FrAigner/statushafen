<script setup>
import { ref, computed, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { OnyxBasicPopover } from 'sit-onyx'
import { useAuth } from '../composables/useAuth'
import { useTenant } from '../composables/useTenant'
import { useApi } from '../composables/useApi'
import { grants, ADMIN_DASHBOARD } from '../utils/permissions'
import { useTheme } from '../composables/useTheme'
import { useCurrentPageSlug } from '../composables/useCurrentPageSlug'
import { useLandingChrome } from '../composables/useLandingChrome'
import AppIcon from '../components/AppIcon.vue'
import LanguageSwitcher from '../components/LanguageSwitcher.vue'

const router = useRouter()
const route = useRoute()
const { t } = useI18n()

// Jahr im Footer-Copyright, einmal beim Laden des Layouts ermittelt.
const currentYear = new Date().getFullYear()
const { currentUser, logout, hasPerm } = useAuth()
const { apiFetch } = useApi()
const { myPages, setMyPages, setActiveTenant } = useTenant()
const { theme, toggleTheme } = useTheme()
const { currentPageSlug } = useCurrentPageSlug()
// Auf der Startseite zeigt der Kopf die Richtung-A-Variante (Logo mit
// Marker, Navigation, Anmelden, Bestell-Knopf); LandingPage meldet sich dort an.
const { isLanding, ordersEnabled } = useLandingChrome()

// Auf einer Statusseite nimmt der Login-Knopf die Seite mit, damit
// die Login-Seite ihren Knopf „Mit SSO anmelden“ zeigen kann.
const goToLogin = () => {
  router.push(currentPageSlug.value ? { path: '/login', query: { page: currentPageSlug.value } } : '/login')
}

const isUserMenuOpen = ref(false)

const handleLogout = () => {
  isUserMenuOpen.value = false
  logout()
  router.push('/')
}

// Öffnet den Abo-Editor der aktuell angezeigten Statusseite. Der Editor lebt
// als ausklappbarer Bereich direkt in StatusPage.vue (kein eigenständiger
// Dialog); statt einer Ref-Verdrahtung durch <router-view/> hindurch wird ein
// DOM-Event genutzt, auf das StatusPage.vue lauscht – funktioniert für alle
// Zugriffswege (Subdomain-Root, /status/:slug) ohne Kenntnis der Route hier.
const openSubscriptionSettings = () => {
  isUserMenuOpen.value = false
  window.dispatchEvent(new CustomEvent('statushafen:open-subscription-manager'))
}

// Den Dashboard-Link soll zeigen, wer auf irgendeiner Seite
// admin_dashboard hat, nicht nur, wer gerade einen Tenant aktiv hat (den setzt
// sonst nur StatusPage.vue). Deshalb lädt das Layout die eigenen Seiten selbst,
// sobald jemand angemeldet ist und noch keine bekannt sind (Logout setzt sie zurück).
watch(currentUser, async (user) => {
  if (!user || myPages.value.length > 0) return
  try {
    const pages = await apiFetch('/api/me/pages')
    // Späte Antwort eines inzwischen abgemeldeten oder gewechselten Nutzers verwerfen.
    if (currentUser.value !== user) return
    setMyPages(pages)
  } catch (err) {
    console.error('Konnte Seiten-Berechtigungen nicht laden', err)
  }
}, { immediate: true })

const dashboardPage = computed(() =>
  myPages.value.find(p => grants(p.Permissions, ADMIN_DASHBOARD)))
const showDashboard = computed(() =>
  hasPerm(ADMIN_DASHBOARD) || (!!currentUser.value && !!dashboardPage.value))

const goToDashboard = () => {
  isUserMenuOpen.value = false
  // Aktive Seite ohne Dashboard-Recht (oder keine aktiv): erste passende Seite
  // aktivieren, damit /admin mit deren Rechten öffnet.
  if (!hasPerm(ADMIN_DASHBOARD)) setActiveTenant(dashboardPage.value.ID)
  router.push('/admin') // erster lesbarer Tab statt fest Komponenten
}
</script>

<template>
  <div class="app-container theme-public" :class="{ 'app-container--landing': isLanding }">
    <header class="app-header" :class="{ 'app-header--landing': isLanding }">
     <div class="header-inner">
      <router-link to="/" class="logo" :class="{ 'logo--landing': isLanding }">
        <!-- Marker überall als Inline-SVG mit currentColor (Token --primary),
             kein PNG und kein Filter mehr; Hell/Dunkel folgt dem Token. -->
        <svg
          class="logo-mark"
          width="28"
          height="28"
          viewBox="0 0 64 64"
          fill="none"
          aria-hidden="true"
        >
          <circle cx="32" cy="32" r="27.5" stroke="currentColor" stroke-width="3.4" />
          <g stroke="currentColor" stroke-width="2.8" stroke-linecap="round">
            <path d="M24.5 16a8.5 8.5 0 0 0 0 11" /><path d="M39.5 16a8.5 8.5 0 0 1 0 11" />
            <path d="M20.5 12.5a12.5 12.5 0 0 0 0 18" /><path d="M43.5 12.5a12.5 12.5 0 0 1 0 18" />
          </g>
          <g fill="currentColor">
            <path d="M27 20 32 13l5 7z" /><rect x="28.5" y="19.5" width="7" height="5" />
            <rect x="26.5" y="24" width="11" height="2.6" rx="1" />
            <path d="M28.6 26.6h6.8L38 47H26z" /><rect x="22.5" y="47" width="19" height="4" rx="1.5" />
          </g>
        </svg>
        <span>Statushafen</span>
      </router-link>

      <nav v-if="isLanding" class="lp-nav" :aria-label="t('landing.navLabel')">
        <a href="#features">{{ t('landing.navFeatures') }}</a>
        <a href="#directory">{{ t('landing.navPages') }}</a>
        <router-link to="/docs">{{ t('landing.navDocs') }}</router-link>
      </nav>

      <div class="nav-actions">
        
        <button
          class="header-control header-control--icon theme-toggle"
          @click="toggleTheme"
          :aria-label="theme === 'light' ? t('common.enableDarkMode') : t('common.enableLightMode')"
          :title="theme === 'light' ? t('common.darkMode') : t('common.lightMode')"
        >
          <AppIcon :name="theme === 'light' ? 'moon' : 'sun'" :size="16" />
        </button>

        <LanguageSwitcher class="header-control" />

        <template v-if="currentUser">
          <OnyxBasicPopover
            v-model:open="isUserMenuOpen"
            :label="t('layout.public.userMenu')"
            position="bottom"
            alignment="end"
          >
            <template #default="{ trigger }">
              <button
                v-bind="trigger"
                class="header-control header-control--icon user-menu-trigger"
                :aria-label="t('layout.public.userMenuOpenAria')"
                :title="t('layout.public.userMenu')"
              >
                <AppIcon name="user" :size="16" />
              </button>
            </template>
            <template #content>
              <ul class="user-menu">
                <li class="user-menu-header">
                  <strong>{{ currentUser.email }}</strong>
                  <small v-if="currentUser.role">({{ currentUser.role }})</small>
                </li>
                <li>
                  <button class="user-menu-item" @click="openSubscriptionSettings">
                    <AppIcon name="settings" :size="14" /> {{ t('layout.public.subscriptionSettings') }}
                  </button>
                </li>
                <li v-if="showDashboard">
                  <button class="user-menu-item" @click="goToDashboard">
                    <AppIcon name="grid" :size="14" /> {{ t('layout.public.dashboard') }}
                  </button>
                </li>
                <li>
                  <button class="user-menu-item user-menu-item--danger" @click="handleLogout">
                    <AppIcon name="logout" :size="14" /> {{ t('nav.logout') }}
                  </button>
                </li>
              </ul>
            </template>
          </OnyxBasicPopover>
        </template>
        <!-- Auf der Anmeldeseite selbst ist ein Anmelden-Knopf überflüssig. -->
        <template v-else-if="route.path !== '/login'">
          <button class="header-control header-control--text login-button" @click="goToLogin">
            {{ t('nav.login') }}
          </button>
        </template>

        <router-link v-if="isLanding && ordersEnabled" to="/order" class="btn btn-primary order-button">
          {{ t('landing.ctaOrder') }}
        </router-link>
      </div>
     </div>
    </header>

    <!-- <main> als Hauptlandmarke. Damit liegt der Seiteninhalt in einer
         Landmarke (axe region/landmark-one-main), und ein <header> innerhalb einer
         View (z. B. .status-header) zählt nicht mehr als zweites banner. -->
    <main class="app-body">
      <router-view />
    </main>

    <!-- Schlichter Footer in derselben Breite wie die Kopfzeile.
         Weitere Links (z. B. Impressum, Datenschutz) kommen als zusätzliche
         <li> in .footer-links, sobald es die Seiten gibt. -->
    <footer class="app-footer">
      <nav class="footer-nav" :aria-label="t('layout.footer.navLabel')">
        <ul class="footer-links">
          <li><router-link to="/docs" class="footer-link">{{ t('layout.footer.docs') }}</router-link></li>
        </ul>
      </nav>
      <p class="footer-copyright">{{ t('layout.footer.copyright', { year: currentYear }) }}</p>
    </footer>
  </div>
</template>

<style scoped>
/* gemeinsame Größe für alle Bedienelemente der Kopfzeile
   (44 px = Mindestzielgröße für Berührung). .action-btn bleibt den
   Tabellenknöpfen vorbehalten. */
.app-header {
  --header-control-size: 44px;
  /* Die Zielfläche bleibt 44 px, sichtbar sind nur 34 px (Vorlage
     30-36 px): ein transparenter Rand der Stärke --header-control-ring vergrößert
     die klickbare Fläche, Hintergrund (background-clip: padding-box) und
     Rahmen (inset box-shadow) liegen innerhalb davon. */
  --header-control-ring: 5px;
  /* Ohne gap stieß die Wortmarke bei 390 px direkt an den
     Theme-Knopf; 15 px wie zwischen den übrigen Knöpfen (.nav-actions). */
  gap: 15px;
  /* Logo und Bedienelemente brechen bei schmalen Breiten in eine
     zweite Zeile um, statt die Seite seitlich aufzuschieben (400 px: +39 px). */
  flex-wrap: wrap;
  row-gap: var(--sp-2);
}

/* war Inline-Style; flex-wrap + min-width verhindern bei 320 px den
   Überlauf (Thema + Sprache + Anmelden sind zusammen 287 px breit). */
.nav-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 15px;
  min-width: 0;
  max-width: 100%;
}

/* Logo: Leuchtturm-Icon + Wortmarke nebeneinander (Icon ist ein Inline-SVG
   mit currentColor). */
.logo {
  display: inline-flex;
  align-items: center;
  gap: 10px;
}

/* Logo und Name führen zur Startseite (2026-10-29), sehen aber weiter wie
   die Wortmarke aus statt wie ein Textlink. */
a.logo {
  color: inherit;
  text-decoration: none;
  border-radius: var(--radius-sm);
}

a.logo:focus-visible {
  outline: 2px solid var(--focus);
  outline-offset: 4px;
}

.logo-mark {
  display: block;
  flex-shrink: 0;
  color: var(--primary);
}

.header-control {
  box-sizing: border-box;
  height: var(--header-control-size);
  border: var(--header-control-ring) solid transparent;
  border-radius: calc(var(--radius-sm) + var(--header-control-ring));
  background: var(--surface);
  background-clip: padding-box;
  box-shadow: inset 0 0 0 1px var(--border);
  color: var(--text);
  font-family: var(--font-sans);
  cursor: pointer;
}

.header-control:hover {
  background-color: var(--surface-2);
}

.header-control:focus-visible {
  outline: 2px solid var(--focus);
  outline-offset: calc(2px - var(--header-control-ring));
}

.header-control--icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: var(--header-control-size);
  padding: 0;
}

/* Text-Bedienelement der Kopfzeile (z. B. Login), damit es exakt wie
   Theme-Toggle und Sprachauswahl aussieht (gleiche Höhe/Rahmen/Radius/Font)
   statt als abweichend gestylter OnyxButton mit Serifen-Fallback. */
.header-control--text {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0 16px;
  font-size: 0.9rem;
  font-weight: 500;
}

/* Spezifischer als .language-switcher in LanguageSwitcher.vue, damit die
   Komponente an anderen Stellen unverändert bleibt. LanguageSwitcher hat zwei
   Wurzelelemente (select + Fehlermeldung), deshalb bekommt das <select> das
   scoped-Attribut dieses Layouts nicht und .header-control griff nie (28 statt
   44 px hoch, 2026-10-29). :deep() reicht die Regeln hinein. */
.app-header :deep(.header-control.language-switcher) {
  box-sizing: border-box;
  height: var(--header-control-size);
  padding: 0 8px;
  border: var(--header-control-ring) solid transparent;
  border-radius: calc(var(--radius-sm) + var(--header-control-ring));
  background-color: var(--surface);
  background-clip: padding-box;
  box-shadow: inset 0 0 0 1px var(--border);
  color: var(--text);
  font-family: var(--font-sans);
  font-size: 0.85rem;
  /* Eigener Pfeil statt des Browser-Standards (wie im Admin-Bereich, style.css):
     Chevron aus zwei Verlaufs-Diagonalen in currentColor. */
  appearance: none;
  -webkit-appearance: none;
  padding-right: 28px;
  cursor: pointer;
  background-image:
    linear-gradient(to top right, transparent calc(50% - 0.9px), currentColor calc(50% - 0.9px) calc(50% + 0.9px), transparent calc(50% + 0.9px)),
    linear-gradient(to bottom right, transparent calc(50% - 0.9px), currentColor calc(50% - 0.9px) calc(50% + 0.9px), transparent calc(50% + 0.9px));
  background-repeat: no-repeat;
  background-size: 5px 5px, 5px 5px;
  background-position: right 15px center, right 10px center;
}

/* Footer. Liegt in .app-container und hat damit dieselbe Breite
   und dieselben Kanten wie die Kopfzeile; Farben nur über Tokens, damit Hell
   und Dunkel ohne eigene Regeln funktionieren. */
.app-footer {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  align-items: center;
  gap: 8px 24px;
  margin-top: 48px;
  padding-top: 16px;
  border-top: 1px solid var(--border);
  color: var(--text-2);
  font-family: var(--font-sans);
  font-size: 0.8rem;
}

.footer-links {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 20px;
  list-style: none;
  margin: 0;
  padding: 0;
}

.footer-link {
  color: var(--text-2);
  border-radius: var(--radius-sm);
}

.footer-link:hover {
  color: var(--text);
}

.footer-link:focus-visible {
  outline: 2px solid var(--focus);
  outline-offset: 2px;
}

.footer-copyright {
  margin: 0;
}

/* ============================================================
   Startseite (Richtung A): volle, klebende Kopfzeile und
   randloser Inhalt. Gilt nur mit .app-container--landing; Statusseiten und
   übrige öffentliche Seiten behalten den schlichten Kopf.
   ============================================================ */
.header-inner {
  display: contents;
}

.app-container.app-container--landing {
  max-width: none;
  padding: 0;
}

.app-header--landing {
  position: sticky;
  top: 0;
  z-index: 10;
  margin-bottom: 0;
  background: color-mix(in srgb, var(--bg) 85%, transparent);
  border-bottom: 1px solid var(--border);
  backdrop-filter: saturate(1.4) blur(8px);
}

.app-header--landing .header-inner {
  display: flex;
  align-items: center;
  gap: var(--sp-5);
  box-sizing: border-box;
  width: 100%;
  max-width: var(--lp-max);
  min-height: 64px;
  margin: 0 auto;
  padding: 0 var(--sp-5);
}

.logo--landing {
  gap: 8px;
  font-size: var(--fs-md);
  font-weight: 700;
  line-height: 1;
  color: var(--text);
}

.lp-nav {
  display: flex;
  gap: var(--sp-4);
  font-weight: 500;
}

.lp-nav a {
  display: inline-flex;
  align-items: center;
  min-height: 44px;
  color: var(--text-2);
  text-decoration: none;
  border-radius: var(--r-sm);
}

.lp-nav a:hover {
  color: var(--text);
}

.lp-nav a:focus-visible {
  outline: 2px solid var(--focus);
  outline-offset: 2px;
}

.app-header--landing .nav-actions {
  margin-left: auto;
}

/* „Anmelden“ als Textlink: ohne Rahmen und Fläche, gleiche Zielgröße. */
.app-header--landing .login-button {
  box-shadow: none;
  background: transparent;
  background-clip: padding-box;
  color: var(--text-2);
}

.app-header--landing .login-button:hover {
  background-color: var(--surface-2);
  color: var(--text);
}

.app-header .order-button {
  min-height: var(--header-control-size);
  border: var(--header-control-ring) solid transparent;
  border-radius: calc(var(--radius-md) + var(--header-control-ring));
  background-clip: padding-box;
  padding: 0 var(--sp-4);
  text-decoration: none;
  white-space: nowrap;
}

/* .btn-primary:hover setzt border-color; der Rand muss transparent bleiben. */
.app-header .order-button:hover {
  border-color: transparent;
}

/* Footer unverändert im Inhalt; auf der Startseite läuft die Linie randlos und
   der Inhalt fluchtet mit der Kopfzeile (--lp-max). */
.app-container--landing .app-footer {
  margin-top: 0;
  padding: var(--sp-5) max(var(--sp-5), calc((100% - var(--lp-max)) / 2 + var(--sp-5)));
  background: var(--surface);
}

@media (max-width: 640px) {
  /* Mobil: Logo und Bedienelemente in der ersten Zeile, die Navigation
     rutscht in eine zweite Zeile; der Bestell-Knopf steht im Hero. */
  .app-header--landing .header-inner {
    flex-wrap: wrap;
    gap: 0 var(--sp-3);
    padding: var(--sp-2) var(--sp-4);
  }

  /* 15 px Abstand sind zu breit für eine Zeile mit Logo. */
  .app-header--landing .nav-actions {
    gap: var(--sp-2);
  }

  .lp-nav {
    order: 3;
    flex: 1 0 100%;
    flex-wrap: wrap;
    gap: 0 var(--sp-4);
  }

  .app-header--landing .login-button {
    padding: 0 var(--sp-2);
  }

  .app-header .order-button {
    display: none;
  }

  .app-container--landing .app-footer {
    padding-inline: var(--sp-4);
  }
}

.user-menu {
  font-family: var(--font-sans);
  list-style: none;
  margin: 0;
  padding: 6px;
  min-width: 220px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  box-shadow: var(--shadow-2);
}

.user-menu-header {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 8px 10px;
  border-bottom: 1px solid var(--border);
  margin-bottom: 4px;
}

.user-menu-header strong {
  font-size: 0.9rem;
  color: var(--text);
  word-break: break-all;
}

.user-menu-header small {
  color: var(--text-secondary);
  font-size: 0.75rem;
}

.user-menu-item {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 8px 10px;
  border: none;
  background: transparent;
  border-radius: var(--radius-sm);
  color: var(--text);
  font-size: 0.85rem;
  font-family: var(--font-sans);
  cursor: pointer;
  text-align: left;
}

.user-menu-item:hover {
  background: var(--surface-2);
}

.user-menu-item--danger {
  color: var(--major);
}
</style>
