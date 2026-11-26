import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useContentLanguages } from './useContentLanguages'
import { languageFlag } from '../utils/languageFlags'
import type { Ref } from 'vue'
import type { ApiFetch } from './useApi'
import type { EntityId } from './useTranslationFactory'
import type { ApiError, EntityTranslation, TranslateResponse } from '../types/api'

/** Text of one further language in the form; done = confirmed. */
export interface TranslationDraft {
  title: string
  message: string
  done: boolean
}

/** Routes of the entity's translations (method syntax, so views may declare `id: number`). */
export interface FormTranslationUrls {
  /** Translations including drafts. */
  list(id: EntityId): string
  /** PUT of one language. */
  save(id: EntityId, code: string): string
}

export interface FormTranslationsOptions {
  apiFetch: ApiFetch
  urls: FormTranslationUrls
  contentField?: string
  /** The form's data of the default language; titleKey/contentKey name its fields. */
  formData: Ref<object>
  canEdit: Readonly<Ref<boolean>>
  titleKey?: string
  contentKey?: string
  contentRequired?: boolean
  titleRequired?: boolean
}

/** Translation state of a language in the switcher: done, filled but not released, or without text. */
export type TranslationState = 'done' | 'draft' | 'empty'

export interface FormLanguage {
  code: string
  name: string
}

export interface FormLanguageOption {
  value: string
  label: string
}

/** showAlert(msg, type) of a view. */
export type ShowAlert = (msg: string, type: string) => void

