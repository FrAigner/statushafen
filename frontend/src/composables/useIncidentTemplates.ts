import { ref } from 'vue'
import type { ApiFetch } from './useApi'
import type { TenantId } from './useTenant'
import type { EntityTranslation, IncidentTemplate } from '../types/api'

// Loads incident/maintenance window templates for a status page and filters
// by kind ('incident' | 'maintenance'). Templates are bound to the
// incident_templates:read permission; loadTemplates therefore fails without an
// error (empty list), so a 403 here does not drag down the actual list view
// for users without write permission.
export function useIncidentTemplates(kind: 'incident' | 'maintenance') {
  const templates = ref<IncidentTemplate[]>([])
  const selectedTemplateId = ref('')

  const loadTemplates = async (apiFetch: ApiFetch, tenantId: TenantId | null, hasPerm: (required: string) => boolean) => {
    if (!hasPerm('incident_templates:read')) return
    try {
      const tpls = await apiFetch<IncidentTemplate[] | null>(`/api/pages/${tenantId}/incident-templates`)
      templates.value = (tpls || []).filter(t => t.Kind === kind)
    } catch (err) {
      // Silently degrade — user may lack incident_templates:read permission (403)
      console.error('Incident-Templates konnten nicht geladen werden:', err)
      templates.value = []
    }
  }

  const selectedTemplate = (): IncidentTemplate | null =>
    templates.value.find(t => String(t.ID) === String(selectedTemplateId.value)) || null

  // Translations of a template (empty if they are missing or not readable), so that
  // "use template" also takes the texts of the further languages into the form.
  const loadTemplateTranslations = async (apiFetch: ApiFetch, tenantId: TenantId | null, templateId: number): Promise<EntityTranslation[]> => {
    try {
      return (await apiFetch<EntityTranslation[] | null>(`/api/pages/${tenantId}/incident-templates/${templateId}/translations`)) || []
    } catch (err) {
      console.error('Übersetzungen der Vorlage konnten nicht geladen werden:', err)
      return []
    }
  }

  return { templates, selectedTemplateId, loadTemplates, selectedTemplate, loadTemplateTranslations }
}
