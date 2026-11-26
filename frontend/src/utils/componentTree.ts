// GET /api/pages/:id/components returns a tree (top level, subcomponents
// in Children). The admin pages used to read only the top level, so
// subcomponents were neither visible nor selectable there. These helpers turn
// it into a flat list in tree order (parents before their children, siblings
// in the backend's order) with an indentation depth.
// Rendered flat instead of recursively, so that tables and selects as well as
// the name lookup by ID (find) get by without recursion of their own.

/** A tree node as the components endpoint sends it. */
export interface ComponentTreeNode {
  ID: number
  Name: string
  Children?: ComponentTreeNode[] | null
  ArchivedAt?: string | null
  [key: string]: unknown
}

/** A node of the flattened tree: the node itself plus its depth (0 = top level). */
export type FlatComponent<T extends ComponentTreeNode = ComponentTreeNode> = T & { depth: number }

export interface ComponentOption {
  label: string
  value: string
}

export const flattenComponentTree = <T extends ComponentTreeNode>(nodes: readonly T[] | null | undefined, depth = 0): FlatComponent<T>[] =>
  (nodes || []).flatMap(n => [{ ...n, depth }, ...flattenComponentTree(n.Children as T[] | null | undefined, depth + 1)])

// Indentation per level. An em space (U+2003) instead of normal spaces,
// because HTML does not collapse it - the indentation stays visible in the
// select flyout.
const INDENT = ' '

// Options for OnyxSelect from the flat list: name indented by depth.
// Archived components (ArchivedAt) are no longer offered for selection
// (incidents, maintenances, templates, subscribers, group); keepIds keeps
// already chosen ones (e.g. the current parent group) in the selection anyway.
export const componentOptions = (flat: readonly { ID: number | string; Name: string; depth?: number; ArchivedAt?: string | null }[] | null | undefined, keepIds: readonly (number | string)[] | null = []): ComponentOption[] =>
  (flat || []).filter(c => !c.ArchivedAt || (keepIds || []).some(k => String(k) === String(c.ID))).map(c => ({ label: INDENT.repeat(c.depth || 0) + c.Name, value: String(c.ID) }))

// Components page following direction C: groups are collapsible, the
// search shows hits together with their parents. Both work on the flat list
// (tree order, `depth`): the descendants of an entry are the directly
// following entries with greater depth.

interface FlatEntry {
  ID: number
  depth: number
}

// IDs of all descendants of the entry with the ID `id` (empty if it has none
// or is missing).
export const descendantIds = (flat: readonly FlatEntry[], id: number): number[] => {
  const start = flat.findIndex(c => c.ID === id)
  if (start === -1) return []
  const ids: number[] = []
  for (let i = start + 1; i < flat.length && (flat[i] as FlatEntry).depth > (flat[start] as FlatEntry).depth; i++) ids.push((flat[i] as FlatEntry).ID)
  return ids
}

// Visible rows of the list. With a search term: entries whose text matches,
// together with all ancestors (so that the hit sits in the tree); collapsed groups
// do not count then, otherwise a hit in a collapsed group would be
// impossible to find. Without a search term: descendants of collapsed groups are omitted.
// `textOf(c)` returns the searchable text of an entry.
export const visibleComponents = <T extends FlatEntry>(flat: readonly T[], { query = '', collapsed = new Set<number>(), textOf }: { query?: string; collapsed?: ReadonlySet<number>; textOf: (c: T) => string }): T[] => {
  const needle = query.trim().toLowerCase()
  if (needle) {
    const keep = new Set<number>()
    flat.forEach((c, i) => {
      if (!textOf(c).toLowerCase().includes(needle)) return
      keep.add(c.ID)
      let depth = c.depth
      for (let j = i - 1; j >= 0 && depth > 0; j--) {
        if ((flat[j] as T).depth < depth) { keep.add((flat[j] as T).ID); depth = (flat[j] as T).depth }
      }
    })
    return flat.filter(c => keep.has(c.ID))
  }
  const rows: T[] = []
  let hiddenBelow: number | null = null
  for (const c of flat) {
    if (hiddenBelow !== null && c.depth > hiddenBelow) continue
    hiddenBelow = collapsed.has(c.ID) ? c.depth : null
    rows.push(c)
  }
  return rows
}
