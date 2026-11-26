<script setup lang="ts">
import { parseSearchQuery, matchesSearchQuery } from '../../composables/useSearchQuery'
import { ref, onMounted, onUnmounted, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useApi } from '../../composables/useApi'
import { useAuth } from '../../composables/useAuth'
import { useTenant } from '../../composables/useTenant'
import SubscribeWizard from '../../components/SubscribeWizard.vue'
import ComponentTreeNode from '../../components/ComponentTreeNode.vue'
import SubscriptionManager from '../../components/SubscriptionManager.vue'
import PageLayoutList from '../../components/PageLayoutList.vue'
import RichText from '../../components/RichText.vue'
import { markdownToPlainText } from '../../utils/richText'
import {
  normalizePageLayout, eventTime, pageOverallStatus, countLeaves, OVERALL_TEXT_KEYS,
} from '../../utils/pageLayout'
import { isResolvedStatus, isUpcomingMaintenance, incidentStatusMeta } from '../../utils/incidentStatus'
import { statusMeta } from '../../utils/status'
import { incidentListUrl, mergeIncidents, PUBLIC_PAGE_SIZE } from '../../utils/incidentPaging'
import { parseAvailability } from '../../utils/availability'
import { toIntlLocale } from '../../utils/datetime'
import { fetchPublicTranslations, fetchPublicUpdateTranslations, localizeUpdates, localizeTree, localizeIncidents, localizeNotices } from '../../utils/publicTranslations'

import { useRoute } from 'vue-router'
import AppIcon from '../../components/AppIcon.vue'
import AvailabilityBars from '../../components/AvailabilityBars.vue'
import ExternalIncidentList from '../../components/ExternalIncidentList.vue'
import { applyEffectiveStatus } from '../../utils/externalStatus'
import EmptyState from '../../components/EmptyState.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import StatusIcon from '../../components/StatusIcon.vue'
import OidcLoginButton from '../../components/OidcLoginButton.vue'
import { useCurrentPageSlug } from '../../composables/useCurrentPageSlug'
import { usePublicPageLanguages } from '../../composables/usePublicPageLanguages'
import { useTheme } from '../../composables/useTheme'
import { useDismissable } from '../../composables/useDismissable'
import { ensureContrast } from '../../utils/contrast'
import { BRAND_ON_PRIMARY, BRAND_SECONDARY_BACKGROUNDS } from '../../utils/brandContrast'
import type { PublicTranslations } from '../../utils/publicTranslations'
import type {
  ApiError, Component, ExternalIncident, GeneralNotice, IncidentDetail, IncidentResponse, IncidentUpdateView,
  IsoDateString, MyPage, PageAvailability, PublicDashboard, StatusPage,
} from '../../types/api'

/** What the history dialog shows: an own incident, a timeline entry of the list layout or an external report. */
interface HistoryIncident {
  ID?: number | string
  Title: string
  Status: string
  Message: string
  StartedAt?: IsoDateString | null
  CreatedAt?: IsoDateString
  /** Set on timeline entries of external reports: the original report. */
  external?: object
}

/** Entry of the history dialog: an incident update, an external update or the initial report. */
interface HistoryEntry {
  id: number | string
  status: string
  message: string
  created_at?: IsoDateString | null
  author_email?: string
  /** External texts are plain text, never Markdown. */
  plain?: boolean
  /** The initial report while updates exist (its status back then is not stored). */
  initial?: boolean
}

/** Updates loaded inline for a current incident. */
interface InlineUpdates {
  state: 'loading' | 'ok' | 'error'
  updates: IncidentUpdateView[]
}

interface NoticeLevelMeta {
  label: string
  class: string
  icon: string
}

const route = useRoute()
const { apiFetch } = useApi()
const { isLoggedIn, currentUser } = useAuth()
const { setActiveTenant, setMyPages } = useTenant()
const { t, locale } = useI18n()
const { theme } = useTheme()

// Stryker disable all: Vue hoists defineProps/defineEmits/defineModel out of setup, instrumented code must not reference local variables there
const props = withDefaults(defineProps<{
  slug: string
  // Preview mode for the admin edit form: unsaved design values (LogoURL,
  // PrimaryColor, SecondaryColor, HeaderStyle, PageLayout) override the saved
  // ones. The page is then read-only (inert), does not poll and leaves the
  // shared page state (current slug, page languages, active tenant) alone.
  preview?: Partial<StatusPage> | null
}>(), {
  preview: null
})
// Stryker restore all

const savedPageInfo = ref<StatusPage | null>(null)
const isPreview = computed(() => props.preview !== null)
const pageInfo = computed(() => (savedPageInfo.value && isPreview.value
  ? { ...savedPageInfo.value, ...props.preview }
  : savedPageInfo.value))

// Page-specific branding (logo, colours, header design).
// Sets --primary/--primary-hover/--text-secondary as an inline style override
// on the root element of this view, so CSS that still uses var(--primary)
// automatically gets the page colour - without changing global defaults for
// other pages (e.g. the admin UI). Pages without their own colour
// (empty string, e.g. because not migrated yet or the admin set nothing)
// fall back to the existing global CSS variables, simply because
// no inline variable is set.
// The brand colours are darkened or lightened as far as needed
// to meet WCAG 1.4.3 (4.5:1): --primary carries white
// text (subscribe button), --text-secondary is text on panel and
// hover surfaces of the current theme. Suitable colours stay unchanged.
// The backgrounds (--surface, --surface-2 of the "public" layer) and
// the text colour on --primary live in utils/brandContrast.js, because
// ensureContrast needs hex values and cannot resolve var().
const brandingStyle = computed(() => {
  const style: Record<string, string> = {}
  if (pageInfo.value?.PrimaryColor) {
    const primary = ensureContrast(pageInfo.value.PrimaryColor, BRAND_ON_PRIMARY)
    style['--primary'] = primary
    style['--primary-hover'] = primary
  }
  if (pageInfo.value?.SecondaryColor) {
    style['--text-secondary'] = ensureContrast(pageInfo.value.SecondaryColor, BRAND_SECONDARY_BACKGROUNDS[theme.value === 'dark' ? 'dark' : 'light'])
  }
  return style
})

const headerStyleClass = computed(() => {
  const style = pageInfo.value?.HeaderStyle
  return style && style !== 'classic' ? `status-header--${style}` : ''
})
// Page layout; missing/unknown value -> "standard".
const isListLayout = computed(() => normalizePageLayout(pageInfo.value?.PageLayout) === 'liste')
// rawComponents/rawIncidents/rawNotices are the originals from the
// dashboard; components/incidents/notices show them in the visitor's language
// (confirmed translations, falling back to the page's default language, then the original).
// Everything else (search, filters, cards, history dialog) works on these
// displayed texts.
const rawComponents = ref<Component[]>([])
const translations = ref<PublicTranslations | null>(null)
const components = computed(() => localizeTree(rawComponents.value, translations.value?.components))
// Mirrored reports of external sources (text only, never HTML, never translated).
const externalIncidents = ref<ExternalIncident[]>([])
// The dashboard delivers all open and the newest resolved incidents;
// older ones are added from the list via "Load older incidents" and are kept
// across the regular refresh.
const dashboardIncidents = ref<IncidentResponse[]>([])
const olderIncidents = ref<IncidentResponse[]>([])
const rawIncidents = computed(() => mergeIncidents(dashboardIncidents.value, olderIncidents.value))
const incidentsMore = ref(false)
const loadingMoreIncidents = ref(false)
const moreIncidentsError = ref(false)
const incidents = computed(() => localizeIncidents(rawIncidents.value, translations.value))
const rawNotices = ref<GeneralNotice[]>([])
const notices = computed(() => localizeNotices(rawNotices.value, translations.value?.notices))
const errorMessage = ref('')
const isPrivateError = ref(false)
const noticesExpanded = ref(false)

// Open incidents and running maintenance, without filters. Controls the
// "all systems operational" card; utils/incidentStatus.js defines which
// statuses count as resolved.
const activeIncidents = computed(() => incidents.value.filter(i => !isResolvedStatus(i.Status) && !isUpcomingMaintenance(i)))

// General notices - already filtered by the backend to active, not
// expired notices (IsActive + StartAt/EndAt window), here they are only
// split by Collapsed: non-collapsed notices appear directly,
// collapsed notices are grouped behind a shared expandable header
// to save space on the status page.
const visibleNotices = computed(() => notices.value.filter(n => !n.Collapsed))
const collapsedNotices = computed(() => notices.value.filter(n => n.Collapsed))

