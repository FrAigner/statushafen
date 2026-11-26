// Gemeinsame Tag-Auswahl-Helfer (Ersatz für das frühere
// Country-Freitextfeld) - toggleTag/tagNames waren identisch in
// AdminComponents/AdminIncidents/AdminMaintenance/AdminUsers dupliziert
// (SonarCloud New-Code-Duplication-Gate).

import { ref, computed } from 'vue'
import { useAuth } from './useAuth'

// toggleTag(formData) liefert eine toggle-Funktion für ein bestimmtes
// formData-Ref: formData.value.TagIDs muss ein Array sein (wird bei Bedarf
// angelegt), damit v-for="t in availableTags" @change="toggle(t.ID)"
// direkt gegen formData.value.TagIDs arbeitet.
export function useTagToggle(formData) {
  return (tagId) => {
    if (!formData.value.TagIDs) formData.value.TagIDs = []
    const idx = formData.value.TagIDs.indexOf(tagId)
    if (idx === -1) {
      formData.value.TagIDs.push(tagId)
    } else {
      formData.value.TagIDs.splice(idx, 1)
    }
  }
}

// tagNames(tags) joint die Namen einer Tag-Liste für Anzeigetexte
// (Tabellen, Detail-Ansichten) - "—" bei leerer/fehlender Liste.
export function tagNames(tags) {
  return (tags || []).map(t => t.Name).join(', ') || '—'
}

// GET /api/tags ist global-admin-only. Global Admins
// laden darüber globale und seiteneigene Tags. Seiten-Rollen bekommen ihre Liste
// aus GET /api/pages/:id/tags (Recht components:read oder incidents:read), dürfen aber nur
// seiteneigene Tags vergeben (das Backend lehnt globale mit 403
// global_tag_forbidden ab) - angeboten werden ihnen deshalb nur diese. Globale
// Tags, die ein bearbeiteter Eintrag schon trägt, bleiben im formData und werden
// unverändert mitgespeichert. Hat eine Seite keine eigenen Tags, entfällt das
// Feld für Seiten-Rollen ganz (ein Hinweis „Unter Tags anlegen“ wäre für sie
// falsch, die Tag-Verwaltung ist ihnen nicht zugänglich).
export function useAvailableTags(apiFetch, pageId) {
  const { isGlobalAdmin } = useAuth()
  const availableTags = ref([])
  const canSelectTags = computed(() => isGlobalAdmin.value || availableTags.value.length > 0)

  const loadAvailableTags = async () => {
    try {
      if (isGlobalAdmin.value) {
        availableTags.value = (await apiFetch(`/api/tags?status_page_id=${pageId.value}`)) || []
      } else {
        const tags = (await apiFetch(`/api/pages/${pageId.value}/tags`)) || []
        availableTags.value = tags.filter(tag => tag.StatusPageID)
      }
    } catch {
      availableTags.value = []
    }
  }

  return { availableTags, canSelectTags, loadAvailableTags }
}
