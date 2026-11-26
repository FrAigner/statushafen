import { computed } from 'vue'
import { useAuth } from './useAuth'
import { perm } from '../utils/permissions'

// Permissions of a tab per action for the views. "New …" buttons depend on
// canCreate, edit/form/quick status on canUpdate, delete on canDelete; without
// canUpdate the form opens read-only.
export function useResourcePerms(resource: string) {
  const { hasPerm } = useAuth()
  return {
    canCreate: computed(() => hasPerm(perm(resource, 'create'))),
    canRead: computed(() => hasPerm(perm(resource, 'read'))),
    canUpdate: computed(() => hasPerm(perm(resource, 'update'))),
    canDelete: computed(() => hasPerm(perm(resource, 'delete'))),
  }
}