const noticeLevelMeta = (level: string) => (({
  info:     { label: t('notices.levelInfo'),     class: 'notice-info',     icon: 'alert-circle' },
  warning:  { label: t('notices.levelWarning'),  class: 'notice-warning',  icon: 'alert-triangle' },
  critical: { label: t('notices.levelCritical'), class: 'notice-critical', icon: 'alert-triangle' },
} as Record<string, NoticeLevelMeta>)[level] ?? { label: level, class: 'notice-info', icon: 'alert-circle' })

// Scheduled maintenance windows (not started yet) get their
// own section with start/end time, instead of appearing in the generic incident list
// as if they were already active.
const upcomingMaintenance = computed(() => incidents.value.filter(isUpcomingMaintenance))
const formatMaintenanceWindow = (i: IncidentResponse) => {
  const intlLocale = toIntlLocale(locale.value)
  const opts: Intl.DateTimeFormatOptions = { dateStyle: 'medium', timeStyle: 'short' }
  const start = i.ScheduledStartAt ? new Date(i.ScheduledStartAt).toLocaleString(intlLocale, opts) : '?'
  const end = i.ScheduledEndAt ? new Date(i.ScheduledEndAt).toLocaleString(intlLocale, { timeStyle: 'short' }) : '?'
  return `${start} – ${end}`
}

// Wizard state
const showWizard = ref(false)

// Opens the subscription editor directly from the subscribe CTA, instead of
// signed-in users having to expand the separate section further down manually
// and click "Load my subscription" first.
const subscriptionManagerRef = ref<InstanceType<typeof SubscriptionManager> | null>(null)
// null = unknown, true/false = subscription exists or not (reported by the SubscriptionManager)
const hasSubscription = ref<boolean | null>(null)
// Scrolling happens inside the manager: its section may only exist once it is open.
const openMySubscription = () => subscriptionManagerRef.value?.revealEditAbo()

// The header menu (PublicLayout.vue) fires this event because -
// reached via the subdomain root or /status/:slug - it has no ref of its own to the
// component rendered here through <router-view/>.
const handleOpenSubscriptionManagerEvent = () => openMySubscription()

// Filter state (restored)
const searchText = ref('')
const debouncedSearchText = ref('')
let searchTimeout: ReturnType<typeof setTimeout> | null = null
// Multiple selection. Empty list = "all"; OR within a group,
// AND between region, status and search text.
const activeCountryTags = ref<string[]>([])
const activeStatusTags = ref<string[]>([])
const searchHelpOpen = ref(false)
// The help closes on a click/tap outside the search field area; the field
// itself counts as inside so the examples stay visible while typing. Escape
// is handled on the input and the button (focus stays where it is).
const searchWrapperRef = ref<HTMLElement | null>(null)
useDismissable(searchHelpOpen, [searchWrapperRef], () => { searchHelpOpen.value = false }, { escape: false })

const toggleIn = (list: string[], key: string) => (list.includes(key) ? list.filter(k => k !== key) : [...list, key])
const toggleCountry = (key: string) => { activeCountryTags.value = toggleIn(activeCountryTags.value, key) }
const toggleStatus = (key: string) => { activeStatusTags.value = toggleIn(activeStatusTags.value, key) }
const resetFilters = () => {
  searchText.value = ''
  debouncedSearchText.value = ''
  activeCountryTags.value = []
  activeStatusTags.value = []
}

// With more than 8 region entries (incl. "all") show only the first
// chips; the active filter stays visible even when collapsed.
const REGION_CHIP_THRESHOLD = 8
const REGION_COLLAPSED_COUNT = 6
const regionsExpanded = ref(false)

const initialLoading = ref(true)
let pollInterval: ReturnType<typeof setInterval> | null = null

// 4a: 90-day availability (GET …/availability). Loads separately from the
// dashboard and does not block the page: errors or an empty answer only leave out
// the bars (availability stays null), the rest of the page stands.
// Per component (?include=components, a much larger answer) only in the
// list layout, which shows the row bars.
const availability = ref<PageAvailability | null>(null)
const lastUpdated = ref<Date | null>(null)
const loadAvailability = async () => {
  const params: string[] = []
  if (route.query.token) params.push(`token=${encodeURIComponent(route.query.token as string)}`)
  if (isListLayout.value) params.push('include=components')
  try {
    const res = await apiFetch(`/api/public/pages/${props.slug}/availability${params.length ? `?${params.join('&')}` : ''}`)
    availability.value = parseAvailability(res) as PageAvailability | null
  } catch {
    availability.value = null
  }
}

watch(searchText, (newVal) => {
  if (searchTimeout) clearTimeout(searchTimeout)
  searchTimeout = setTimeout(() => {
    debouncedSearchText.value = newVal
  }, 300)
})

// Translations for the visitor's UI language. The fetch
// runs in parallel with the dashboard; while the loading state is shown, no
// original text appears that would switch afterwards. Errors yield null (original texts).
// translationsRequest discards stale answers on a quick language switch.
let translationsRequest = 0
const loadTranslations = () => fetchPublicTranslations(apiFetch, props.slug, locale.value, route.query.token)
watch(locale, async () => {
  // Fetch the update texts of already loaded incidents in the new language.
  const loaded = new Set(Object.keys(inlineUpdates.value))
  if (historyIncident.value?.ID) loaded.add(String(historyIncident.value.ID))
  for (const id of loaded) loadUpdateTexts(Number(id))
  const request = ++translationsRequest
  const next = await loadTranslations()
  // On a switch the old state stays until the new one is there (no flicker).
  if (request === translationsRequest) translations.value = next
})

const loadData = async (isInitial = false) => {
  if (isInitial) {
    initialLoading.value = true
  }
  try {
    // A subscriber token in the link (from a notification mail
    // for a private status page) is passed on to the dashboard request,
    // so a subscriber without an account of their own can still open the page.
    const tokenParam = route.query.token ? `?token=${encodeURIComponent(route.query.token as string)}` : ''
    const request = ++translationsRequest
    const [res, translated] = await Promise.all([
      apiFetch<PublicDashboard>(`/api/public/pages/${props.slug}/dashboard${tokenParam}`),
      loadTranslations(),
    ])
    savedPageInfo.value = res.page
    if (!isPreview.value) setPageLanguages(res.languages)
    if (request === translationsRequest) translations.value = translated
    // The effective status everywhere (own + external).
    rawComponents.value = applyEffectiveStatus(res.components)
    externalIncidents.value = res.external_incidents || []
    dashboardIncidents.value = res.incidents
    // After loading more, its result applies, otherwise the dashboard's value.
    if (olderIncidents.value.length === 0) incidentsMore.value = !!res.incidents_more
    rawNotices.value = res.notices || []
    lastUpdated.value = new Date()
  } catch (err) {
    // Detect by the status instead of the (now translated) message text;
    // an anonymous 401 is no "session expired" any more since then.
    // A private page without permission answers like a missing page with
    // 404; the visitor sees both as "not available" and can log in.
    const status = (err as ApiError).status
    if (status === 401 || status === 403 || status === 404) {
      errorMessage.value = t('statusPage.privateErrorMessage')
      isPrivateError.value = true
    } else {
      errorMessage.value = t('statusPage.genericLoadError')
    }
  } finally {
    initialLoading.value = false
  }
  if (pageInfo.value) await loadAvailability()
}

// Loads the next page of resolved incidents (incidents and maintenance) after the
// ones already shown. Less than a full page means: nothing more left.
const loadMoreIncidents = async () => {
  loadingMoreIncidents.value = true
  moreIncidentsError.value = false
  try {
    const offset = rawIncidents.value.filter(i => isResolvedStatus(i.Status)).length
    const batch = await apiFetch<IncidentResponse[]>(incidentListUrl(pageInfo.value?.ID as number, {
      maintenance: 'any', state: 'resolved', limit: PUBLIC_PAGE_SIZE, offset,
    }))
    olderIncidents.value = mergeIncidents(olderIncidents.value, batch)
    incidentsMore.value = batch.length >= PUBLIC_PAGE_SIZE
  } catch {
    moreIncidentsError.value = true
  } finally {
    loadingMoreIncidents.value = false
  }
}

// The page-specific permission in the header menu must not follow the tenant
// last chosen in the admin area, but the status page currently
// shown. That is why activeTenantId is always synchronized here to
// the currently loaded page as soon as a user is logged in.
// The Dashboard link in PublicLayout is decided by the role on THIS page
// (matched via currentPageSlug), never by a role on another page.
watch(() => [pageInfo.value?.ID, isLoggedIn.value] as const, async ([pageId, loggedIn]) => {
  if (!pageId || !loggedIn || isPreview.value) return
  try {
    const pages = await apiFetch<MyPage[]>('/api/me/pages')
    setMyPages(pages)
    setActiveTenant(pageId)
  } catch (err) {
    console.error('Konnte Seiten-Berechtigungen nicht laden', err)
  }
})

// Preview: switching the layout to "liste" needs the per-component bars.
watch(isListLayout, () => {
  if (isPreview.value && savedPageInfo.value) loadAvailability()
})

