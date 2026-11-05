import { describe, it, expect } from 'vitest'
import { vueSources, templatePart, startTags } from '../../test-support/templateScan.js'

// Redesign Stufe 4d, Verwaltung/System: Schutz gegen den Rückfall in
// Inline-Styles und in eine zweite h1.
//
// - Die Verwaltungs-Views haben keine Inline-Styles mehr (style="…" und
//   :style="…"). Layout, Abstände und Farben stehen in Klassen und Tokens;
//   Farben für Kategorien/Status kommen aus Klassen, nicht aus Hex im Template.
// - Die Admin-Topbar trägt die h1 (Marke). Die Seitentitel dieser Views sind
//   h2 (PageHeader :level="2"); ein <h1> im Template würde die Überschriftenfolge
//   brechen.
const VIEWS = [
  'views/admin/AdminStatusPages.vue',
  'views/admin/AdminUsers.vue',
  'views/admin/AdminRoles.vue',
  'views/admin/AdminTags.vue',
  'views/admin/AdminAuditLog.vue',
  'views/admin/AdminMonitoring.vue',
  'views/admin/AdminSSO.vue',
  'views/admin/AdminUiTranslations.vue',
  'views/admin/AdminSystemSettings.vue',
  'views/admin/ProfileView.vue',
  'components/PermissionMatrix.vue',
]

describe.each(VIEWS)('%s (Stufe 4d)', (file) => {
  const tags = startTags(templatePart(vueSources[file]))

  it('hat keine Inline-Styles im Template', () => {
    const mitStyle = tags.filter(tag => 'style' in tag.attrs || ':style' in tag.attrs).map(tag => tag.raw)
    expect(mitStyle).toEqual([])
  })

  it('enthält keine h1 (die Admin-Topbar trägt sie)', () => {
    expect(tags.filter(tag => tag.name === 'h1').map(tag => tag.raw)).toEqual([])
  })
})

describe('PageHeader (level 2) statt eigener Kopfzeile', () => {
  const MIT_KOPF = VIEWS.filter(f => f.startsWith('views/'))

  it.each(MIT_KOPF)('%s nutzt PageHeader mit level=2 und keine .page-header/.content-header', (file) => {
    const template = templatePart(vueSources[file])
    expect(template).toMatch(/<PageHeader\s+:level="2"/)
    expect(template).not.toMatch(/class="(page-header|content-header)"/)
  })
})
