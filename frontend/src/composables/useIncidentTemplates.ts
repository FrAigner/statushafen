import { ref } from 'vue'

// Lädt Incident-/Wartungsfenster-Vorlagen für eine Statusseite
// und filtert nach kind ('incident' | 'maintenance'). Vorlagen sind an die
// incident_templates:read-Berechtigung gebunden; loadTemplates schlägt deshalb ohne
// Fehler fehl (leere Liste), damit ein 403 hier nicht die eigentliche
// Listenansicht für Nutzer ohne Schreibrecht mitreißt.
export function useIncidentTemplates(kind) {
  const templates = ref([])
  const selectedTemplateId = ref('')

  const loadTemplates = async (apiFetch, tenantId, hasPerm) => {
    if (!hasPerm('incident_templates:read')) return
    try {
      const tpls = await apiFetch(`/api/pages/${tenantId}/incident-templates`)
      templates.value = (tpls || []).filter(t => t.Kind === kind)
    } catch (err) {
      // Silently degrade — user may lack incident_templates:read permission (403)
      console.error('Incident-Templates konnten nicht geladen werden:', err)
      templates.value = []
    }
  }

  const selectedTemplate = () =>
    templates.value.find(t => String(t.ID) === String(selectedTemplateId.value)) || null

  // Übersetzungen einer Vorlage (leer, wenn sie fehlen oder nicht lesbar sind), damit
  // „Vorlage verwenden“ auch die Texte der weiteren Sprachen ins Formular übernimmt.
  const loadTemplateTranslations = async (apiFetch, tenantId, templateId) => {
    try {
      return (await apiFetch(`/api/pages/${tenantId}/incident-templates/${templateId}/translations`)) || []
    } catch (err) {
      console.error('Übersetzungen der Vorlage konnten nicht geladen werden:', err)
      return []
    }
  }

  return { templates, selectedTemplateId, loadTemplates, selectedTemplate, loadTemplateTranslations }
}