onMounted(async () => {
  if (isPreview.value) {
    await loadData(true)
    return
  }
  window.addEventListener('statushafen:open-subscription-manager', handleOpenSubscriptionManagerEvent)
  await loadData(true)
  pollInterval = setInterval(() => {
    loadData(false)
  }, 60000)
})

// Provide the slug for the login button in the header (PublicLayout.vue).
const { currentPageSlug, subscriptionsAvailable } = useCurrentPageSlug()
// Languages of the page for the language switcher in the header.
const { setPageLanguages, clearPageLanguages } = usePublicPageLanguages()
watch(() => props.slug, (slug) => { if (!isPreview.value) currentPageSlug.value = slug }, { immediate: true })
// Same condition as the SubscriptionManager in the template (content shown, no preview).
const showsSubscriptions = computed(() => !isPreview.value && !isPrivateError.value && !initialLoading.value)
watch(showsSubscriptions, (shown) => { subscriptionsAvailable.value = shown }, { immediate: true })
onUnmounted(() => {
  if (isPreview.value) return
  currentPageSlug.value = null
  subscriptionsAvailable.value = false
  clearPageLanguages()
})

onUnmounted(() => {
  window.removeEventListener('statushafen:open-subscription-manager', handleOpenSubscriptionManagerEvent)
  if (pollInterval) clearInterval(pollInterval)
  if (searchTimeout) clearTimeout(searchTimeout)
})


// Determine all existing tags from the data dynamically (replacement
// for the former single Country field - a component can now carry several
// tags instead of a single value).
const availableCountries = computed(() => {
  const extractTagNames = (nodes: Component[] | null | undefined): string[] => {
    if (!nodes) return []
    let result: string[] = []
    for (const n of nodes) {
      result = result.concat((n.Tags || []).map(t => t.Name))
      if (n.Children) result = result.concat(extractTagNames(n.Children))
    }
    return result
  }
  const allTagNames = extractTagNames(components.value)
  return [...new Set(allTagNames)].sort()
})

const regionsCollapsible = computed(() => availableCountries.value.length + 1 > REGION_CHIP_THRESHOLD)
const visibleCountries = computed(() => {
  if (!regionsCollapsible.value || regionsExpanded.value) return availableCountries.value
  return availableCountries.value.filter((c, i) => i < REGION_COLLAPSED_COUNT || activeCountryTags.value.includes(c))
})
const hiddenCountryCount = computed(() => availableCountries.value.length - visibleCountries.value.length)

const statusTags = computed(() => [
  { key: 'all', label: t('statusPage.filterAll'), dot: null },
  { key: 'operational', label: t('status.operational'), dot: 'operational' },
  { key: 'degraded_performance', label: t('status.degradedPerformance'), dot: 'degraded' },
  { key: 'partial_outage', label: t('status.partialOutage'), dot: 'partial' },
  { key: 'major_outage', label: t('status.majorOutage'), dot: 'major' },
])

// Recursive search: filters the tree and keeps groups that have matching children
const filterTree = (nodes: Component[] | null | undefined, query: ReturnType<typeof parseSearchQuery>, countryQ: string[], statusQ: string[]): Component[] => {
  if (!nodes) return []
  const result: Component[] = []
  for (const node of nodes) {
    const matchText = matchesSearchQuery(query, [node.Name, node.Description])
    const matchCountry = !countryQ.length || node.Tags?.some(t => countryQ.includes(t.Name))
    const matchStatus = !statusQ.length || statusQ.includes(node.Status)

    // Filter children recursively
    const filteredChildren = node.Children && node.Children.length > 0
      ? filterTree(node.Children, query, countryQ, statusQ)
      : []

    // Include the node if it matches itself OR children match
    if ((matchText && matchCountry && matchStatus) || filteredChildren.length > 0) {
      result.push({ ...node, Children: filteredChildren })
    }
  }
  return result
}

const searchQuery = computed(() => parseSearchQuery(debouncedSearchText.value))

const filteredComponents = computed(() =>
  filterTree(components.value, searchQuery.value, activeCountryTags.value, activeStatusTags.value)
)

// The status chips stand for component statuses (operational …),
// not for incident statuses (investigating …). An incident matches if one
// of its affected components has the chosen status. What counts is the
// status from the component tree, i.e. the same value (overlaid by the backend with open
// incidents) the component list is
// filtered by; the copy in i.Components carries the manually set status.
const componentStatusById = computed(() => {
  const map = new Map<number, string>()
  const walk = (nodes: Component[] | null | undefined) => {
    for (const n of nodes || []) {
      map.set(n.ID, n.Status)
      walk(n.Children)
    }
  }
  walk(components.value)
  return map
})
// Country tags per component, so external reports (without tags of their
// own) can follow the country filter via their affected components.
const componentTagsById = computed(() => {
  const map = new Map<number, string[]>()
  const walk = (nodes: Component[] | null | undefined) => {
    for (const n of nodes || []) {
      map.set(n.ID, (n.Tags || []).map(tag => tag.Name))
      walk(n.Children)
    }
  }
  walk(components.value)
  return map
})
const incidentMatchesStatus = (i: IncidentResponse, statusQ: string[]) =>
  (i.Components || []).some(c => statusQ.includes(componentStatusById.value.get(c.ID) as string))

const filteredIncidents = computed(() => {
  return incidents.value.filter(i => {
    if (isUpcomingMaintenance(i)) return false
    const matchText = matchesSearchQuery(searchQuery.value, [i.Title, markdownToPlainText(i.Message)])
    const matchCountry = !activeCountryTags.value.length || i.Tags?.some(t => activeCountryTags.value.includes(t.Name))
    const matchStatus = !activeStatusTags.value.length || incidentMatchesStatus(i, activeStatusTags.value)
    return matchText && matchCountry && matchStatus
  })
})

// Search and filters also cover the external reports. Text:
// title and names of the affected components; country and status follow the
// affected components (their tags or effective status, as with own
// incidents). A report matches if at least one affected component matches.
const filteredExternalIncidents = computed(() => externalIncidents.value.filter(e => {
  const affected = e.affected_components || []
  const matchText = matchesSearchQuery(searchQuery.value, [e.title, ...affected.map(c => c.name)])
  const matchCountry = !activeCountryTags.value.length
    || affected.some(c => (componentTagsById.value.get(c.id) || []).some(n => activeCountryTags.value.includes(n)))
  const matchStatus = !activeStatusTags.value.length
    || affected.some(c => activeStatusTags.value.includes(componentStatusById.value.get(c.id) as string))
  return matchText && matchCountry && matchStatus
}))

const externalUpdatesNewestFirst = (e: ExternalIncident) =>
  [...(e.updates || [])].sort((a, b) => new Date(b.display_at).getTime() - new Date(a.display_at).getTime())

// In the list layout external reports appear in the timeline,
// as incident-like entries (external carries the original for the dialog).
const externalTimelineItems = computed(() => filteredExternalIncidents.value.map(e => ({
  ID: `ext-${e.id}`,
  Title: e.title,
  Status: e.status,
  Message: externalUpdatesNewestFirst(e)[0]?.body || '',
  StartedAt: e.kind === 'maintenance' && e.scheduled_for ? e.scheduled_for : (e.started_at || e.updated_at),
  IsMaintenance: false,
  external: e,
})))
// PageLayoutList types its entries as full incidents; the external entries only carry what the timeline reads.
type TimelineIncidents = InstanceType<typeof PageLayoutList>['$props']['timelineIncidents']
const timelineIncidents = computed(() => [...filteredIncidents.value, ...externalTimelineItems.value] as TimelineIncidents)

// Resolved incidents do not belong under "Current incidents",
// but into the history below (newest first). Both lists follow
// the same filters; the list layout still shows everything in its timeline.
const currentIncidents = computed(() => filteredIncidents.value.filter(i => !isResolvedStatus(i.Status)))
const pastIncidents = computed(() => filteredIncidents.value
  .filter(i => isResolvedStatus(i.Status))
  .sort((a, b) => new Date(eventTime(b) as string).getTime() - new Date(eventTime(a) as string).getTime()))


// 4a: incident status as StatusBadge properties (level for shape
// and colour, text from i18n). Unknown raw values stay neutral and show the
// raw value; the mapping lives in utils/incidentStatus.js.
const incidentBadge = (status: string) => {
  const meta = incidentStatusMeta(status)
  return { level: meta.level, label: meta.labelKey ? t(meta.labelKey) : status }
}

