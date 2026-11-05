import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useContentLanguages } from './useContentLanguages'
import { languageFlag } from '../utils/languageFlags'

// Sprachumschalter im Bearbeiten-Formular. Die Standardsprache der
// Seite ist der Datensatz selbst; für jede weitere aktive Sprache der Seite
// hält dieses Composable Titel, Text und „fertig“-Stand. Der Ablauf im Formular:
// Sprachen laden (load), beim Öffnen die vorhandenen Übersetzungen holen
// (loadFor) bzw. leeren (reset), beim Speichern alle ausgefüllten Sprachen
// schreiben (saveAll). Eine Sprache ohne Text bleibt ohne Übersetzung, die
// Statusseite fällt dann auf die Standardsprache zurück.
//
// urls.list(id) liefert die Übersetzungen inkl. Entwürfe, urls.save(id, code)
// speichert eine Sprache (PUT mit title, <contentField>, status). Mit formData
// (Felder titleKey/contentKey) und canEdit liefert es zusätzlich die Bindungen
// für das Formular: titleModel/contentModel zeigen je nach Sprachreiter die
// Felder der Standardsprache oder der Übersetzung. Mit contentRequired=false
// (Komponenten: Beschreibung optional) genügt der Titel für „ausgefüllt“.
// Mit titleRequired=false (Updates zu Vorfällen haben nur eine
// Nachricht) gibt es keinen Titel: „ausgefüllt“ heißt dann Nachricht vorhanden,
// beim Speichern und Übersetzen wird kein Titel gesendet.
export function useFormTranslations({
  apiFetch, urls, contentField = 'message', formData, canEdit,
  titleKey = 'Title', contentKey = 'Message', contentRequired = true, titleRequired = true,
}) {
  const titleless = !titleRequired
  const { t } = useI18n()
  const { catalog, pageLanguages, loadCatalog, loadPageLanguages } = useContentLanguages()

  const active = ref('') // '' = Standardsprache
  const pageId = ref(null)
  // „Übersetzen“-Knopf (Vorschlag aus der Standardsprache).
  const translating = ref(false)
  const translateError = ref('')
  const translateFallback = ref(false) // Vorschlag kam vom Ersatzdienst
  const translatingAll = ref(false)
  const translateAllProgress = ref('') // „Übersetze … 3 von 29“
  const translateAllSummary = ref('') // Ergebnis nach „alles übersetzen“
  const drafts = ref({}) // code -> { title, message, done }

  const nameOf = (code) => catalog.value.find(l => l.Code === code)?.Name || code
  const defaultCode = computed(() => pageLanguages.value.find(l => l.IsDefault)?.LanguageCode || '')
  const languages = computed(() => pageLanguages.value
    .filter(l => !l.IsDefault)
    .map(l => ({ code: l.LanguageCode, name: nameOf(l.LanguageCode) })))
  const multilingual = computed(() => languages.value.length > 0)

  const load = async (id) => {
    pageId.value = id
    try {
      await Promise.all([loadCatalog(), loadPageLanguages(id)])
      // Eine leere Antwort (null) wie „keine Sprachen“ behandeln.
      catalog.value ??= []
      pageLanguages.value ??= []
    } catch {
      // Ohne Sprachliste bleibt das Formular einsprachig statt den Vorfall-Tab zu blockieren.
      pageLanguages.value = []
    }
  }

  const blank = () => ({ title: '', message: '', done: false })
  // Das reaktive Objekt zurückgeben, nicht das Ergebnis der Zuweisung (`??=` liefert
  // das rohe Objekt): sonst bleiben Zähler und Reiterstand beim Tippen veraltet.
  const draftOf = (code) => {
    if (!drafts.value[code]) drafts.value[code] = blank()
    return drafts.value[code]
  }

  const reset = () => { active.value = ''; drafts.value = {}; translateError.value = ''; translateFallback.value = false; translateAllSummary.value = '' }
  watch(active, () => { translateError.value = ''; translateFallback.value = false })

  // Übersetzungszeilen der API als Entwürfe übernehmen (ersetzt vorhandene). Auch für „Vorlage
  // verwenden“: die Texte der Vorlage landen so im Formular des neuen Eintrags.
  const setFromRows = (rows) => {
    const next = {}
    for (const r of rows) next[r.LanguageCode] = { title: r.Title ?? '', message: r[contentField.charAt(0).toUpperCase() + contentField.slice(1)], done: r.Status === 'confirmed' }
    drafts.value = next
  }

  const loadFor = async (id) => {
    reset()
    try {
      setFromRows(await apiFetch(urls.list(id)))
    } catch {
      // Übersetzungen sind optional (auch eine leere Antwort landet hier); das Formular bleibt bearbeitbar.
    }
  }

  const filled = (d) => !!(d && (titleless ? d.message.trim() : d.title.trim() && (!contentRequired || d.message.trim())))
  const touched = (d) => !!(d && (d.title.trim() || d.message.trim()))

  // 'done' fertig, 'draft' ausgefüllt aber nicht freigegeben, 'empty' ohne Text.
  const stateOf = (code) => {
    const d = drafts.value[code]
    if (!filled(d)) return 'empty'
    return d.done ? 'done' : 'draft'
  }

  // Erste Sprache mit nur Titel oder nur Text (null, wenn alles stimmig ist).
  const incompleteCode = () => languages.value.find(l => touched(drafts.value[l.code]) && !filled(drafts.value[l.code]))?.code ?? null

  // Vor dem Speichern: Übersetzung mit nur Titel oder nur Text → zur Sprache springen und melden
  // (true = alles stimmig). showAlert(msg, type) ist die Meldung der Ansicht.
  const checkComplete = (showAlert) => {
    const code = showSwitcher.value ? incompleteCode() : null
    if (!code) return true
    active.value = code
    showAlert(t('formLanguages.translationIncomplete', { name: nameOf(code) }), 'warning')
    return false
  }

  const saveAll = async (id) => {
    for (const { code } of languages.value) {
      const d = drafts.value[code]
      if (!filled(d)) continue
      await apiFetch(urls.save(id, code), {
        method: 'PUT',
        body: JSON.stringify({ ...(titleless ? {} : { title: d.title.trim() }), [contentField]: d.message.trim(), status: d.done ? 'confirmed' : 'draft' }),
      })
    }
  }

  // Bindungen fürs Formular (formData und canEdit sind Pflicht).
  const showSwitcher = computed(() => multilingual.value && canEdit.value)
  const onDefaultLang = computed(() => !showSwitcher.value || active.value === '')
  const activeDraft = computed(() => draftOf(active.value))
  const bound = (key, field) => computed({
    get: () => onDefaultLang.value ? formData.value[key] : activeDraft.value[field],
    set: (v) => { if (onDefaultLang.value) formData.value[key] = v; else activeDraft.value[field] = v },
  })
  const titleModel = bound(titleKey, 'title')
  const contentModel = bound(contentKey, 'message')
  // Übersetzen geht nur in einer weiteren Sprache und mit Titel (ohne Titel: mit Nachricht)
  // in der Standardsprache.
  const canTranslate = computed(() => !onDefaultLang.value && !!formData.value[titleless ? contentKey : titleKey]?.trim())
  // Schickt Titel und Nachricht der Standardsprache an den Dienst und trägt das Ergebnis als
  // Entwurf („fertig“ wird zurückgenommen) in die gewählte Sprache ein. Fehler (nicht
  // eingerichtet, nicht erreichbar, Sprache nicht unterstützt) landen in translateError.
  const translate = async () => {
    if (!canTranslate.value || translating.value) return
    const code = active.value
    translateError.value = ''
    translateFallback.value = false
    translating.value = true
    try {
      const res = await apiFetch(`/api/pages/${pageId.value}/translate`, {
        method: 'POST',
        body: JSON.stringify({ target_language: code, ...(titleless ? {} : { title: formData.value[titleKey] }), message: formData.value[contentKey] ?? '' }),
      })
      Object.assign(draftOf(code), { title: titleless ? '' : res.title, message: res.message, done: false })
      translateFallback.value = res.fallback === true
    } catch (err) {
      translateError.value = err.message
    } finally {
      translating.value = false
    }
  }
  // „Alles übersetzen“: übersetzt Titel und Nachricht der Standardsprache nacheinander in ALLE weiteren
  // Sprachen der Seite, die noch leer sind (ausgefüllte Entwürfe und fertige Texte bleiben unberührt).
  // Nicht unterstützte Sprachen werden übersprungen und gezählt; jeder andere Fehler bricht den Lauf ab.
  const emptyLanguages = computed(() => languages.value.filter(l => stateOf(l.code) === 'empty'))
  const canTranslateAll = computed(() => showSwitcher.value && !!formData.value[titleless ? contentKey : titleKey]?.trim() && emptyLanguages.value.length > 0 && !translatingAll.value && !translating.value)
  const translateAll = async () => {
    if (!canTranslateAll.value) return
    const targets = emptyLanguages.value.map(l => l.code)
    translateError.value = ''
    translateAllSummary.value = ''
    translatingAll.value = true
    let done = 0
    let skipped = 0
    let stopped = ''
    try {
      for (const code of targets) {
        translateAllProgress.value = t('formLanguages.translatingAll', { n: done + skipped + 1, total: targets.length })
        try {
          const res = await apiFetch(`/api/pages/${pageId.value}/translate`, {
            method: 'POST',
            body: JSON.stringify({ target_language: code, ...(titleless ? {} : { title: formData.value[titleKey] }), message: formData.value[contentKey] ?? '' }),
          })
          Object.assign(draftOf(code), { title: titleless ? '' : res.title, message: res.message, done: false })
          done++
        } catch (err) {
          // Nur „Sprache nicht unterstützt“ betrifft eine einzelne Sprache. Alles andere (Kontingent, Zugang,
          // Dienst oder Netz weg, Sitzung abgelaufen, Fehler ohne JSON-Körper) würde bei den übrigen Sprachen
          // genauso scheitern: Abbruch mit dem Fehlertext statt aussichtsloser Anfragen.
          if (err.data?.code === 'translation_language_unsupported') { skipped++; continue }
          stopped = err.message
          break
        }
      }
    } finally {
      translatingAll.value = false
      translateAllProgress.value = ''
    }
    translateAllSummary.value = stopped
      ? t('formLanguages.translateAllStopped', { done, reason: stopped })
      : t('formLanguages.translateAllDone', { done, skipped })
  }
  // Flagge (falls bekannt) vor den Namen, damit man die Sprache auch in langen Listen schnell findet.
  const withFlag = (code, label) => [languageFlag(code), label].filter(Boolean).join(' ')
  const langOptions = computed(() => [
    { value: '', label: withFlag(defaultCode.value, t('formLanguages.langDefault', { name: nameOf(defaultCode.value) })) },
    ...languages.value.map(l => ({ value: l.code, label: withFlag(l.code, `${l.name} · ${t(`formLanguages.langState.${stateOf(l.code)}`)}`) })),
  ])

  return { titleless, canTranslate, translate, translating, translateError, translateFallback, canTranslateAll, translateAll, translatingAll, translateAllProgress, translateAllSummary, emptyLanguages, showSwitcher, onDefaultLang, activeDraft, titleModel, contentModel, langOptions, active, drafts, defaultCode, languages, multilingual, nameOf, load, loadFor, setFromRows, reset, draftOf, stateOf, incompleteCode, checkComplete, saveAll }
}
