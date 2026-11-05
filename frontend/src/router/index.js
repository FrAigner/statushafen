import { createRouter, createWebHistory } from 'vue-router'
import { useAuth } from '../composables/useAuth'
import { isBasicAuthChallenge } from '../composables/useApi'


// Layouts bleiben eager: praktisch jede Route braucht ohnehin eines der beiden
// (zielt auf die - deutlich größeren - View-Komponenten ab).
import PublicLayout from '../layouts/PublicLayout.vue'
import AdminLayout from '../layouts/AdminLayout.vue'

// Alle Admin- und Public-Views per dynamischem import() statt statischem
// Top-Level-Import geladen: Vite erzeugt daraus pro View einen
// eigenen Chunk, der erst beim tatsächlichen Aufruf der Route nachgeladen
// wird, statt den kompletten Admin-Bereich in den initialen Bundle
// aufzunehmen.
const PublicRoot = () => import('../views/public/PublicRoot.vue')
const StatusPage = () => import('../views/public/StatusPage.vue')
const Unsubscribe = () => import('../views/public/Unsubscribe.vue')
const SubscribeConfirmPage = () => import('../views/public/SubscribeConfirmPage.vue')
const InvitationPage = () => import('../views/public/InvitationPage.vue')
const OrderPage = () => import('../views/public/OrderPage.vue')
const OrderConfirmPage = () => import('../views/public/OrderConfirmPage.vue')
const OrderSetPasswordPage = () => import('../views/public/OrderSetPasswordPage.vue')
const DocsPage = () => import('../views/public/DocsPage.vue')
const LoginView = () => import('../views/auth/LoginView.vue')
const SSOCallback = () => import('../views/auth/SSOCallback.vue')

const AdminComponents = () => import('../views/admin/AdminComponents.vue')
const AdminExternalSources = () => import('../views/admin/AdminExternalSources.vue')
const AdminIncidents = () => import('../views/admin/AdminIncidents.vue')
const AdminMaintenance = () => import('../views/admin/AdminMaintenance.vue')
const AdminNotices = () => import('../views/admin/AdminNotices.vue')
const AdminIncidentTemplates = () => import('../views/admin/AdminIncidentTemplates.vue')
const AdminNotificationTemplates = () => import('../views/admin/AdminNotificationTemplates.vue')
const AdminSubscribers = () => import('../views/admin/AdminSubscribers.vue')
const AdminUsers = () => import('../views/admin/AdminUsers.vue')
const AdminRoles = () => import('../views/admin/AdminRoles.vue')
const AdminTags = () => import('../views/admin/AdminTags.vue')
const AdminLanguages = () => import('../views/admin/AdminLanguages.vue')
const AdminStatusPages = () => import('../views/admin/AdminStatusPages.vue')
const AdminSSO = () => import('../views/admin/AdminSSO.vue')
const AdminOverview = () => import('../views/admin/AdminOverview.vue')
const AdminMonitoring = () => import('../views/admin/AdminMonitoring.vue')
const AdminAuditLog = () => import('../views/admin/AdminAuditLog.vue')
const AdminIncidentDetail = () => import('../views/admin/AdminIncidentDetail.vue')
const ProfileView = () => import('../views/admin/ProfileView.vue')
const AdminIncidentTranslations = () => import('../views/admin/AdminIncidentTranslations.vue')
const AdminComponentTranslations = () => import('../views/admin/AdminComponentTranslations.vue')
const AdminGeneralNoticeTranslations = () => import('../views/admin/AdminGeneralNoticeTranslations.vue')
const AdminUiTranslations = () => import('../views/admin/AdminUiTranslations.vue')
const AdminSystemSettings = () => import('../views/admin/AdminSystemSettings.vue')
const AdminHome = () => import('../views/admin/AdminHome.vue')

