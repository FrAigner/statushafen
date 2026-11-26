import { computed } from 'vue'
import { useAuth } from './useAuth'
import { perm } from '../utils/permissions'

// Rechte eines Tabs je Aktion für die Views. „Neu …“-Knöpfe hängen
// an canCreate, Bearbeiten/Formular/Schnellstatus an canUpdate, Löschen an
// canDelete; ohne canUpdate öffnet sich das Formular schreibgeschützt.
export function useResourcePerms(resource) {
  const { hasPerm } = useAuth()
  return {
    canCreate: computed(() => hasPerm(perm(resource, 'create'))),
    canRead: computed(() => hasPerm(perm(resource, 'read'))),
    canUpdate: computed(() => hasPerm(perm(resource, 'update'))),
    canDelete: computed(() => hasPerm(perm(resource, 'delete'))),
  }
}