// 4a: overall status at the top of the page, independent of the filters
// (as already in the list layout, review finding 2). A running maintenance becomes
// "maintenance in progress" as long as there is no disruption.
const overall = computed(() => pageOverallStatus(components.value, incidents.value))
const overallLevel = computed(() => statusMeta(overall.value).level)
const overallText = computed(() => t(OVERALL_TEXT_KEYS[overall.value] as string))
const leafCounts = computed(() => countLeaves(components.value))
const brandInitial = computed(() => (pageInfo.value?.Name || '?').trim().charAt(0).toUpperCase())
const heroSummary = computed(() => t('statusPage.heroSummary', { ok: leafCounts.value.operational, total: leafCounts.value.total }))

// The update history per incident is only loaded on click
// (GET /api/pages/:page_id/incidents/:id), so the dashboard payload
// (GET /api/public/pages/:slug/dashboard) stays lean. The detail
// endpoint is already publicly readable (main.go), so this change
// creates no new data exposure but shows in the UI what the
// API already delivers today.
// 4a: for the current incidents the page loads the updates
// right away, so the timeline sits directly below the incident. At most
// INLINE_INCIDENTS incidents and INLINE_UPDATES updates per incident (newest
// first); everything else is in the history dialog.
const INLINE_INCIDENTS = 3
const INLINE_UPDATES = 3
const inlineUpdates = ref<Record<number, InlineUpdates>>({}) // incident ID -> { state: 'loading' | 'ok' | 'error', updates: [] }
const inlineLoadedKey = new Map<number, string>() // incident ID -> "Status|UpdatedAt" of the last loaded state

// Update texts in the visitor's language (incident ID -> { update ID: text }).
// They are loaded with the updates and fetched again for the loaded incidents
// on a language switch; if a text is missing, the message stays in the original.
const updateTexts = ref<Record<number, Record<string, unknown>>>({})
const updateTextsRequest = new Map<number, number>() // incident ID -> counter; discards stale answers on a language switch
const loadUpdateTexts = async (incidentId: number) => {
  const request = (updateTextsRequest.get(incidentId) ?? 0) + 1
  updateTextsRequest.set(incidentId, request)
  const map = await fetchPublicUpdateTranslations(apiFetch, props.slug, incidentId, locale.value, route.query.token)
  if (updateTextsRequest.get(incidentId) === request) updateTexts.value = { ...updateTexts.value, [incidentId]: map }
}
const withUpdateTexts = <T extends { id: number | string; message?: string }>(incidentId: number | string | undefined, list: T[]) => localizeUpdates(list, updateTexts.value[incidentId as number])

const setInline = (id: number, value: InlineUpdates) => { inlineUpdates.value = { ...inlineUpdates.value, [id]: value } }
const loadInlineUpdates = async (incident: IncidentResponse) => {
  setInline(incident.ID, { state: 'loading', updates: [] })
  try {
    const [detail] = await Promise.all([
      apiFetch<IncidentDetail>(`/api/pages/${pageInfo.value?.ID}/incidents/${incident.ID}`),
      loadUpdateTexts(incident.ID),
    ])
    // Newest first, like the timeline in AdminIncidentDetail.vue.
    setInline(incident.ID, { state: 'ok', updates: [...(detail.updates || [])].reverse() })
  } catch {
    setInline(incident.ID, { state: 'error', updates: [] })
  }
}
const inlineKey = (i: IncidentResponse) => `${i.Status}|${i.UpdatedAt}`
const inlineIncidents = computed(() => currentIncidents.value.slice(0, INLINE_INCIDENTS))
watch(inlineIncidents, (list) => {
  for (const i of list) {
    if (inlineLoadedKey.get(i.ID) === inlineKey(i)) continue
    inlineLoadedKey.set(i.ID, inlineKey(i))
    loadInlineUpdates(i)
  }
})
const inlineState = (id: number) => inlineUpdates.value[id]?.state
const inlineVisibleUpdates = (id: number) => withUpdateTexts(id, (inlineUpdates.value[id]?.updates ?? []).slice(0, INLINE_UPDATES))

const fmtDateTime = (d: IsoDateString | Date | null | undefined) => d ? new Date(d).toLocaleString(toIntlLocale(locale.value), { dateStyle: 'medium', timeStyle: 'short' }) : '—'
const fmtShortDate = (d: IsoDateString | Date | null | undefined) => d ? new Date(d).toLocaleString(toIntlLocale(locale.value)) : '—'
const incidentTimestamp = (i: IncidentResponse) => (i.IsMaintenance ? formatMaintenanceWindow(i) : fmtShortDate(i.StartedAt || i.CreatedAt))

const historyIncident = ref<HistoryIncident | null>(null)
const historyUpdates = ref<HistoryEntry[]>([])
const historyLoading = ref(false)
const historyError = ref('')
// The initial report (Incident.Message) is kept when there are updates and
// is the oldest history entry (the list runs newest first, so at the
// end). Without updates it is the only entry, with the current status;
// with updates the status back then is not stored, so instead of
// the status badge it gets the label "initial report".
const historyEntries = computed((): HistoryEntry[] => {
  const i = historyIncident.value
  const updates = withUpdateTexts(i?.ID, historyUpdates.value)
  if (!i?.Message) return updates
  const hasUpdates = updates.length > 0
  const initial: HistoryEntry = { id: 'initial', initial: hasUpdates, status: hasUpdates ? '' : i.Status, message: i.Message, created_at: i.StartedAt || i.CreatedAt }
  return [...updates, initial]
})

// History of an external report. The updates are already in the
// dashboard answer, nothing is loaded; the dialog shows them like
// own incidents (newest first, text only). Without an initial report (Message empty).
const openExternalHistory = (e: ExternalIncident) => {
  historyIncident.value = { Title: e.title, Status: e.status, Message: '' }
  historyUpdates.value = externalUpdatesNewestFirst(e)
    .map(u => ({ id: u.id, status: u.status, message: u.body, plain: true, created_at: u.display_at }))
  historyError.value = ''
  historyLoading.value = false
}

const openHistory = async (incident: HistoryIncident) => {
  if (incident.external) return openExternalHistory(incident.external as ExternalIncident)
  historyIncident.value = incident
  historyUpdates.value = []
  historyError.value = ''
  historyLoading.value = true
  try {
    const [detail] = await Promise.all([
      apiFetch<IncidentDetail>(`/api/pages/${pageInfo.value?.ID}/incidents/${incident.ID}`),
      loadUpdateTexts(incident.ID as number),
    ])
    // Newest first, like the timeline in AdminIncidentDetail.vue.
    historyUpdates.value = [...(detail.updates || [])].reverse()
  } catch (err) {
    historyError.value = t('statusPage.historyLoadError') + (err as Error).message
  } finally {
    historyLoading.value = false
  }
}

const closeHistory = () => {
  historyIncident.value = null
}
</script>

