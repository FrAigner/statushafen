<script setup>
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
import { ensureContrast } from '../../utils/contrast'
import { BRAND_ON_PRIMARY, BRAND_SECONDARY_BACKGROUNDS } from '../../utils/brandContrast'

const route = useRoute()
const { apiFetch } = useApi()
const { isLoggedIn, currentUser } = useAuth()
const { setActiveTenant, setMyPages } = useTenant()
const { t, locale } = useI18n()
const { theme } = useTheme()

// Stryker disable all: defineProps/defineEmits/defineModel werden von Vue aus dem Setup gehoben, instrumentierter Code darf dort keine lokalen Variablen referenzieren
const props = defineProps({
  slug: {
    type: String,
    required: true
  }
})
// Stryker restore all

const pageInfo = ref(null)

// seiten-spezifisches Branding (Logo, Farben, Header-Design).
// Setzt --primary/--primary-hover/--text-secondary als Inline-Style-Override
// auf dem Wurzel-Element dieser View, sodass CSS, das weiterhin var(--primary)
// nutzt, automatisch die Seitenfarbe bekommt - ohne globale Defaults für
// andere Seiten (z.B. das Admin-UI) zu verändern. Seiten ohne eigene Farbe
// (leerer String, z.B. weil noch nicht migriert oder Admin hat nichts
// gesetzt) fallen auf die bestehenden globalen CSS-Variablen zurück, indem
// einfach keine Inline-Variable gesetzt wird.
// Die Markenfarben werden bei Bedarf so weit nachgedunkelt bzw.
// aufgehellt, dass sie WCAG 1.4.3 (4,5:1) erfüllen: --primary trägt weiße
// Schrift (Abonnieren-Button), --text-secondary ist Schrift auf Panel- und
// Hover-Flächen des aktuellen Themes. Passende Farben bleiben unverändert.
// Die Hintergründe (--surface, --surface-2 der Ebene „public“) und
// die Schriftfarbe auf --primary stehen in utils/brandContrast.js, weil
// ensureContrast Hex-Werte braucht und var() nicht auflösen kann.
const brandingStyle = computed(() => {
  const style = {}
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
// Seiten-Layout; fehlender/unbekannter Wert -> "standard".
const isListLayout = computed(() => normalizePageLayout(pageInfo.value?.PageLayout) === 'liste')
// rawComponents/rawIncidents/rawNotices sind die Originale aus dem
// Dashboard; components/incidents/notices zeigen sie in der Sprache des Besuchers
// (bestätigte Übersetzungen, Rückfall Standardsprache der Seite, dann Original).
// Alles Weitere (Suche, Filter, Karten, Verlauf-Dialog) arbeitet auf diesen
// angezeigten Texten.
const rawComponents = ref([])
const translations = ref(null)
const components = computed(() => localizeTree(rawComponents.value, translations.value?.components))
// gespiegelte Meldungen externer Quellen (nur Text, nie HTML, nie übersetzt).
const externalIncidents = ref([])
// Das Dashboard liefert alle offenen und die neuesten behobenen Vorfälle;
// ältere kommen per „Ältere Vorfälle laden“ aus der Liste dazu und bleiben bei
// der regelmäßigen Aktualisierung erhalten.
const dashboardIncidents = ref([])
const olderIncidents = ref([])
const rawIncidents = computed(() => mergeIncidents(dashboardIncidents.value, olderIncidents.value))
const incidentsMore = ref(false)
const loadingMoreIncidents = ref(false)
const moreIncidentsError = ref(false)
const incidents = computed(() => localizeIncidents(rawIncidents.value, translations.value))
const rawNotices = ref([])
const notices = computed(() => localizeNotices(rawNotices.value, translations.value?.notices))
const errorMessage = ref('')
const isPrivateError = ref(false)
const noticesExpanded = ref(false)

// offene Vorfälle und laufende Wartungen, ohne Filter. Steuert die
// „Alles im grünen Bereich“-Karte; welche Status als behoben gelten, legt
// utils/incidentStatus.js fest.
const activeIncidents = computed(() => incidents.value.filter(i => !isResolvedStatus(i.Status) && !isUpcomingMaintenance(i)))

// General Notices - vom Backend bereits auf aktive, nicht
// abgelaufene Notices gefiltert (IsActive + StartAt/EndAt-Fenster), hier nur
// noch nach Collapsed aufteilen: nicht eingeklappte Notices erscheinen direkt,
// Collapsed-Notices werden hinter einem gemeinsamen aufklappbaren Header
// gruppiert, um auf der Statusseite Platz zu sparen.
const visibleNotices = computed(() => notices.value.filter(n => !n.Collapsed))
const collapsedNotices = computed(() => notices.value.filter(n => n.Collapsed))

const noticeLevelMeta = (level) => ({
  info:     { label: t('notices.levelInfo'),     class: 'notice-info',     icon: 'alert-circle' },
  warning:  { label: t('notices.levelWarning'),  class: 'notice-warning',  icon: 'alert-triangle' },
  critical: { label: t('notices.levelCritical'), class: 'notice-critical', icon: 'alert-triangle' },
}[level] ?? { label: level, class: 'notice-info', icon: 'alert-circle' })

// geplante Wartungsfenster (noch nicht gestartet) bekommen eine
// eigene Sektion mit Start-/Endzeit, statt in der generischen Incident-Liste
// zu erscheinen, als wären sie bereits aktiv.
const upcomingMaintenance = computed(() => incidents.value.filter(isUpcomingMaintenance))
const formatMaintenanceWindow = (i) => {
  const intlLocale = toIntlLocale(locale.value)
  const opts = { dateStyle: 'medium', timeStyle: 'short' }
  const start = i.ScheduledStartAt ? new Date(i.ScheduledStartAt).toLocaleString(intlLocale, opts) : '?'
  const end = i.ScheduledEndAt ? new Date(i.ScheduledEndAt).toLocaleString(intlLocale, { timeStyle: 'short' }) : '?'
  return `${start} – ${end}`
}

// Wizard State
const showWizard = ref(false)

// Öffnet den Abo-Editor direkt aus dem Abonnieren-CTA heraus, statt dass
// angemeldete Nutzer die separate Sektion weiter unten erst manuell aufklappen
// und "Mein Abo laden" klicken müssen.
const subscriptionManagerRef = ref(null)
// null = unbekannt, true/false = Abonnement vorhanden bzw. nicht (vom SubscriptionManager gemeldet)
const hasSubscription = ref(null)
const openMySubscription = () => {
  document.getElementById('subscription-manager-anchor')?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  subscriptionManagerRef.value?.openEditAbo()
}

// Header-Menü (PublicLayout.vue) löst dieses Event aus, da es –
// via Subdomain-Root oder /status/:slug erreicht – keine eigene Ref auf die
// hier gerenderte Komponente durch <router-view/> hindurch hat.
const handleOpenSubscriptionManagerEvent = () => openMySubscription()

// Filter state (wiederhergestellt)
const searchText = ref('')
const debouncedSearchText = ref('')
let searchTimeout = null
// Mehrfachauswahl. Leere Liste = „Alle“; innerhalb einer Gruppe ODER,
// zwischen Region, Status und Suchtext UND.
const activeCountryTags = ref([])
const activeStatusTags = ref([])
const searchHelpOpen = ref(false)

const toggleIn = (list, key) => (list.includes(key) ? list.filter(k => k !== key) : [...list, key])
const toggleCountry = (key) => { activeCountryTags.value = toggleIn(activeCountryTags.value, key) }
const toggleStatus = (key) => { activeStatusTags.value = toggleIn(activeStatusTags.value, key) }
const resetFilters = () => {
  searchText.value = ''
  debouncedSearchText.value = ''
  activeCountryTags.value = []
  activeStatusTags.value = []
}

// Ab mehr als 8 Region-Einträgen (inkl. „Alle“) nur die ersten
// Chips zeigen; der aktive Filter bleibt auch eingeklappt sichtbar.
const REGION_CHIP_THRESHOLD = 8
const REGION_COLLAPSED_COUNT = 6
const regionsExpanded = ref(false)

const initialLoading = ref(true)
let pollInterval = null

// 4a: 90-Tage-Verfügbarkeit (GET …/availability). Lädt getrennt vom
// Dashboard und blockiert die Seite nicht: Fehler oder eine leere Antwort lassen
// nur die Balken weg (availability bleibt null), der Rest der Seite steht.
// Je Komponente (?include=components, deutlich größere Antwort) nur im
// Listen-Layout, das die Zeilenbalken zeigt.
const availability = ref(null)
const lastUpdated = ref(null)
const loadAvailability = async () => {
  const params = []
  if (route.query.token) params.push(`token=${encodeURIComponent(route.query.token)}`)
  if (isListLayout.value) params.push('include=components')
  try {
    const res = await apiFetch(`/api/public/pages/${props.slug}/availability${params.length ? `?${params.join('&')}` : ''}`)
    availability.value = parseAvailability(res)
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

// Übersetzungen für die Oberflächensprache des Besuchers. Der Abruf
// läuft parallel zum Dashboard; solange der Ladezustand steht, erscheint kein
// Originaltext, der danach umspringt. Fehler ergeben null (Originaltexte).
// translationsRequest verwirft veraltete Antworten bei schnellem Sprachwechsel.
let translationsRequest = 0
const loadTranslations = () => fetchPublicTranslations(apiFetch, props.slug, locale.value, route.query.token)
watch(locale, async () => {
  // Update-Texte der schon geladenen Vorfälle in der neuen Sprache holen.
  const loaded = new Set(Object.keys(inlineUpdates.value))
  if (historyIncident.value?.ID) loaded.add(String(historyIncident.value.ID))
  for (const id of loaded) loadUpdateTexts(Number(id))
  const request = ++translationsRequest
  const next = await loadTranslations()
  // Beim Wechsel bleibt der alte Stand stehen, bis der neue da ist (kein Flackern).
  if (request === translationsRequest) translations.value = next
})

const loadData = async (isInitial = false) => {
  if (isInitial) {
    initialLoading.value = true
  }
  try {
    // ein Subscriber-Token im Link (aus einer Benachrichtigungsmail
    // für eine private Statusseite) wird an den Dashboard-Request durchgereicht,
    // damit ein Abonnent ohne eigenes Konto die Seite trotzdem öffnen kann.
    const tokenParam = route.query.token ? `?token=${encodeURIComponent(route.query.token)}` : ''
    const request = ++translationsRequest
    const [res, translated] = await Promise.all([
      apiFetch(`/api/public/pages/${props.slug}/dashboard${tokenParam}`),
      loadTranslations(),
    ])
    pageInfo.value = res.page
    setPageLanguages(res.languages)
    if (request === translationsRequest) translations.value = translated
    // überall der effektive Status (eigener + externer).
    rawComponents.value = applyEffectiveStatus(res.components)
    externalIncidents.value = res.external_incidents || []
    dashboardIncidents.value = res.incidents
    // Nach einem Nachladen gilt dessen Ergebnis, sonst die Angabe des Dashboards.
    if (olderIncidents.value.length === 0) incidentsMore.value = !!res.incidents_more
    rawNotices.value = res.notices || []
    lastUpdated.value = new Date()
  } catch (err) {
    // am Status statt am (jetzt übersetzten) Meldungstext erkennen;
    // eine anonyme 401 ist seitdem auch kein „Sitzung abgelaufen“ mehr.
    // Eine private Seite ohne Berechtigung antwortet wie eine fehlende Seite mit
    // 404; der Besucher sieht beides als „nicht verfügbar“ und kann sich anmelden.
    if (err.status === 401 || err.status === 403 || err.status === 404) {
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

// Lädt die nächste Seite behobener Vorfälle (Vorfälle und Wartungen) hinter den
// bereits angezeigten. Weniger als eine volle Seite heißt: nichts mehr offen.
const loadMoreIncidents = async () => {
  loadingMoreIncidents.value = true
  moreIncidentsError.value = false
  try {
    const offset = rawIncidents.value.filter(i => isResolvedStatus(i.Status)).length
    const batch = await apiFetch(incidentListUrl(pageInfo.value?.ID, {
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

// die seitenbezogene Berechtigung im Header-Menü darf sich nicht nach dem
// zuletzt im Admin-Bereich gewählten Mandanten richten, sondern nach der
// gerade angezeigten Statusseite. Deshalb wird activeTenantId hier immer auf
// die aktuell geladene Seite synchronisiert, sobald ein User eingeloggt ist.
// Betreiber-Entscheidung: Der Dashboard-Link erscheint trotzdem
// auch ohne Admin-Recht auf DIESER Seite, sobald der Nutzer es auf irgendeiner
// Seite hat (PublicLayout); der Klick schaltet dann auf diese Seite um.
watch(() => [pageInfo.value?.ID, isLoggedIn.value], async ([pageId, loggedIn]) => {
  if (!pageId || !loggedIn) return
  try {
    const pages = await apiFetch('/api/me/pages')
    setMyPages(pages)
    setActiveTenant(pageId)
  } catch (err) {
    console.error('Konnte Seiten-Berechtigungen nicht laden', err)
  }
})

onMounted(async () => {
  window.addEventListener('statushafen:open-subscription-manager', handleOpenSubscriptionManagerEvent)
  await loadData(true)
  pollInterval = setInterval(() => {
    loadData(false)
  }, 60000)
})

// Slug für den Login-Knopf im Kopf (PublicLayout.vue) bereitstellen.
const { currentPageSlug } = useCurrentPageSlug()
// Sprachen der Seite für den Sprachumschalter im Kopf.
const { setPageLanguages, clearPageLanguages } = usePublicPageLanguages()
watch(() => props.slug, (slug) => { currentPageSlug.value = slug }, { immediate: true })
onUnmounted(() => { currentPageSlug.value = null; clearPageLanguages() })

onUnmounted(() => {
  window.removeEventListener('statushafen:open-subscription-manager', handleOpenSubscriptionManagerEvent)
  if (pollInterval) clearInterval(pollInterval)
  if (searchTimeout) clearTimeout(searchTimeout)
})


// Dynamisch alle vorhandenen Tags aus den Daten ermitteln (Ersatz
// für das frühere einzelne Country-Feld - eine Komponente kann jetzt mehrere
// Tags tragen statt eines einzelnen Werts).
const availableCountries = computed(() => {
  const extractTagNames = (nodes) => {
    if (!nodes) return []
    let result = []
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

// Rekursive Suche: filtert den Baum und behält Gruppen die passende Kinder haben
const filterTree = (nodes, query, countryQ, statusQ) => {
  if (!nodes) return []
  const result = []
  for (const node of nodes) {
    const matchText = matchesSearchQuery(query, [node.Name, node.Description])
    const matchCountry = !countryQ.length || node.Tags?.some(t => countryQ.includes(t.Name))
    const matchStatus = !statusQ.length || statusQ.includes(node.Status)

    // Kinder rekursiv filtern
    const filteredChildren = node.Children && node.Children.length > 0
      ? filterTree(node.Children, query, countryQ, statusQ)
      : []

    // Knoten aufnehmen wenn er selbst passt ODER Kinder passen
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

// Die Status-Chips stehen für Komponenten-Status (operational …),
// nicht für Vorfall-Status (investigating …). Ein Vorfall passt, wenn eine
// seiner betroffenen Komponenten den gewählten Status hat. Maßgeblich ist der
// Status aus dem Komponentenbaum, also derselbe (vom Backend mit offenen
// Vorfällen überlagerte) Wert, nach dem auch die Komponentenliste
// gefiltert wird; die Kopie in i.Components trägt den manuell gesetzten Status.
const componentStatusById = computed(() => {
  const map = new Map()
  const walk = (nodes) => {
    for (const n of nodes || []) {
      map.set(n.ID, n.Status)
      walk(n.Children)
    }
  }
  walk(components.value)
  return map
})
// Länder-Tags je Komponente, damit externe Meldungen (ohne eigene
// Tags) dem Länderfilter über ihre betroffenen Komponenten folgen können.
const componentTagsById = computed(() => {
  const map = new Map()
  const walk = (nodes) => {
    for (const n of nodes || []) {
      map.set(n.ID, (n.Tags || []).map(tag => tag.Name))
      walk(n.Children)
    }
  }
  walk(components.value)
  return map
})
const incidentMatchesStatus = (i, statusQ) =>
  (i.Components || []).some(c => statusQ.includes(componentStatusById.value.get(c.ID)))

const filteredIncidents = computed(() => {
  return incidents.value.filter(i => {
    if (isUpcomingMaintenance(i)) return false
    const matchText = matchesSearchQuery(searchQuery.value, [i.Title, markdownToPlainText(i.Message)])
    const matchCountry = !activeCountryTags.value.length || i.Tags?.some(t => activeCountryTags.value.includes(t.Name))
    const matchStatus = !activeStatusTags.value.length || incidentMatchesStatus(i, activeStatusTags.value)
    return matchText && matchCountry && matchStatus
  })
})

// Suche und Filter erfassen auch die externen Meldungen. Text:
// Titel und Namen der betroffenen Komponenten; Land und Status folgen den
// betroffenen Komponenten (deren Tags bzw. effektivem Status, wie bei eigenen
// Vorfällen). Eine Meldung passt, wenn mindestens eine betroffene Komponente passt.
const filteredExternalIncidents = computed(() => externalIncidents.value.filter(e => {
  const affected = e.affected_components || []
  const matchText = matchesSearchQuery(searchQuery.value, [e.title, ...affected.map(c => c.name)])
  const matchCountry = !activeCountryTags.value.length
    || affected.some(c => (componentTagsById.value.get(c.id) || []).some(n => activeCountryTags.value.includes(n)))
  const matchStatus = !activeStatusTags.value.length
    || affected.some(c => activeStatusTags.value.includes(componentStatusById.value.get(c.id)))
  return matchText && matchCountry && matchStatus
}))

const externalUpdatesNewestFirst = (e) =>
  [...(e.updates || [])].sort((a, b) => new Date(b.display_at) - new Date(a.display_at))

// Im Layout liste stehen externe Meldungen in der Zeitleiste, dazu
// als Vorfall-ähnliche Einträge (external trägt das Original für den Dialog).
const externalTimelineItems = computed(() => filteredExternalIncidents.value.map(e => ({
  ID: `ext-${e.id}`,
  Title: e.title,
  Status: e.status,
  Message: externalUpdatesNewestFirst(e)[0]?.body || '',
  StartedAt: e.kind === 'maintenance' && e.scheduled_for ? e.scheduled_for : (e.started_at || e.updated_at),
  IsMaintenance: false,
  external: e,
})))
const timelineIncidents = computed(() => [...filteredIncidents.value, ...externalTimelineItems.value])

// Behobene Vorfälle gehören nicht unter „Aktuelle Vorfälle“,
// sondern in den Verlauf darunter (neueste zuerst). Beide Listen folgen
// denselben Filtern; das Layout liste zeigt weiter alles in seiner Zeitleiste.
const currentIncidents = computed(() => filteredIncidents.value.filter(i => !isResolvedStatus(i.Status)))
const pastIncidents = computed(() => filteredIncidents.value
  .filter(i => isResolvedStatus(i.Status))
  .sort((a, b) => new Date(eventTime(b)) - new Date(eventTime(a))))


// 4a: Vorfallstatus als StatusBadge-Eigenschaften (Stufe für Form
// und Farbe, Text aus i18n). Unbekannte Rohwerte bleiben neutral und zeigen den
// Rohwert; die Zuordnung steht in utils/incidentStatus.js.
const incidentBadge = (status) => {
  const meta = incidentStatusMeta(status)
  return { level: meta.level, label: meta.labelKey ? t(meta.labelKey) : status }
}

// 4a: Gesamtstatus oben auf der Seite, unabhängig von den Filtern
// (wie schon im Listen-Layout, Review-Befund 2). Eine laufende Wartung wird zu
// „Wartungsarbeiten laufen“, solange keine Störung vorliegt.
const overall = computed(() => pageOverallStatus(components.value, incidents.value))
const overallLevel = computed(() => statusMeta(overall.value).level)
const overallText = computed(() => t(OVERALL_TEXT_KEYS[overall.value]))
const leafCounts = computed(() => countLeaves(components.value))
const brandInitial = computed(() => (pageInfo.value?.Name || '?').trim().charAt(0).toUpperCase())
const heroSummary = computed(() => t('statusPage.heroSummary', { ok: leafCounts.value.operational, total: leafCounts.value.total }))

// Update-Verlauf je Incident wird erst bei Klick nachgeladen
// (GET /api/pages/:page_id/incidents/:id), damit das Dashboard-Payload
// (GET /api/public/pages/:slug/dashboard) schlank bleibt. Der Detail-
// Endpunkt ist bereits öffentlich lesbar (main.go), diese Änderung
// erzeugt also keine neue Datenexposition, sondern zeigt im UI, was die
// API schon heute liefert.
// 4a: Für die aktuellen Vorfälle lädt die Seite die Updates
// gleich mit, damit die Zeitleiste direkt unter dem Vorfall steht. Höchstens
// INLINE_INCIDENTS Vorfälle und je Vorfall INLINE_UPDATES Updates (neueste
// zuerst); alles weitere steht im Verlauf-Dialog.
const INLINE_INCIDENTS = 3
const INLINE_UPDATES = 3
const inlineUpdates = ref({}) // Vorfall-ID -> { state: 'loading' | 'ok' | 'error', updates: [] }
const inlineLoadedKey = new Map() // Vorfall-ID -> "Status|UpdatedAt" des zuletzt geladenen Stands

// Texte der Updates in der Sprache des Besuchers (Vorfall-ID -> { Update-ID: Text }).
// Sie werden mit den Updates geladen und beim Sprachwechsel für die geladenen
// Vorfälle neu geholt; fehlt ein Text, bleibt die Nachricht im Original.
const updateTexts = ref({})
const updateTextsRequest = new Map() // Vorfall-ID -> Zähler; verwirft veraltete Antworten beim Sprachwechsel
const loadUpdateTexts = async (incidentId) => {
  const request = (updateTextsRequest.get(incidentId) ?? 0) + 1
  updateTextsRequest.set(incidentId, request)
  const map = await fetchPublicUpdateTranslations(apiFetch, props.slug, incidentId, locale.value, route.query.token)
  if (updateTextsRequest.get(incidentId) === request) updateTexts.value = { ...updateTexts.value, [incidentId]: map }
}
const withUpdateTexts = (incidentId, list) => localizeUpdates(list, updateTexts.value[incidentId])

const setInline = (id, value) => { inlineUpdates.value = { ...inlineUpdates.value, [id]: value } }
const loadInlineUpdates = async (incident) => {
  setInline(incident.ID, { state: 'loading', updates: [] })
  try {
    const [detail] = await Promise.all([
      apiFetch(`/api/pages/${pageInfo.value?.ID}/incidents/${incident.ID}`),
      loadUpdateTexts(incident.ID),
    ])
    // Neueste zuerst, analog zur Timeline in AdminIncidentDetail.vue.
    setInline(incident.ID, { state: 'ok', updates: [...(detail.updates || [])].reverse() })
  } catch {
    setInline(incident.ID, { state: 'error', updates: [] })
  }
}
const inlineKey = (i) => `${i.Status}|${i.UpdatedAt}`
const inlineIncidents = computed(() => currentIncidents.value.slice(0, INLINE_INCIDENTS))
watch(inlineIncidents, (list) => {
  for (const i of list) {
    if (inlineLoadedKey.get(i.ID) === inlineKey(i)) continue
    inlineLoadedKey.set(i.ID, inlineKey(i))
    loadInlineUpdates(i)
  }
})
const inlineState = (id) => inlineUpdates.value[id]?.state
const inlineVisibleUpdates = (id) => withUpdateTexts(id, (inlineUpdates.value[id]?.updates ?? []).slice(0, INLINE_UPDATES))

const fmtDateTime = (d) => d ? new Date(d).toLocaleString(toIntlLocale(locale.value), { dateStyle: 'medium', timeStyle: 'short' }) : '—'
const fmtShortDate = (d) => d ? new Date(d).toLocaleString(toIntlLocale(locale.value)) : '—'
const incidentTimestamp = (i) => (i.IsMaintenance ? formatMaintenanceWindow(i) : fmtShortDate(i.StartedAt || i.CreatedAt))

const historyIncident = ref(null)
const historyUpdates = ref([])
const historyLoading = ref(false)
const historyError = ref('')
// Die Erstmeldung (Incident.Message) bleibt bei Updates erhalten und
// steht als ältester Verlaufseintrag (die Liste läuft neueste zuerst, also am
// Ende). Ohne Updates ist sie der einzige Eintrag mit dem aktuellen Status;
// mit Updates ist der damalige Status nicht gespeichert, dann gibt es statt
// des Status-Badges die Kennzeichnung „Erstmeldung“.
const historyEntries = computed(() => {
  const i = historyIncident.value
  const updates = withUpdateTexts(i?.ID, historyUpdates.value)
  if (!i?.Message) return updates
  const hasUpdates = updates.length > 0
  const initial = { id: 'initial', initial: hasUpdates, status: hasUpdates ? '' : i.Status, message: i.Message, created_at: i.StartedAt || i.CreatedAt }
  return [...updates, initial]
})

// Verlauf einer externen Meldung. Die Updates liegen schon in der
// Dashboard-Antwort, es wird nichts nachgeladen; der Dialog zeigt sie wie bei
// eigenen Vorfällen (neueste zuerst, nur Text). Ohne Erstmeldung (Message leer).
const openExternalHistory = (e) => {
  historyIncident.value = { Title: e.title, Status: e.status, Message: '' }
  historyUpdates.value = externalUpdatesNewestFirst(e)
    .map(u => ({ id: u.id, status: u.status, message: u.body, plain: true, created_at: u.display_at }))
  historyError.value = ''
  historyLoading.value = false
}

const openHistory = async (incident) => {
  if (incident.external) return openExternalHistory(incident.external)
  historyIncident.value = incident
  historyUpdates.value = []
  historyError.value = ''
  historyLoading.value = true
  try {
    const [detail] = await Promise.all([
      apiFetch(`/api/pages/${pageInfo.value?.ID}/incidents/${incident.ID}`),
      loadUpdateTexts(incident.ID),
    ])
    // Neueste zuerst, analog zur Timeline in AdminIncidentDetail.vue.
    historyUpdates.value = [...(detail.updates || [])].reverse()
  } catch (err) {
    historyError.value = t('statusPage.historyLoadError') + err.message
  } finally {
    historyLoading.value = false
  }
}

const closeHistory = () => {
  historyIncident.value = null
}
</script>

<template>
  <!-- 4a: Der einzige Inline-Style dieser View ist die Kundenmarke
       (--primary/--primary-hover/--text-secondary aus brandingStyle); alles
       andere läuft über Klassen und Tokens. -->
  <div class="content-area" :style="brandingStyle">
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
      <OidcLoginButton class="private-error-sso" :slug="props.slug" fallback />
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
      <!-- Kopf (Richtung B): Kundenmarke mit Logo, Name und „Systemstatus“ -->
      <header class="sp-head" :class="headerStyleClass">
        <div class="sp-brand">
          <img v-if="pageInfo?.LogoURL" :src="pageInfo.LogoURL" alt="" class="status-header-logo" />
          <span v-else class="sp-brand-mark" aria-hidden="true">{{ brandInitial }}</span>
          <span class="sp-brand-name">{{ pageInfo?.Name || t('statusPage.statusFallbackTitle') }}</span>
          <span class="sp-brand-sub">{{ t('statusPage.brandSubtitle') }}</span>
        </div>
      </header>

      <!-- Gesamtstatus als großer, nach Status getönter Hero ganz oben, unabhängig
           von Filtern (Form + Farbe + Text). Der Titel ist die h1 der Seite. -->
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

      <!-- Filter-Bereich -->
      <div class="filter-bar" role="search" :aria-label="t('statusPage.filterLabel')">
        <!-- Freitext-Suche -->
        <div class="search-wrapper">
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

        <!-- Länder-Tags -->
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

        <!-- Status-Tags: Form (StatusIcon) + Text, nicht nur Farbe -->
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

        <!-- Ergebnis-Counter -->
        <div class="filter-result-count" role="status" v-if="searchText || activeCountryTags.length || activeStatusTags.length">
          {{ t('statusPage.filterResultCount', { components: filteredComponents.length, incidents: filteredIncidents.length + filteredExternalIncidents.length }) }}
          <button class="clear-all-btn" @click="resetFilters">
            {{ t('statusPage.clearFiltersButton') }}
          </button>
        </div>
      </div>

      <!-- Aktuelle Vorfälle mit Zeitleiste, vor der Komponentenliste -->
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

      <!-- Geplante Wartungsarbeiten -->
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

      <!-- Vorfälle und Wartungen externer Quellen, klar als „extern“ gekennzeichnet -->
      <ExternalIncidentList
        v-if="!isListLayout && filteredExternalIncidents.length > 0"
        :items="filteredExternalIncidents"
        @open-history="openExternalHistory"
      />

      <!-- Verfügbarkeit der letzten 90 Tage (Gesamtseite). Ohne Daten entfällt der Abschnitt. -->
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

      <!-- Komponenten: Listen-Layout (Karten mit Balken, Zeitleiste) oder Baum -->
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

      <!-- Ankündigungen: neueste zuerst, nicht eingeklappte Notices
           direkt sichtbar, Collapsed-Notices hinter aufklappbarem Header -->
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

      <!-- Vergangene Vorfälle -->
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

      <!-- Ältere behobene Vorfälle nachladen (Dashboard liefert nur die neuesten) -->
      <div v-if="incidentsMore" class="sp-more">
        <button type="button" class="incident-history-btn" data-test="load-more-incidents" :disabled="loadingMoreIncidents" @click="loadMoreIncidents">
          <AppIcon name="clock" :size="13" /> {{ t('statusPage.loadMoreIncidents') }}
        </button>
        <p v-if="moreIncidentsError" class="sp-muted" role="alert" data-test="load-more-error">{{ t('statusPage.loadMoreIncidentsError') }}</p>
      </div>

      <!-- ====== Abonnieren ====== -->
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

      <!-- ====== Abonnement verwalten (einzige Stelle; der Kopf-Menüpunkt öffnet sie per Event) ====== -->
      <SubscriptionManager
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

    <!-- Incident-Verlauf Modal -->
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
/* 4a: Alle Farben, Abstände und Radien aus Tokens (tokens.css);
   --primary, --primary-hover und --text-secondary bleiben bewusst die Namen,
   die die Kundenmarke überschreibt (brandingStyle). */

.sp-alert { margin-bottom: var(--sp-5); }

/* Kopf (Richtung B): Kundenmarke links. Header-Branding:
   Standard-Layout ("classic") zeigt Logo links neben dem Namen - "centered" und
   "minimal" werden nur per Modifier-Klasse aktiviert, wenn eine Seite
   HeaderStyle explizit gesetzt hat. */
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

/* Gesamtstatus als Hero: große Form, getönte Fläche, Text. Farbe nie allein. */
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

/* Abschnitte */
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

/* Vorfall mit Zeitleiste */
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

/* Notices - reuse .incident-card visual language, eigene
   Level-Farben statt Incident-Status-Farben */
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

/* Hilfe-Icon + Tooltip am Suchfeld */
.search-help-btn {
  flex-shrink: 0;
  width: 24px;
  height: 24px;
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
.search-wrapper {
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

.search-wrapper .search-input {
  padding-left: 32px;
  padding-right: 30px;
}

.search-clear {
  position: absolute;
  right: var(--sp-2);
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
  /* Ohne Umbruch lagen Hinweis und Button bei 390 px rechts
     außerhalb der Karte (.subscribe-cta-actions bis x=782), weil die
     Aktionen nicht schrumpfen. Reicht der Platz nicht, stapeln sich die
     Aktionen jetzt unter dem Text. */
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: var(--sp-5);
}

/* Der Text darf auf die Kartenbreite schrumpfen (min-width: 0)
   und nimmt mindestens 240 px ein, bevor die Aktionen umbrechen. */
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
  /* Hinweis „noch kein Abonnement“ und die Knöpfe brechen
     untereinander um, statt die Karte zu sprengen. */
  flex-wrap: wrap;
  align-items: center;
  gap: var(--sp-3);
  flex-shrink: 0;
  max-width: 100%;
}

/* Vergangene Vorfälle: kompakte Zeilen statt Karten, bricht in
   schmalen Ansichten um statt horizontal zu scrollen. */
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

/* "Verlauf anzeigen" Button auf der Incident-Karte */
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

/* Verlauf-Modal + Timeline, im Stil von AdminIncidentDetail.vue übernommen
   (.tl-row etc.) damit Admin- und Public-Ansicht der Timeline konsistent
   aussehen. Scoped styles vererben sich nicht component-übergreifend,
   daher hier dupliziert statt importiert. */
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
