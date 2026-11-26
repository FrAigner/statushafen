import { createRouter, createWebHistory } from 'vue-router'
import type { RouteRecordRaw } from 'vue-router'
import { useAuth } from '../composables/useAuth'
import { isBasicAuthChallenge } from '../composables/useApi'
import { safeReturnTo } from '../utils/safeReturnTo'
import type { AuthUser } from '../types/api'

declare module 'vue-router' {
  interface RouteMeta {
    /** The route (and its children) needs a signed-in user. */
    requiresAuth?: boolean
    /** Start page of the admin area: AdminLayout leads to the first readable tab. */
    home?: boolean
    /** Permission needed to read the page (`<resource>:read`, see utils/adminNav.ts). */
    permission?: string
    /** Only global admins may open the page. */
    globalOnly?: boolean
    /** Base path of the docs links when the docs are shown inside the admin frame. */
    docsBase?: string
  }
}

// Layouts stay eager: practically every route needs one of the two anyway
// (this targets the - considerably larger - view components).
import PublicLayout from '../layouts/PublicLayout.vue'
import AdminLayout from '../layouts/AdminLayout.vue'

// All admin and public views are loaded via dynamic import() instead of a static
// top-level import: Vite creates a separate chunk per view from it, which
// is only loaded when the route is actually visited, instead of putting the
// complete admin area into the initial bundle.
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

const routes: RouteRecordRaw[] = [
  {
    path: '/',
    component: PublicLayout,
    children: [
      { path: '', component: PublicRoot }
    ]
  },
  {
    // Host-independent access path to tenant status pages (e.g. for
    // deployments without their own wildcard domain, where subdomains like
    // "acme.<host>" are not available).
    path: '/status/:slug',
    component: PublicLayout,
    children: [
      { path: '', component: StatusPage, props: true }
    ]
  },
  {
    // Self-service unsubscribe link from notification mails,
    // authenticated via a signed ?token= instead of a login.
    path: '/unsubscribe',
    component: PublicLayout,
    children: [
      { path: '', component: Unsubscribe }
    ]
  },
  {
    // Double opt-in link from the confirmation mail of an email subscription.
    path: '/subscribe/confirm',
    component: PublicLayout,
    children: [
      { path: '', component: SubscribeConfirmPage }
    ]
  },
  {
    // Invitation link to a status page (accept/decline).
    path: '/invitation',
    component: PublicLayout,
    children: [
      { path: '', component: InvitationPage }
    ]
  },
  {
    // Self-service ordering of new status pages ("gateway"
    // homepage) - flow: form -> email confirmation -> set password,
    // see handlers/page_order.go in the backend for the complete flow.
    path: '/order',
    component: PublicLayout,
    children: [
      { path: '', component: OrderPage },
      { path: 'confirm', component: OrderConfirmPage },
      { path: 'set-password', component: OrderSetPasswordPage }
    ]
  },
  {
    // Built-in documentation, public (normal SPA path, so no separate
    // basic auth exception in Caddy is needed). One section per
    // subpage; /docs shows the first.
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
    // SSO callback – no layout needed
    path: '/auth/sso-callback',
    component: SSOCallback
  },
  {
    path: '/admin',
    component: AdminLayout,
    meta: { requiresAuth: true },
    children: [
      // Start page of the admin area. AdminLayout leads (meta.home) to the
      // first tab the user may read, or shows "no permission".
      { path: '', component: AdminHome, meta: { home: true } },
      // meta.permission names the permission the page needs for reading
      // (`<resource>:read` of the tab, see utils/adminNav.ts). If it is missing,
      // AdminLayout shows the "no permission" page instead of the view (no
      // API calls, no redirect). Global resources (overview, users, ...)
      // are only readable for global admins; useAuth.hasPerm handles that.
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
      // Language management: a page admin without global admin rights
      // must be able to enable the languages of their own status page. The
      // global-admin-only part (create/edit the language catalogue) hides itself
      // inside the component via isGlobalAdmin.
      { path: 'languages', component: AdminLanguages, meta: { permission: 'languages:read' } },
      // props: true, otherwise :incidentId etc. does not arrive as a prop
      // in the view and it loads `…/undefined`.
      { path: 'incidents/:incidentId/translations', component: AdminIncidentTranslations, props: true, meta: { permission: 'incidents:read' } },
      { path: 'components/:componentId/translations', component: AdminComponentTranslations, props: true, meta: { permission: 'components:read' } },
      { path: 'notices/:noticeId/translations', component: AdminGeneralNoticeTranslations, props: true, meta: { permission: 'notices:read' } },
      // SSO per status page: `sso:read` on the page.
      { path: 'sso', component: AdminSSO, meta: { permission: 'sso:read' } },
      // UI translation editor: a page admin must be able to override the
      // UI texts of their own page. The global-admin-only scope hides itself
      // inside the component via isGlobalAdmin.
      { path: 'ui-translations', component: AdminUiTranslations, meta: { permission: 'ui_translations:read' } },
      // System settings: first global platform setting
      // (self-service ordering on/off).
      { path: 'system-settings', component: AdminSystemSettings, meta: { permission: 'system_settings:read' } },
      // Minimal profile page - for all signed-in users, not only
      // global admins, since this is where (for now only) one's own UI language
      // is set.
      { path: 'profile', component: ProfileView },
      // The docs inside the admin frame (sidebar link "Documentation"): same page as /docs,
      // every docs link stays below this base. Login only, no extra permission.
      { path: 'docs/:section?', component: DocsPage, meta: { docsBase: '/admin/docs' } }
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
        // X-Auth-Token instead of Authorization (that one carries Caddy's basic auth)
        headers: { 'X-Auth-Token': token.value }
      })
      // Caddy's basic auth 401 says nothing about the app token.
      if ((res.status === 401 && !isBasicAuthChallenge(res)) || res.status === 403) {
        logout()
      } else if (res.ok) {
        const data = await res.json() as AuthUser
        setAuth(token.value, data)
      } else {
        console.warn(`Verifizierungs-Serverfehler: Status ${res.status}`)
      }
    } catch (err) {
      console.error('Verbindung zum Auth-Server fehlgeschlagen:', err)
    }
  }

  // A direct visit of a protected route is an explicit choice: come back to it after
  // sign-in. Someone already signed in who lands on /login?returnTo=... goes straight
  // to the checked target (fallback: landing page).
  if (to.meta.requiresAuth && !isLoggedIn.value) {
    next({ path: '/login', query: { returnTo: to.fullPath } })
  } else if (to.path === '/login' && isLoggedIn.value && to.query.returnTo !== undefined) {
    next(safeReturnTo(to.query.returnTo, router, '/'))
  } else {
    next()
  }
})

export default router