<template>
  <!-- 4a: The only inline style of this view is the customer brand
       (--primary/--primary-hover/--text-secondary from brandingStyle); everything
       else runs via classes and tokens. -->
  <div class="content-area" :style="brandingStyle" :inert="isPreview || undefined">
    <div v-if="errorMessage && !isPrivateError" class="alert alert-error sp-alert">
      {{ errorMessage }}
    </div>

    <!-- Private Error State -->
    <div v-if="isPrivateError" class="private-error-state">
      <AppIcon name="lock" :size="32" />
      <h2>{{ t('statusPage.privateErrorTitle') }}</h2>
      <p>{{ errorMessage }}</p>
      <router-link :to="{ path: '/login', query: { returnTo: route.fullPath, page: props.slug } }">
        <button class="btn-subscribe-cta">{{ t('statusPage.privateErrorLoginButton') }}</button>
      </router-link>
      <OidcLoginButton class="private-error-sso" :slug="props.slug" :return-to="route.fullPath" fallback />
    </div>

    <!-- Loading Indicator -->
    <div v-if="initialLoading && !isPrivateError" class="loading-state" role="status" aria-live="polite">
      <div class="loading-skeleton">
        <div class="skeleton-line skeleton-line--title"></div>
        <div class="skeleton-card" v-for="n in 4" :key="n"></div>
      </div>
      <span class="loading-label">
        <span class="spinner" aria-hidden="true"></span>
        {{ t('statusPage.loadingLabel') }}
      </span>
    </div>

    <template v-else-if="!isPrivateError && !initialLoading">
      <!-- Header (Direction B): customer brand with logo, name and "System status" -->
      <header class="sp-head" :class="headerStyleClass">
        <div class="sp-brand">
          <img v-if="pageInfo?.LogoURL" :src="pageInfo.LogoURL" alt="" class="status-header-logo" />
          <span v-else class="sp-brand-mark" aria-hidden="true">{{ brandInitial }}</span>
          <span class="sp-brand-name">{{ pageInfo?.Name || t('statusPage.statusFallbackTitle') }}</span>
          <span class="sp-brand-sub">{{ t('statusPage.brandSubtitle') }}</span>
        </div>
      </header>

      <!-- Overall status as a large hero tinted by status at the very top, independent
           of filters (shape + colour + text). The title is the page's h1. -->
      <section
        class="sp-hero"
        :class="`sp-hero--${overallLevel}`"
        role="status"
        aria-labelledby="sp-overall"
      >
        <StatusIcon :status="overall" :size="56" class="sp-hero-icon" />
        <div class="sp-hero-text">
          <h1 id="sp-overall" class="sp-hero-title">{{ overallText }}</h1>
          <p v-if="leafCounts.total > 0" class="sp-hero-sub">{{ heroSummary }} {{ t('statusPage.lastUpdated', { time: fmtDateTime(lastUpdated) }) }}</p>
          <p v-if="pageInfo?.Description" class="status-description">{{ pageInfo.Description }}</p>
        </div>
      </section>

      <!-- Filter area -->
      <div class="filter-bar" role="search" :aria-label="t('statusPage.filterLabel')">
        <!-- Free text search -->
        <div ref="searchWrapperRef" class="search-wrapper">
          <AppIcon name="search" :size="15" class="search-icon" />
          <input
            v-model="searchText"
            type="text"
            class="search-input"
            :placeholder="t('statusPage.searchPlaceholder')"
            :aria-label="t('statusPage.searchAriaLabel')"
            aria-describedby="search-help-text"
            @keydown.esc="searchHelpOpen = false"
          />
          <button v-if="searchText" class="search-clear" @click="searchText = ''" :aria-label="t('statusPage.searchClearAria')">
            <AppIcon name="x" :size="14" />
          </button>
          <button
            type="button"
            class="search-help-btn"
            :aria-label="t('statusPage.searchHelpAria')"
            :aria-expanded="searchHelpOpen ? 'true' : 'false'"
            aria-controls="search-help-text"
            @click="searchHelpOpen = !searchHelpOpen"
            @keydown.esc="searchHelpOpen = false"
          >?</button>
          <div id="search-help-text" class="search-help" role="tooltip" v-show="searchHelpOpen">
            <p>{{ t('statusPage.searchHelpIntro') }}</p>
            <ul>
              <li>{{ t('statusPage.searchHelpExampleAnd') }}</li>
              <li>{{ t('statusPage.searchHelpExampleOr') }}</li>
              <li>{{ t('statusPage.searchHelpExamplePhrase') }}</li>
            </ul>
          </div>
        </div>

        <!-- Country tags -->
        <div class="tag-group">
          <span class="tag-label">{{ t('statusPage.regionLabel') }}</span>
          <div class="tag-chips">
            <button
              class="filter-tag"
              :class="{ active: !activeCountryTags.length }"
              :aria-pressed="!activeCountryTags.length"
              @click="activeCountryTags = []"
            >{{ t('statusPage.filterAll') }}</button>
            <button
              v-for="country in visibleCountries"
              :key="country"
              class="filter-tag"
              :class="{ active: activeCountryTags.includes(country) }"
              :aria-pressed="activeCountryTags.includes(country)"
              @click="toggleCountry(country)"
            >{{ country }}</button>
            <button
              v-if="regionsCollapsible"
              type="button"
              class="tag-more"
              :aria-expanded="regionsExpanded ? 'true' : 'false'"
              @click="regionsExpanded = !regionsExpanded"
            >{{ regionsExpanded ? t('statusPage.showLess') : t('statusPage.showMore', { count: hiddenCountryCount }) }}</button>
          </div>
        </div>

        <!-- Status tags: shape (StatusIcon) + text, not colour alone -->
        <div class="tag-group">
          <span class="tag-label">{{ t('statusPage.statusLabel') }}</span>
          <div class="tag-chips">
            <button
              v-for="tag in statusTags"
              :key="tag.key"
              class="filter-tag"
              :class="{ active: tag.key === 'all' ? !activeStatusTags.length : activeStatusTags.includes(tag.key), [`status-tag-${tag.key}`]: true }"
              :aria-pressed="tag.key === 'all' ? !activeStatusTags.length : activeStatusTags.includes(tag.key)"
              @click="tag.key === 'all' ? (activeStatusTags = []) : toggleStatus(tag.key)"
            >
              <StatusIcon v-if="tag.dot" :status="tag.dot" :size="12" />
              {{ tag.label }}
            </button>
          </div>
        </div>

        <!-- Result counter -->
        <div class="filter-result-count" role="status" v-if="searchText || activeCountryTags.length || activeStatusTags.length">
          {{ t('statusPage.filterResultCount', { components: filteredComponents.length, incidents: filteredIncidents.length + filteredExternalIncidents.length }) }}
          <button class="clear-all-btn" @click="resetFilters">
            {{ t('statusPage.clearFiltersButton') }}
          </button>
        </div>
      </div>

      <!-- Current incidents with timeline, before the component list -->
      <section v-if="activeIncidents.length > 0" class="sp-section" aria-labelledby="sp-current-heading">
        <h2 id="sp-current-heading" class="sp-heading">{{ t('statusPage.currentIncidentsHeading') }}</h2>
        <article class="sp-incident" v-for="i in currentIncidents" :key="i.ID">
          <header class="sp-incident-head">
            <h3 class="sp-incident-title">
              <AppIcon v-if="i.IsMaintenance" name="clock" :size="14" :title="t('statusPage.maintenanceWindowTitle')" />
              {{ i.Title }}
            </h3>
            <StatusBadge :status="incidentBadge(i.Status).level" :label="incidentBadge(i.Status).label" />
          </header>
          <p class="sp-incident-meta">
            {{ t('statusPage.affectsPrefix') }} <strong>{{ (i.Components || []).map(c => c.Name).join(', ') || t('statusPage.unknownComponent') }}</strong>
            <span class="incident-timestamp">{{ incidentTimestamp(i) }}</span>
          </p>
          <RichText class="sp-incident-message" :text="i.Message" />

          <p v-if="inlineState(i.ID) === 'loading'" class="sp-muted" role="status">{{ t('common.loading') }}</p>
          <p v-else-if="inlineState(i.ID) === 'error'" class="sp-muted">{{ t('statusPage.incidentUpdatesLoadError') }}</p>
          <ol v-else-if="inlineVisibleUpdates(i.ID).length > 0" class="sp-timeline" :aria-label="t('statusPage.incidentUpdatesLabel', { title: i.Title })">
            <li v-for="(u, idx) in inlineVisibleUpdates(i.ID)" :key="u.id" class="sp-timeline-item">
              <div class="sp-timeline-meta">
                <span v-if="idx === 0" class="sp-timeline-latest">{{ t('statusPage.latestUpdateLabel') }}</span>
                <StatusBadge variant="plain" :status="incidentBadge(u.status).level" :label="incidentBadge(u.status).label" />
                <time class="sp-timeline-time" :datetime="u.created_at">{{ fmtDateTime(u.created_at) }}</time>
              </div>
              <RichText class="sp-timeline-message" :text="u.message" />
            </li>
          </ol>

          <button class="incident-history-btn" @click="openHistory(i)">
            <AppIcon name="clock" :size="13" /> {{ t('statusPage.viewFullHistoryButton') }}
          </button>
        </article>
        <EmptyState
          v-if="currentIncidents.length === 0"
          icon="search"
          :title="t('statusPage.noIncidentsForFilter')"
        />
      </section>

      <!-- Scheduled maintenance -->
      <section v-if="upcomingMaintenance.length > 0" class="sp-section" aria-labelledby="sp-maintenance-heading">
        <h2 id="sp-maintenance-heading" class="sp-heading"><AppIcon name="clock" :size="18" /> {{ t('statusPage.upcomingMaintenanceHeading') }}</h2>
        <article class="sp-incident" v-for="i in upcomingMaintenance" :key="i.ID">
          <header class="sp-incident-head">
            <h3 class="sp-incident-title">{{ i.Title }}</h3>
            <StatusBadge status="maintenance" :label="t('statusPage.scheduledBadge')" />
          </header>
          <RichText class="sp-incident-message" :text="i.Message" />
          <p class="sp-incident-meta">
            {{ t('statusPage.affectsPrefix') }} <strong>{{ (i.Components || []).map(c => c.Name).join(', ') || t('statusPage.unknownComponent') }}</strong>
            <span class="incident-timestamp">{{ formatMaintenanceWindow(i) }}</span>
          </p>
          <button class="incident-history-btn" @click="openHistory(i)">
            <AppIcon name="clock" :size="13" /> {{ t('statusPage.viewHistoryButton') }}
          </button>
        </article>
      </section>

      <!-- Incidents and maintenance of external sources, clearly marked as "external" -->
      <ExternalIncidentList
        v-if="!isListLayout && filteredExternalIncidents.length > 0"
        :items="filteredExternalIncidents"
        @open-history="(item) => openExternalHistory(item!)"
      />

      <!-- Availability of the last 90 days (whole page). Without data the section is left out. -->
      <section v-if="availability" class="sp-section" aria-labelledby="sp-availability-heading">
        <h2 id="sp-availability-heading" class="sp-heading">{{ t('availability.heading') }}</h2>
        <div class="sp-card">
          <AvailabilityBars
            :name="t('statusPage.overallLabel')"
            :days="availability.days"
            :uptime-percent="availability.uptime_percent ?? null"
          />
        </div>
      </section>

      <!-- Components: list layout (cards with bars, timeline) or tree -->
      <PageLayoutList
        v-if="isListLayout"
        :components="filteredComponents"
        :timeline-incidents="timelineIncidents"
        :availability="availability"
        @open-history="openHistory"
      />
      <section v-else class="sp-section" aria-labelledby="sp-components-heading">
        <h2 id="sp-components-heading" class="sp-heading">{{ t('pageLayout.groupsHeading') }}</h2>
        <div class="component-tree">
          <ComponentTreeNode
            v-for="node in filteredComponents"
            :key="node.ID"
            :node="node"
            :depth="0"
          />
          <EmptyState
            v-if="filteredComponents.length === 0"
            icon="search"
            :title="t('statusPage.noComponentsFound')"
            :text="t('statusPage.noComponentsHint')"
          />
        </div>
      </section>

      <!-- Announcements: newest first, non-collapsed notices
           directly visible, collapsed notices behind an expandable header -->
      <section v-if="notices.length > 0" class="sp-section" aria-labelledby="sp-notices-heading">
        <h2 id="sp-notices-heading" class="sp-heading"><AppIcon name="alert-circle" :size="18" /> {{ t('statusPage.announcementsHeading') }}</h2>
        <div class="notices-list">
          <div
            v-for="n in visibleNotices"
            :key="n.ID"
            class="notice-card"
            :class="noticeLevelMeta(n.Level).class"
          >
            <div class="notice-header">
              <h3><AppIcon :name="noticeLevelMeta(n.Level).icon" :size="14" /> {{ n.Title }}</h3>
              <span class="notice-timestamp">{{ fmtShortDate(n.CreatedAt) }}</span>
            </div>
            <RichText class="notice-message" :text="n.Message" />
          </div>

          <div v-if="collapsedNotices.length > 0" class="notice-collapsed-group">
            <button
              class="notice-collapsed-toggle"
              type="button"
              :aria-expanded="noticesExpanded"
              @click="noticesExpanded = !noticesExpanded"
            >
              <AppIcon :name="noticesExpanded ? 'chevron-down' : 'chevron-right'" :size="14" />
              {{ noticesExpanded ? t('statusPage.hideMoreAnnouncements') : t('statusPage.showMoreAnnouncements', { count: collapsedNotices.length }) }}
            </button>
            <div v-if="noticesExpanded" class="notice-collapsed-content fade-in">
              <div
                v-for="n in collapsedNotices"
                :key="n.ID"
                class="notice-card"
                :class="noticeLevelMeta(n.Level).class"
              >
                <div class="notice-header">
                  <h3><AppIcon :name="noticeLevelMeta(n.Level).icon" :size="14" /> {{ n.Title }}</h3>
                  <span class="notice-timestamp">{{ fmtShortDate(n.CreatedAt) }}</span>
                </div>
                <RichText class="notice-message" :text="n.Message" />
              </div>
            </div>
          </div>
        </div>
      </section>

      <!-- Past incidents -->
      <section v-if="!isListLayout && pastIncidents.length > 0" class="sp-section" aria-labelledby="sp-past-heading">
        <h2 id="sp-past-heading" class="sp-heading"><AppIcon name="clock" :size="18" /> {{ t('statusPage.pastIncidentsHeading') }}</h2>
        <ul class="past-incidents-list">
          <li class="past-incident" v-for="i in pastIncidents" :key="i.ID">
            <div class="past-incident-main">
              <span class="past-incident-title">{{ i.Title }}</span>
              <span class="incident-timestamp">{{ incidentTimestamp(i) }}</span>
            </div>
            <StatusBadge :status="incidentBadge(i.Status).level" :label="incidentBadge(i.Status).label" />
            <button class="incident-history-btn" @click="openHistory(i)">
              <AppIcon name="clock" :size="13" /> {{ t('statusPage.viewHistoryButton') }}
            </button>
          </li>
        </ul>
      </section>

      <!-- Load older resolved incidents (the dashboard delivers only the newest) -->
      <div v-if="incidentsMore" class="sp-more">
        <button type="button" class="incident-history-btn" data-test="load-more-incidents" :disabled="loadingMoreIncidents" @click="loadMoreIncidents">
          <AppIcon name="clock" :size="13" /> {{ t('statusPage.loadMoreIncidents') }}
        </button>
        <p v-if="moreIncidentsError" class="sp-muted" role="alert" data-test="load-more-error">{{ t('statusPage.loadMoreIncidentsError') }}</p>
      </div>

      <!-- ====== Subscribe ====== -->
      <section class="subscribe-cta" aria-labelledby="sp-subscribe-heading">
        <div class="subscribe-cta-text">
          <h2 id="sp-subscribe-heading"><AppIcon name="bell" :size="18" /> {{ t('statusPage.subscribeCtaTitle') }}</h2>
          <p>{{ t('statusPage.subscribeCtaDesc') }}</p>
        </div>
        <div class="subscribe-cta-actions">
          <p v-if="isLoggedIn && hasSubscription === false" class="subscribe-no-sub">{{ t('statusPage.noSubscriptionYet') }}</p>
          <button class="btn-subscribe-cta" @click="showWizard = true">
            {{ t('statusPage.subscribeNowButton') }} <AppIcon name="arrow-right" :size="14" />
          </button>
        </div>
      </section>

      <!-- ====== Manage subscription (the only place; the header menu item opens it via an event) ====== -->
      <SubscriptionManager
        v-if="showsSubscriptions"
        id="subscription-manager-anchor"
        ref="subscriptionManagerRef"
        :isLoggedIn="isLoggedIn"
        :currentUser="currentUser"
        :components="components"
        :slug="props.slug"
        @open-wizard="showWizard = true"
        @subscription-state="hasSubscription = $event"
      />
    </template>

    <!-- Subscribe Wizard Modal -->
    <SubscribeWizard
      v-if="showWizard"
      :components="components"
      :slug="props.slug"
      @close="showWizard = false"
      @subscribed="subscriptionManagerRef?.refresh()"
    />

    <!-- Incident history modal -->
    <Teleport to="body">
      <div v-if="historyIncident" class="history-backdrop" @click.self="closeHistory">
        <div class="history-modal" role="dialog" aria-modal="true" aria-labelledby="history-title">
          <div class="history-header">
            <h3 id="history-title">{{ historyIncident.Title }}</h3>
            <button class="history-close" :aria-label="t('statusPage.historyCloseAria')" @click="closeHistory">
              <AppIcon name="x" :size="16" />
            </button>
          </div>

          <div class="history-body">
            <p v-if="historyLoading" class="sp-muted history-state">{{ t('common.loading') }}</p>
            <div v-else-if="historyError" class="alert alert-error">{{ historyError }}</div>
            <template v-else>
              <EmptyState v-if="historyEntries.length === 0" :title="t('statusPage.historyNoUpdates')" />
              <div v-for="(u, idx) in historyEntries" :key="u.id" class="tl-row">
                <div class="tl-gutter">
                  <span class="tl-dot" :class="`tl-dot--${incidentBadge(u.status).level ?? 'unknown'}`"></span>
                  <span v-if="idx < historyEntries.length - 1" class="tl-line"></span>
                </div>
                <div class="tl-body">
                  <div class="tl-head">
                    <span v-if="u.initial" class="tl-initial">{{ t('statusPage.historyInitialLabel') }}</span>
                    <StatusBadge v-else variant="plain" :status="incidentBadge(u.status).level" :label="incidentBadge(u.status).label" />
                    <span class="tl-time ent-mono">{{ fmtDateTime(u.created_at) }}</span>
                  </div>
                  <RichText class="tl-msg" :text="u.message" :plain="u.plain" />
                  <div v-if="u.author_email" class="tl-author">— {{ u.author_email }}</div>
                </div>
              </div>
            </template>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