// Language switcher in the edit form. The page's default language is the
// record itself; for every further active language of the page this
// composable holds title, text and "done" state. The flow in the form: load
// the languages (load), fetch the existing translations when opening (loadFor)
// or clear them (reset), write all filled languages when saving (saveAll). A
// language without text stays without a translation; the status page then
// falls back to the default language.
//
// urls.list(id) returns the translations including drafts, urls.save(id, code)
// stores one language (PUT with title, <contentField>, status). With formData
// (fields titleKey/contentKey) and canEdit it also provides the bindings for
// the form: titleModel/contentModel show the fields of the default language or
// of the translation, depending on the language tab. With
// contentRequired=false (components: description optional) the title is
// enough for "filled". With titleRequired=false (incident updates only have a
// message) there is no title: "filled" then means a message is present, and
// no title is sent when saving and translating.
export function useFormTranslations({
  apiFetch, urls, contentField = 'message', formData, canEdit,
  titleKey = 'Title', contentKey = 'Message', contentRequired = true, titleRequired = true,
}: FormTranslationsOptions) {
  const titleless = !titleRequired
  const { t } = useI18n()
  const { catalog, pageLanguages, loadCatalog, loadPageLanguages } = useContentLanguages()

  // The form's fields by name (titleKey/contentKey are only known at runtime).
  const form = () => formData.value as Record<string, unknown>
  const active = ref('') // '' = default language
  const pageId = ref<EntityId | null>(null)
  // "Translate" button (suggestion from the default language).
  const translating = ref(false)
  const translateError = ref('')
  const translateFallback = ref(false) // suggestion came from the fallback service
  const translatingAll = ref(false)
  const translateAllProgress = ref('') // "Translating … 3 of 29"
  const translateAllSummary = ref('') // result after "translate all"
  const drafts = ref<Record<string, TranslationDraft>>({}) // code -> { title, message, done }

  const nameOf = (code: string): string => catalog.value.find(l => l.Code === code)?.Name || code
  const defaultCode = computed(() => pageLanguages.value.find(l => l.IsDefault)?.LanguageCode || '')
  const languages = computed<FormLanguage[]>(() => pageLanguages.value
    .filter(l => !l.IsDefault)
    .map(l => ({ code: l.LanguageCode, name: nameOf(l.LanguageCode) })))
  const multilingual = computed(() => languages.value.length > 0)

  const load = async (id: EntityId) => {
    pageId.value = id
    try {
      await Promise.all([loadCatalog(), loadPageLanguages(id)])
      // Treat an empty response (null) like "no languages".
      catalog.value ??= []
      pageLanguages.value ??= []
    } catch {
      // Without a language list the form stays monolingual instead of blocking the incident tab.
      pageLanguages.value = []
    }
  }

  const blank = (): TranslationDraft => ({ title: '', message: '', done: false })
  // Return the reactive object, not the result of the assignment (`??=` returns
  // the raw object): otherwise counters and tab state go stale while typing.
  const draftOf = (code: string): TranslationDraft => {
    if (!drafts.value[code]) drafts.value[code] = blank()
    return drafts.value[code]!
  }

  const reset = () => { active.value = ''; drafts.value = {}; translateError.value = ''; translateFallback.value = false; translateAllSummary.value = '' }
  watch(active, () => { translateError.value = ''; translateFallback.value = false })

  // Take over the API's translation rows as drafts (replaces existing ones). Also for "use
  // template": the template's texts end up in the form of the new entry this way.
  const setFromRows = (rows: EntityTranslation[]) => {
    const next: Record<string, TranslationDraft> = {}
    for (const r of rows) next[r.LanguageCode] = { title: r.Title ?? '', message: r[contentField.charAt(0).toUpperCase() + contentField.slice(1)] as string, done: r.Status === 'confirmed' }
    drafts.value = next
  }

  const loadFor = async (id: EntityId) => {
    reset()
    try {
      setFromRows(await apiFetch<EntityTranslation[]>(urls.list(id)))
    } catch {
      // Translations are optional (an empty response ends up here too); the form stays editable.
    }
  }

  const filled = (d: TranslationDraft | undefined): d is TranslationDraft => !!(d && (titleless ? d.message.trim() : d.title.trim() && (!contentRequired || d.message.trim())))
  const touched = (d: TranslationDraft | undefined): boolean => !!(d && (d.title.trim() || d.message.trim()))

  // 'done' finished, 'draft' filled but not released, 'empty' without text.
  const stateOf = (code: string): TranslationState => {
    const d = drafts.value[code]
    if (!filled(d)) return 'empty'
    return d.done ? 'done' : 'draft'
  }

  // First language with only a title or only a text (null if everything is consistent).
  const incompleteCode = (): string | null => languages.value.find(l => touched(drafts.value[l.code]) && !filled(drafts.value[l.code]))?.code ?? null

  // Before saving: a translation with only a title or only a text -> jump to the language and report
  // (true = everything consistent). showAlert(msg, type) is the view's message.
  const checkComplete = (showAlert: ShowAlert): boolean => {
    const code = showSwitcher.value ? incompleteCode() : null
    if (!code) return true
    active.value = code
    showAlert(t('formLanguages.translationIncomplete', { name: nameOf(code) }), 'warning')
    return false
  }

  const saveAll = async (id: EntityId) => {
    for (const { code } of languages.value) {
      const d = drafts.value[code]
      if (!filled(d)) continue
      await apiFetch(urls.save(id, code), {
        method: 'PUT',
        body: JSON.stringify({ ...(titleless ? {} : { title: d.title.trim() }), [contentField]: d.message.trim(), status: d.done ? 'confirmed' : 'draft' }),
      })
    }
  }

  // Bindings for the form (formData and canEdit are required).
  const showSwitcher = computed(() => multilingual.value && canEdit.value)
  const onDefaultLang = computed(() => !showSwitcher.value || active.value === '')
  const activeDraft = computed(() => draftOf(active.value))
  const bound = (key: string, field: 'title' | 'message') => computed<string>({
    get: () => onDefaultLang.value ? form()[key] as string : activeDraft.value[field],
    set: (v) => { if (onDefaultLang.value) form()[key] = v; else activeDraft.value[field] = v },
  })
  const titleModel = bound(titleKey, 'title')
  const contentModel = bound(contentKey, 'message')
  // Translating only works in a further language and with a title (without title: with a message)
  // in the default language.
  const canTranslate = computed(() => !onDefaultLang.value && !!(form()[titleless ? contentKey : titleKey] as string | undefined)?.trim())
  // Sends title and message of the default language to the service and enters the result as a
  // draft ("done" is withdrawn) into the chosen language. Errors (not set up, not reachable,
  // language not supported) end up in translateError.
  const translate = async () => {
    if (!canTranslate.value || translating.value) return
    const code = active.value
    translateError.value = ''
    translateFallback.value = false
    translating.value = true
    try {
      const res = await apiFetch<TranslateResponse>(`/api/pages/${pageId.value}/translate`, {
        method: 'POST',
        body: JSON.stringify({ target_language: code, ...(titleless ? {} : { title: form()[titleKey] }), message: form()[contentKey] ?? '' }),
      })
      Object.assign(draftOf(code), { title: titleless ? '' : res.title, message: res.message, done: false })
      translateFallback.value = res.fallback === true
    } catch (err) {
      translateError.value = (err as Error).message
    } finally {
      translating.value = false
    }
  }
  // "Translate all": translates title and message of the default language one after another into ALL
  // further languages of the page that are still empty (filled drafts and finished texts stay untouched).
  // Unsupported languages are skipped and counted; any other error aborts the run.
  const emptyLanguages = computed(() => languages.value.filter(l => stateOf(l.code) === 'empty'))
  const canTranslateAll = computed(() => showSwitcher.value && !!(form()[titleless ? contentKey : titleKey] as string | undefined)?.trim() && emptyLanguages.value.length > 0 && !translatingAll.value && !translating.value)
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
          const res = await apiFetch<TranslateResponse>(`/api/pages/${pageId.value}/translate`, {
            method: 'POST',
            body: JSON.stringify({ target_language: code, ...(titleless ? {} : { title: form()[titleKey] }), message: form()[contentKey] ?? '' }),
          })
          Object.assign(draftOf(code), { title: titleless ? '' : res.title, message: res.message, done: false })
          done++
        } catch (err) {
          // Only "language not supported" concerns a single language. Everything else (quota, access,
          // service or network gone, session expired, error without JSON body) would fail the same way for the
          // remaining languages: abort with the error text instead of hopeless requests.
          if ((err as ApiError).data?.code === 'translation_language_unsupported') { skipped++; continue }
          stopped = (err as Error).message
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
  // Flag (if known) before the name, so the language is quick to find even in long lists.
  const withFlag = (code: string, label: string) => [languageFlag(code), label].filter(Boolean).join(' ')
  const langOptions = computed<FormLanguageOption[]>(() => [
    { value: '', label: withFlag(defaultCode.value, t('formLanguages.langDefault', { name: nameOf(defaultCode.value) })) },
    ...languages.value.map(l => ({ value: l.code, label: withFlag(l.code, `${l.name} · ${t(`formLanguages.langState.${stateOf(l.code)}`)}`) })),
  ])

  return { titleless, canTranslate, translate, translating, translateError, translateFallback, canTranslateAll, translateAll, translatingAll, translateAllProgress, translateAllSummary, emptyLanguages, showSwitcher, onDefaultLang, activeDraft, titleModel, contentModel, langOptions, active, drafts, defaultCode, languages, multilingual, nameOf, load, loadFor, setFromRows, reset, draftOf, stateOf, incompleteCode, checkComplete, saveAll }
}

export type FormTranslations = ReturnType<typeof useFormTranslations>
