// Redesign stage 3: the only place that maps component status to level,
// shape and wording. StatusIcon, StatusBadge and QuickStatus read only from
// here; views never compute with raw values themselves.
//
// Four levels for components (operational, degraded, partial, major).
// Maintenance is not a component status in the backend (maintenances are
// incidents with kind "maintenance"); the status page derives it from a running
// maintenance. That is why it exists as a fifth presentation but does not belong
// in STATUS_LEVELS and therefore not in the quick status.
//
// Raw backend values: operational, degraded_performance, partial_outage,
// major_outage (models.go). The short forms are understood as well because
// views and tests already use them.

export type ComponentStatusLevel = 'operational' | 'degraded' | 'partial' | 'major'
export type StatusLevel = ComponentStatusLevel | 'maintenance'
export type StatusShape = 'circle-check' | 'triangle-exclamation' | 'diamond-minus' | 'octagon-cross' | 'square-clock' | 'circle'

export interface StatusMeta {
  level: StatusLevel | null
  shape: StatusShape
  labelKey: string | null
}

export const STATUS_LEVELS: readonly ComponentStatusLevel[] = Object.freeze(['operational', 'degraded', 'partial', 'major'])
export const STATUS_MAINTENANCE = 'maintenance'

const RAW_BY_LEVEL: Readonly<Record<ComponentStatusLevel, string>> = Object.freeze({
  operational: 'operational',
  degraded: 'degraded_performance',
  partial: 'partial_outage',
  major: 'major_outage',
})

const LEVEL_BY_VALUE: Readonly<Record<string, StatusLevel>> = Object.freeze({
  operational: 'operational',
  degraded: 'degraded',
  degraded_performance: 'degraded',
  partial: 'partial',
  partial_outage: 'partial',
  major: 'major',
  major_outage: 'major',
  maintenance: STATUS_MAINTENANCE,
  // Raw backend value (componentstatus.UnderMaintenance), e.g. from a running
  // maintenance of a linked external source.
  under_maintenance: STATUS_MAINTENANCE,
})

// i18n keys (status.*); the wording "Beeinträchtigt" stays.
const LABEL_KEY: Readonly<Record<StatusLevel, string>> = Object.freeze({
  operational: 'status.operational',
  degraded: 'status.degradedPerformance',
  partial: 'status.partialOutage',
  major: 'status.majorOutage',
  maintenance: 'status.maintenance',
})

// Shape per level: status is always shape + colour + text, never colour alone.
const SHAPE: Readonly<Record<StatusLevel, StatusShape>> = Object.freeze({
  operational: 'circle-check',
  degraded: 'triangle-exclamation',
  partial: 'diamond-minus',
  major: 'octagon-cross',
  maintenance: 'square-clock',
})

// Level for a raw or short value; unknown values yield null.
export function normalizeStatus(value: unknown): StatusLevel | null {
  return Object.hasOwn(LEVEL_BY_VALUE, value as PropertyKey) ? LEVEL_BY_VALUE[value as string] as StatusLevel : null
}

// Raw backend value for one of the four levels (for writes);
// maintenance and unknown values yield null.
export function toRawStatus(value: unknown): string | null {
  const level = normalizeStatus(value)
  return level && Object.hasOwn(RAW_BY_LEVEL, level) ? RAW_BY_LEVEL[level as ComponentStatusLevel] : null
}

// Everything the presentation needs. Unknown values get a neutral
// presentation (level null, shape "circle"), so that no status colour is ever
// claimed that does not exist.
export function statusMeta(value: unknown): StatusMeta {
  const level = normalizeStatus(value)
  if (!level) return { level: null, shape: 'circle', labelKey: null }
  return { level, shape: SHAPE[level], labelKey: LABEL_KEY[level] }
}