/* 4a: all colours, spacing and radii from tokens (tokens.css);
   --primary, --primary-hover and --text-secondary deliberately keep the names
   the customer brand overrides (brandingStyle). */

.sp-alert { margin-bottom: var(--sp-5); }

/* Header (Direction B): customer brand on the left. Header branding:
   the standard layout ("classic") shows the logo left of the name - "centered" and
   "minimal" are only activated via a modifier class when a page has
   set HeaderStyle explicitly. */
.sp-head {
  display: flex;
  align-items: center;
  padding-block: var(--sp-4);
  border-bottom: 1px solid var(--border);
}
.sp-brand { display: flex; align-items: center; gap: var(--sp-3); min-inline-size: 0; font-size: var(--fs-md); font-weight: 600; color: var(--text); }
.sp-brand-mark {
  display: inline-grid;
  place-items: center;
  inline-size: 32px;
  block-size: 32px;
  border-radius: var(--r-md);
  background: var(--primary);
  color: var(--on-primary);
  font-weight: 700;
}
.sp-brand-name { overflow-wrap: anywhere; }
.sp-brand-sub { padding-inline-start: var(--sp-3); border-inline-start: 1px solid var(--border); font-weight: 400; font-size: var(--fs-sm); color: var(--text-secondary); }

.status-header-logo {
  max-height: 40px;
  max-width: 160px;
  object-fit: contain;
  flex-shrink: 0;
}