const routes = [
  {
    path: '/',
    component: PublicLayout,
    children: [
      { path: '', component: PublicRoot }
    ]
  },
  {
    // Host-unabhängiger Zugriffsweg auf Mandanten-Statusseiten (z.B. für
    // Deployments ohne eigene Wildcard-Domain, wo Subdomains wie
    // "acme.<host>" nicht verfügbar sind).
    path: '/status/:slug',
    component: PublicLayout,
    children: [
      { path: '', component: StatusPage, props: true }
    ]
  },
  {
    // Self-Service-Unsubscribe-Link aus Benachrichtigungsmails,
    // authentifiziert per signiertem ?token= statt Login.
    path: '/unsubscribe',
    component: PublicLayout,
    children: [
      { path: '', component: Unsubscribe }
    ]
  },
  {
    // Double-Opt-in-Link aus der Bestätigungsmail eines E-Mail-Abos.
    path: '/subscribe/confirm',
    component: PublicLayout,
    children: [
      { path: '', component: SubscribeConfirmPage }
    ]
  },
  {
    // Einladungslink auf eine Statusseite (annehmen/ablehnen).
    path: '/invitation',
    component: PublicLayout,
    children: [
      { path: '', component: InvitationPage }
    ]
  },
  {
    // Self-Service-Bestellung neuer Statusseiten ("Eingangstor"-
    // Homepage) - Ablauf: Formular -> Email-Bestätigung -> Passwort setzen,
    // siehe handlers/page_order.go im Backend für den vollständigen Flow.
    path: '/order',
    component: PublicLayout,
    children: [
      { path: '', component: OrderPage },
      { path: 'confirm', component: OrderConfirmPage },
      { path: 'set-password', component: OrderSetPasswordPage }
    ]
  },
  {
    // Eingebaute Dokumentation, öffentlich (normaler SPA-Pfad,
    // daher keine eigene Basic-Auth-Ausnahme in Caddy nötig). Ein Abschnitt je
    // Unterseite, /docs zeigt den ersten.
    path: '/docs',
    component: PublicLayout,
    children: [
      { path: ':section?', component: DocsPage }
    ]
  },
  {
    path: '/login',
    component: PublicLayout,
    children: [
      { path: '', component: LoginView }
    ]
  },
  {
    // SSO Callback – kein Layout nötig
    path: '/auth/sso-callback',
    component: SSOCallback
  },
  {
    path: '/admin',
    component: AdminLayout,
    meta: { requiresAuth: true },
    children: [
      // Startseite des Admin-Bereichs. AdminLayout führt (meta.home) zum
      // ersten Tab, den der Nutzer lesen darf, oder zeigt „Keine Berechtigung“.
      { path: '', component: AdminHome, meta: { home: true } },
      // meta.permission nennt das Recht, das die Seite zum Lesen
      // braucht (`<ressource>:read` des Tabs, siehe utils/adminNav.js). Fehlt es,
      // zeigt AdminLayout die Seite „Keine Berechtigung“ statt der View (keine
      // API-Aufrufe, keine Weiterleitung). Globale Ressourcen (overview, users, ...)
      // sind nur für Global Admins lesbar, das regelt useAuth.hasPerm.
      { path: 'overview', component: AdminOverview, meta: { permission: 'overview:read' } },
      { path: 'monitoring', component: AdminMonitoring, meta: { permission: 'monitoring:read' } },
      { path: 'pages', component: AdminStatusPages, meta: { permission: 'pages:read', globalOnly: true } },
      { path: 'components', component: AdminComponents, meta: { permission: 'components:read' } },
      { path: 'external-sources', component: AdminExternalSources, meta: { permission: 'external_sources:read' } },
      { path: 'incidents', component: AdminIncidents, meta: { permission: 'incidents:read' } },
      { path: 'incidents/:pageId/:id', component: AdminIncidentDetail, meta: { permission: 'incidents:read' } },
      { path: 'maintenance', component: AdminMaintenance, meta: { permission: 'maintenance:read' } },
      { path: 'notices', component: AdminNotices, meta: { permission: 'notices:read' } },
      { path: 'incident-templates', component: AdminIncidentTemplates, meta: { permission: 'incident_templates:read' } },
      { path: 'notification-templates', component: AdminNotificationTemplates, meta: { permission: 'notification_templates:read' } },
      { path: 'subscribers', component: AdminSubscribers, meta: { permission: 'subscribers:read' } },
      { path: 'audit', component: AdminAuditLog, meta: { permission: 'audit:read' } },
      { path: 'users', component: AdminUsers, meta: { permission: 'users:read' } },
      { path: 'roles', component: AdminRoles, meta: { permission: 'roles:read' } },
      { path: 'tags', component: AdminTags, meta: { permission: 'tags:read' } },
      // Sprachverwaltung: Ein Seiten-Admin ohne Global-Admin-Rechte
      // muss die Sprachen seiner eigenen Statusseite aktivieren können. Der
      // global-admin-only-Teil (Sprachkatalog anlegen/bearbeiten) blendet sich
      // innerhalb der Komponente selbst per isGlobalAdmin aus.
      { path: 'languages', component: AdminLanguages, meta: { permission: 'languages:read' } },
      // props: true, sonst kommt :incidentId usw. nicht als Prop
      // in der View an und sie lädt `…/undefined`.
      { path: 'incidents/:incidentId/translations', component: AdminIncidentTranslations, props: true, meta: { permission: 'incidents:read' } },
      { path: 'components/:componentId/translations', component: AdminComponentTranslations, props: true, meta: { permission: 'components:read' } },
      { path: 'notices/:noticeId/translations', component: AdminGeneralNoticeTranslations, props: true, meta: { permission: 'notices:read' } },
      // SSO je Statusseite: `sso:read` auf der Seite.
      { path: 'sso', component: AdminSSO, meta: { permission: 'sso:read' } },
      // UI-Übersetzungs-Editor: ein Seiten-Admin muss die
      // UI-Texte seiner eigenen Seite überschreiben können. Der global-admin-only
      // Scope blendet sich innerhalb der Komponente per isGlobalAdmin aus.
      { path: 'ui-translations', component: AdminUiTranslations, meta: { permission: 'ui_translations:read' } },
      // Systemeinstellungen: erste globale Plattform-Einstellung
      // (Self-Service-Bestellung an/aus).
      { path: 'system-settings', component: AdminSystemSettings, meta: { permission: 'system_settings:read' } },
      // Minimale Profilseite - für alle eingeloggten User, nicht
      // nur Global Admins, da hier (vorerst nur) die eigene UI-Sprache
      // eingestellt wird.
      { path: 'profile', component: ProfileView }
    ]
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

// Global Navigation Guard
router.beforeEach(async (to, from, next) => {
  const { isLoggedIn, token, setAuth, logout } = useAuth()
  
  // Verify token on first load if it exists but user profile is missing
  if (token.value && !isLoggedIn.value) {
    try {
      const apiBase = import.meta.env.VITE_API_URL || ''
      // Direct fetch to avoid circular dependency in useApi inside guard
      const res = await fetch(`${apiBase}/api/me`, {
        // X-Auth-Token statt Authorization (dort steht Caddys Basic Auth)
        headers: { 'X-Auth-Token': token.value }
      })
      // Caddys Basic-Auth-401 sagt nichts über das App-Token.
      if ((res.status === 401 && !isBasicAuthChallenge(res)) || res.status === 403) {
        logout()
      } else if (res.ok) {
        const data = await res.json()
        setAuth(token.value, data)
      } else {
        console.warn(`Verifizierungs-Serverfehler: Status ${res.status}`)
      }
    } catch (err) {
      console.error('Verbindung zum Auth-Server fehlgeschlagen:', err)
    }
  }

  // Check route meta
  if (to.meta.requiresAuth && !isLoggedIn.value) {
    next('/login')
  } else {
    next()
  }
})

export default router
