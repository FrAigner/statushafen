// Shared tag selection helpers (replacement for the former free-text country
// field): toggleTag/tagNames were duplicated identically in
// AdminComponents/AdminIncidents/AdminMaintenance/AdminUsers (SonarCloud
// new-code duplication gate).

import { ref, computed } from 'vue'
import type { Ref } from 'vue'
import { useAuth } from './useAuth'
import type { ApiFetch } from './useApi'
import type { TenantId } from './useTenant'
import type { Tag } from '../types/api'

// toggleTag(formData) returns a toggle function for a particular formData
// ref: formData.value.TagIDs must be an array (created if needed), so that
// v-for="t in availableTags" @change="toggle(t.ID)" works directly against
// formData.value.TagIDs.
export function useTagToggle(formData: Ref<{ TagIDs?: number[] | null }>) {
  return (tagId: number) => {
    if (!formData.value.TagIDs) formData.value.TagIDs = []
    const idx = formData.value.TagIDs.indexOf(tagId)
    if (idx === -1) {
      formData.value.TagIDs.push(tagId)
    } else {
      formData.value.TagIDs.splice(idx, 1)
    }
  }
}

// tagNames(tags) joins the names of a tag list for display texts
// (tables, detail views); "—" for an empty/missing list.
export function tagNames(tags: Pick<Tag, 'Name'>[] | null | undefined): string {
  return (tags || []).map(t => t.Name).join(', ') || '—'
}

// GET /api/tags is global-admin-only. Global admins load global and
// page-specific tags through it. Page roles get their list from
// GET /api/pages/:id/tags (permission components:read or incidents:read), but may
// only assign page-specific tags (the backend rejects global ones with 403
// global_tag_forbidden), so only those are offered to them. Global tags an
// edited entry already carries stay in formData and are saved unchanged. If a
// page has no tags of its own, the field disappears entirely for page roles (a
// hint "create under Tags" would be wrong for them, tag management is not
// accessible to them).
export function useAvailableTags(apiFetch: ApiFetch, pageId: Readonly<Ref<TenantId | null>>) {
  const { isGlobalAdmin } = useAuth()
  const availableTags = ref<Tag[]>([])
  const canSelectTags = computed(() => isGlobalAdmin.value || availableTags.value.length > 0)

  const loadAvailableTags = async () => {
    try {
      if (isGlobalAdmin.value) {
        availableTags.value = (await apiFetch<Tag[] | null>(`/api/tags?status_page_id=${pageId.value}`)) || []
      } else {
        const tags = (await apiFetch<Tag[] | null>(`/api/pages/${pageId.value}/tags`)) || []
        availableTags.value = tags.filter(tag => tag.StatusPageID)
      }
    } catch {
      availableTags.value = []
    }
  }

  return { availableTags, canSelectTags, loadAvailableTags }
}