.status-header--centered { justify-content: center; text-align: center; }
.status-header--minimal .status-header-logo { max-height: 28px; }
.status-header--minimal .sp-brand { font-size: var(--fs-base); }

.status-description { margin: var(--sp-2) 0 0; color: var(--text-secondary); }

/* Overall status as a hero: large shape, tinted area, text. Never colour alone. */
.sp-hero {
  display: flex;
  align-items: center;
  gap: var(--sp-5);
  padding: var(--sp-6) var(--sp-5);
  margin-block: var(--sp-5);
  border: 1px solid var(--st-line, var(--border));
  border-radius: var(--r-lg);
  background: var(--st-soft, var(--surface-2));
}
.sp-hero--operational { --st-soft: var(--ok-soft); --st-line: var(--ok); --st-text: var(--ok-text); }
.sp-hero--degraded { --st-soft: var(--deg-soft); --st-line: var(--deg); --st-text: var(--deg-text); }
.sp-hero--partial { --st-soft: var(--part-soft); --st-line: var(--part); --st-text: var(--part-text); }
.sp-hero--major { --st-soft: var(--major-soft); --st-line: var(--major); --st-text: var(--major-text); }
.sp-hero--maintenance { --st-soft: var(--maint-soft); --st-line: var(--maint); --st-text: var(--maint-text); }
.sp-hero-text { min-inline-size: 0; }
.sp-hero-title { margin: 0; font-size: var(--fs-xl); font-weight: 600; line-height: 1.25; color: var(--st-text, var(--text)); }
.sp-hero-sub { margin: var(--sp-1) 0 0; font-size: var(--fs-base); color: var(--text-2); }
@media (max-width: 600px) {
  .sp-hero { align-items: flex-start; padding: var(--sp-5) var(--sp-4); gap: var(--sp-4); }
}

/* Sections */
.sp-section { margin-top: var(--sp-6); }
.sp-heading {
  display: flex;
  align-items: center;
  gap: var(--sp-2);
  margin: 0 0 var(--sp-3);
  font-size: var(--fs-lg);
  font-weight: 600;
  color: var(--text);
}
.sp-card {
  padding: var(--sp-4);
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  background: var(--surface);
}
.sp-more { margin: var(--sp-4) 0; }
.sp-muted { margin: var(--sp-2) 0 0; font-size: var(--fs-sm); color: var(--text-secondary); }

/* Incident with timeline */
.sp-incident {
  padding: var(--sp-4) var(--sp-5);
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  background: var(--surface);
}
.sp-incident + .sp-incident { margin-top: var(--sp-3); }
.sp-incident-head { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: var(--sp-3); }
.sp-incident-title { display: flex; align-items: center; gap: var(--sp-2); margin: 0; font-size: var(--fs-md); font-weight: 600; color: var(--text); overflow-wrap: anywhere; }
.sp-incident-meta { margin: var(--sp-2) 0 0; font-size: var(--fs-base); color: var(--text-secondary); }
.sp-incident-message { margin: var(--sp-2) 0 0; font-size: var(--fs-base); color: var(--text); overflow-wrap: anywhere; }

.sp-timeline {
  list-style: none;
  margin: var(--sp-4) 0 0;
  padding: 0 0 0 var(--sp-4);
  border-inline-start: 2px solid var(--border);
  display: flex;
  flex-direction: column;
  gap: var(--sp-3);
}
.sp-timeline-meta { display: flex; flex-wrap: wrap; align-items: center; gap: var(--sp-2) var(--sp-3); }
.sp-timeline-latest { font-size: var(--fs-xs); font-weight: 600; color: var(--text-secondary); text-transform: uppercase; letter-spacing: .04em; }
.tl-initial { font-size: var(--fs-xs); font-weight: 600; color: var(--text-secondary); }
.sp-timeline-time { font-size: var(--fs-xs); color: var(--text-3); font-variant-numeric: tabular-nums; }
.sp-timeline-message { margin: var(--sp-1) 0 0; font-size: var(--fs-base); color: var(--text-secondary); overflow-wrap: anywhere; }

/* Notices - reuse .incident-card visual language, own
   level colours instead of incident status colours */
.notices-list { display: flex; flex-direction: column; }

.notice-card {
  border: 1px solid var(--border);
  border-inline-start: 3px solid var(--border-strong);
  background: var(--surface);
  border-radius: var(--r-md);
  padding: var(--sp-3) var(--sp-4);
}
.notice-card + .notice-card { margin-top: var(--sp-3); }
.notice-card.notice-info     { border-inline-start-color: var(--maint); }
.notice-card.notice-warning  { border-inline-start-color: var(--deg); }
.notice-card.notice-critical { border-inline-start-color: var(--major); }

.notice-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: var(--sp-2); gap: var(--sp-3); }
.notice-header h3 { display: flex; align-items: center; gap: var(--sp-2); margin: 0; font-size: var(--fs-md); font-weight: 600; }
.notice-message { margin: 0; font-size: var(--fs-base); color: var(--text-secondary); line-height: 1.5; }
.notice-timestamp { color: var(--text-3); font-size: var(--fs-sm); white-space: nowrap; }

.notice-collapsed-group { margin-top: var(--sp-3); }

.notice-collapsed-toggle {
  display: flex;
  align-items: center;
  gap: var(--sp-2);
  width: 100%;
  min-block-size: 36px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: var(--sp-2) var(--sp-4);
  font-size: var(--fs-base);
  font-weight: 500;
  color: var(--text-secondary);
  cursor: pointer;
  font-family: inherit;
  transition: background 0.15s, color 0.15s;
}
.notice-collapsed-toggle:hover { background: var(--surface-2); color: var(--text); }
.notice-collapsed-toggle:focus-visible { outline: 2px solid var(--focus); outline-offset: 2px; }

.notice-collapsed-content { margin-top: var(--sp-3); }

.component-tree {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  overflow: hidden;
}

/* Private error state */
.private-error-state {
  text-align: center;
  padding: var(--sp-7) var(--sp-5);
  background: var(--surface);
  border-radius: var(--r-lg);
  margin-top: var(--sp-6);
  color: var(--text-3);
}

.private-error-state h2 {
  font-size: var(--fs-2xl);
  margin: var(--sp-4) 0;
  color: var(--text);
}

.private-error-state p {
  color: var(--text-secondary);
  margin-bottom: var(--sp-6);
}

/* Loading state: skeleton placeholder + spinner label */
.loading-state { padding: var(--sp-5) 0 var(--sp-6); }

.loading-skeleton {
  display: flex;
  flex-direction: column;
  gap: var(--sp-3);
  opacity: 0.6;
}

.skeleton-line {
  height: 28px;
  width: 220px;
  border-radius: var(--r-md);
  background: var(--surface);
  border: 1px solid var(--border);
}

.skeleton-line--title { margin-bottom: var(--sp-3); }

.skeleton-card {
  height: 52px;
  border-radius: var(--r-md);
  background: var(--surface);
  border: 1px solid var(--border);
}

.skeleton-line,
.skeleton-card {
  position: relative;
  overflow: hidden;
}

.skeleton-line::after,
.skeleton-card::after {
  content: '';
  position: absolute;
  inset: 0;
  background: linear-gradient(90deg, transparent, var(--surface-2), transparent);
  animation: skeleton-shimmer 1.6s ease-in-out infinite;
}

@keyframes skeleton-shimmer {
  0% { transform: translateX(-100%); }
  100% { transform: translateX(100%); }
}

.loading-label {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--sp-3);
  margin-top: var(--sp-5);
  text-align: center;
  color: var(--text-secondary);
  font-size: var(--fs-md);
}

.spinner {
  width: 16px;
  height: 16px;
  border-radius: 50%;
  border: 2px solid var(--border);
  border-top-color: var(--primary);
  animation: spinner-rotate 0.7s linear infinite;
  flex-shrink: 0;
}

@keyframes spinner-rotate {
  to { transform: rotate(360deg); }
}

@media (prefers-reduced-motion: reduce) {
  .skeleton-line::after,
  .skeleton-card::after {
    animation: none;
  }
  .spinner {
    animation: none;
    border-top-color: var(--border);
  }
}

/* Help icon + tooltip at the search field */
.search-help-btn {
  flex-shrink: 0;
  width: var(--search-help-size);
  height: var(--search-help-size);
  margin-left: var(--sp-2);
  border: 1px solid var(--border-strong);
  border-radius: 50%;
  background: transparent;
  color: inherit;
  font-weight: 600;
  cursor: pointer;
}
.search-help-btn:focus-visible { outline: 2px solid var(--focus); outline-offset: 2px; }
.search-help {
  position: absolute;
  top: calc(100% + 6px);
  right: 0;
  z-index: 20;
  width: min(320px, 90vw);
  padding: var(--sp-3) var(--sp-4);
  border: 1px solid var(--border-strong);
  border-radius: var(--r-lg);
  background: var(--surface);
  box-shadow: var(--shadow-2);
  font-size: var(--fs-sm);
}
.search-help p { margin: 0 0 var(--sp-2); }
.search-help ul { margin: 0; padding-left: 18px; }

/* Search box (icon + clear button) */
/* --search-help-size is shared by the help button and the clear button
   offset, so the clear button stays inside the input. */
.search-wrapper {
  --search-help-size: 24px;
  display: flex;
  align-items: center;
  gap: var(--sp-2);
  position: relative;
}

.search-icon {
  position: absolute;
  left: 10px;
  color: var(--text-3);
  pointer-events: none;
}

/* min-width: 0 overrides the global 180px minimum, so the input shrinks to
   the wrapper and the help button stays inside the viewport at 320px. */
.search-wrapper .search-input {
  flex: 1 1 auto;
  min-width: 0;
  padding-left: 32px;
  padding-right: 30px;
}

/* The wrapper also holds the help button: skip its width, the wrapper
   gap and its margin, then keep the usual inset inside the input. */
.search-clear {
  position: absolute;
  right: calc(var(--search-help-size) + 3 * var(--sp-2));
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: transparent;
  border: none;
  color: var(--text-3);
  cursor: pointer;
  padding: var(--sp-1);
  border-radius: 50%;
  line-height: 0;
}

.search-clear:hover {
  color: var(--text);
  background: var(--surface-2);
}

.incident-timestamp {
  color: var(--text-3);
  font-size: var(--fs-sm);
  margin-left: var(--sp-3);
}

.subscribe-cta {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  padding: var(--sp-5);
  margin-top: var(--sp-6);
  display: flex;
  /* Without wrapping, the hint and button sat to the right outside
     the card at 390 px (.subscribe-cta-actions up to x=782), because the
     actions do not shrink. If there is not enough room, the
     actions now stack below the text. */
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: var(--sp-5);
}

/* The text may shrink to the card width (min-width: 0)
   and takes at least 240 px before the actions wrap. */
.subscribe-cta-text {
  flex: 1 1 240px;
  min-width: 0;
}

.subscribe-cta-text h2 {
  display: flex;
  align-items: center;
  gap: var(--sp-2);
  margin: 0 0 var(--sp-1);
  font-size: var(--fs-lg);
  font-weight: 600;
  color: var(--text);
}

.subscribe-cta-text p {
  margin: 0;
  font-size: var(--fs-base);
  color: var(--text-secondary);
}

.btn-subscribe-cta {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-2);
  background: var(--primary);
  color: var(--on-primary);
  border: none;
  border-radius: var(--r-md);
  min-block-size: var(--ctl-h);
  padding: var(--sp-3) var(--sp-5);
  font-size: var(--fs-base);
  font-weight: 600;
  cursor: pointer;
  white-space: nowrap;
  font-family: inherit;
  transition: background 0.15s;
}

.btn-subscribe-cta:hover { background: var(--primary-hover); }
.btn-subscribe-cta:focus-visible { outline: 2px solid var(--focus); outline-offset: 2px; }

.subscribe-no-sub {
  margin: 0;
  font-size: var(--fs-sm);
  color: var(--text-secondary);
}

.subscribe-cta-actions {
  display: flex;
  /* The "no subscription yet" hint and the buttons wrap
     below each other instead of bursting the card. */
  flex-wrap: wrap;
  align-items: center;
  gap: var(--sp-3);
  flex-shrink: 0;
  max-width: 100%;
}

/* Past incidents: compact rows instead of cards, wraps in
   narrow views instead of scrolling horizontally. */
.past-incidents-list {
  list-style: none;
  margin: 0;
  padding: 0;
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  background: var(--surface);
}

.past-incident {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--sp-2) var(--sp-3);
  padding: var(--sp-3) var(--sp-4);
}

.past-incident + .past-incident { border-top: 1px solid var(--border); }

.past-incident-main {
  display: flex;
  flex-direction: column;
  flex: 1 1 200px;
  min-width: 0;
}

.past-incident-title {
  font-weight: 600;
  overflow-wrap: anywhere;
}

.past-incident .incident-history-btn { margin-top: 0; }
.past-incident-main .incident-timestamp { margin-left: 0; }

/* "Show history" button on the incident card */
.incident-history-btn {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-2);
  margin-top: var(--sp-3);
  min-block-size: 32px;
  padding: var(--sp-1) var(--sp-3);
  background: transparent;
  border: 1px solid var(--border-strong);
  border-radius: var(--r-md);
  color: var(--text-secondary);
  font-size: var(--fs-sm);
  font-weight: 500;
  cursor: pointer;
  font-family: inherit;
  transition: border-color 0.15s, color 0.15s;
}

.incident-history-btn:hover {
  border-color: var(--primary);
  color: var(--primary);
}
.incident-history-btn:focus-visible { outline: 2px solid var(--focus); outline-offset: 2px; }

/* History modal + timeline, adopted from the style of AdminIncidentDetail.vue
   (.tl-row etc.) so the admin and public views of the timeline look
   consistent. Scoped styles are not inherited across components,
   hence duplicated here instead of imported. */
.history-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.55);
  backdrop-filter: blur(6px);
  z-index: 1000;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: var(--sp-5);
}

.history-modal {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  width: 100%;
  max-width: 560px;
  max-height: 80vh;
  display: flex;
  flex-direction: column;
  box-shadow: var(--shadow-2);
}

.history-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--sp-3);
  padding: var(--sp-4) var(--sp-5);
  border-bottom: 1px solid var(--border);
}

.history-header h3 {
  margin: 0;
  font-size: var(--fs-md);
  color: var(--text);
}

.history-close {
  background: none;
  border: none;
  color: var(--text-3);
  cursor: pointer;
  padding: var(--sp-2);
  display: flex;
  align-items: center;
  border-radius: 50%;
}

.history-close:hover {
  color: var(--text);
  background: var(--surface-2);
}
.history-close:focus-visible { outline: 2px solid var(--focus); outline-offset: 2px; }

.history-body {
  padding: var(--sp-5) var(--sp-5) var(--sp-1);
  overflow-y: auto;
}
.history-state { text-align: center; padding: var(--sp-5); }

.tl-row { display: grid; grid-template-columns: 22px 1fr; gap: var(--sp-3); }
.tl-gutter { display: flex; flex-direction: column; align-items: center; }
.tl-dot { width: 13px; height: 13px; border-radius: 50%; border: 3px solid var(--surface); margin-top: 3px; flex-shrink: 0; background: var(--text-3); box-shadow: 0 0 0 1.5px var(--text-3); }
.tl-dot--operational { background: var(--ok); box-shadow: 0 0 0 1.5px var(--ok); }
.tl-dot--degraded { background: var(--deg); box-shadow: 0 0 0 1.5px var(--deg); }
.tl-dot--partial { background: var(--part); box-shadow: 0 0 0 1.5px var(--part); }
.tl-dot--major { background: var(--major); box-shadow: 0 0 0 1.5px var(--major); }
.tl-dot--maintenance { background: var(--maint); box-shadow: 0 0 0 1.5px var(--maint); }
.tl-line { flex: 1; width: 2px; background: var(--border); margin: var(--sp-1) 0; }
.tl-body { padding-bottom: var(--sp-5); }
.tl-head { display: flex; align-items: center; gap: var(--sp-3); flex-wrap: wrap; }
.tl-time { font-size: var(--fs-xs); color: var(--text-3); }
.tl-msg { font-size: var(--fs-base); line-height: 1.6; margin: var(--sp-2) 0 0; color: var(--text-secondary); }
.tl-author { font-size: var(--fs-xs); color: var(--text-3); margin-top: var(--sp-2); }
</style>
