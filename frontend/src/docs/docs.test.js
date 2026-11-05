// happy-dom ist für DOMPurify zu unvollständig (siehe
// AdminNotificationTemplates.test.js): Die Bereinigung würde dort nicht greifen.
// @vitest-environment jsdom
import { describe, it, expect } from 'vitest'
import {
  SECTION_IDS,
  DOC_SOURCES,
  slugify,
  byLocaleAndSlug,
  renderMarkdown,
  extractTitle,
  stripTitle,
  resolveSection,
  plainText,
} from './index.js'
import { NOTIFICATION_TEMPLATE_PLACEHOLDERS } from '../utils/notificationTemplatePlaceholders.js'
import de from '../i18n/locales/de.json'
import en from '../i18n/locales/en.json'

// Inhalte und Rendering der eingebauten Dokumentation.

describe('Doku-Inhalte', () => {
  // Die Hilfeseite nannte Abonnent, Ankündigung und Wartung als "ohne Chip".
  it('audit-log nennt jede Kategorie der Filter-Chips in beiden Sprachen', () => {
    const cats = ['Auth', 'Incident', 'Config', 'User', 'Subscriber', 'Notice', 'Maintenance', 'System']
    for (const [loc, messages] of [['de', de], ['en', en]]) {
      const md = DOC_SOURCES[loc]['audit-log']
      for (const c of cats) {
        const label = messages.auditLog[`cat${c}`]
        expect(label, `${loc}: auditLog.cat${c}`).toBeTruthy()
        expect(md, `${loc}/audit-log.md ohne ${label}`).toContain(label)
      }
      expect(md).not.toMatch(/keinen eigenen Chip|no chip\s+of their own/)
    }
  })


  // Review B1: Die Verdichtung der Statushistorie löscht nie einen Ausfall; die Hilfe darf
  // keine falsche Abweichung („höchstens 30 s je Eintrag“, „ein Eintrag mit dem Endstand“) versprechen.
  it('Verfügbarkeits-Hilfe beschreibt die pessimistische Verdichtung (zwei Einträge je Fenster)', () => {
    const de = DOC_SOURCES.de['oeffentliche-statusseite']
    const en = DOC_SOURCES.en['oeffentliche-statusseite']
    expect(de).toMatch(/höchstens zwei Einträge/)
    expect(de).toMatch(/pessimistisch/)
    expect(de).not.toMatch(/um höchstens 30\s+Sekunden ab/)
    expect(en).toMatch(/at most two entries/)
    expect(en).toMatch(/pessimistic/)
    expect(en).not.toMatch(/deviates by at most 30 seconds per entry/)
    expect(de).toContain('history_truncated')
    expect(en).toContain('history_truncated')
  })

  // Eine Rolle je Konto und Seite, letzter Owner, SSO-Verknüpfung beim Entfernen.
  it('Seitenbenutzer-Hilfe nennt letzten Owner und SSO-Verknüpfung; Fehlercode ist übersetzt', () => {
    for (const [loc, messages] of [['de', de], ['en', en]]) {
      for (const page of ['statusseiten', 'benutzer-und-rollen']) {
        const md = DOC_SOURCES[loc][page]
        expect(md, `${loc}/${page}.md ohne pages:update-Hinweis`).toMatch(/pages:update/)
        expect(md, `${loc}/${page}.md ohne SSO-Verknüpfung`).toMatch(/SSO/)
        expect(md, `${loc}/${page}.md ohne Eindeutigkeit der Seitenrolle`).toMatch(/genau eine Rolle|exactly one role/)
      }
      expect(JSON.stringify(messages), `${loc}: last_page_owner`).toContain('"last_page_owner"')
    }
  })

  it('jeder Abschnitt der Navigation hat eine deutsche Datei mit Titel und Text', () => {
    for (const id of SECTION_IDS) {
      const md = DOC_SOURCES.de[id]
      expect(md, `de/${id}.md fehlt`).toBeTruthy()
      expect(extractTitle(md), `de/${id}.md ohne Überschrift`).not.toBe('')
      expect(plainText(md).length, `de/${id}.md ist leer`).toBeGreaterThan(200)
    }
  })

  // Englisch darf fehlen (Fallback mit Hinweis), soll aber für alle
  // Abschnitte vorliegen. Fehlt eine Datei, muss resolveSection den Fallback
  // melden, damit die Seite den Hinweis zeigt.
  it('jeder Abschnitt hat englischen Inhalt oder liefert den deutschen Fallback mit Hinweis', () => {
    for (const id of SECTION_IDS) {
      const r = resolveSection(id, 'en')
      expect(r, id).not.toBeNull()
      if (DOC_SOURCES.en?.[id]) {
        expect(r.fallback).toBe(false)
        expect(r.locale).toBe('en')
        expect(extractTitle(r.markdown)).not.toBe('')
      } else {
        expect(r.fallback).toBe(true)
        expect(r.locale).toBe('de')
      }
    }
    expect(Object.keys(DOC_SOURCES.en).sort()).toEqual([...SECTION_IDS].sort())
  })

  it('es gibt keine Doku-Dateien, die in der Navigation fehlen', () => {
    for (const locale of Object.keys(DOC_SOURCES)) {
      for (const id of Object.keys(DOC_SOURCES[locale])) {
        expect(SECTION_IDS, `${locale}/${id}.md ist nicht in SECTION_IDS`).toContain(id)
      }
    }
  })

  // Die Platzhalter-Tabelle muss mit der Liste im Editor übereinstimmen.
  it('Mail-Templates nennen jeden Platzhalter des Editors, in beiden Sprachen', () => {
    for (const locale of ['de', 'en']) {
      const md = DOC_SOURCES[locale]['mail-templates']
      for (const { name } of NOTIFICATION_TEMPLATE_PLACEHOLDERS) {
        expect(md, `${locale}: {{.${name}}} fehlt`).toContain(`\`{{.${name}}}\``)
      }
    }
  })

  // Interne Links (/docs/<abschnitt>#<anker>) müssen in jeder Sprache auf
  // einen existierenden Abschnitt und eine existierende Überschrift zeigen;
  // die Anker entstehen aus den (sprachabhängigen) Überschriften.
  it('alle internen Doku-Links zeigen auf vorhandene Abschnitte und Anker', () => {
    const broken = []
    for (const locale of Object.keys(DOC_SOURCES)) {
      for (const [id, md] of Object.entries(DOC_SOURCES[locale])) {
        for (const [, target, anchor] of md.matchAll(/\]\(\/docs\/([a-z0-9-]+)(?:#([a-z0-9-]+))?\)/g)) {
          if (!SECTION_IDS.includes(target)) { broken.push(`${locale}/${id}: ${target}`); continue }
          if (!anchor) continue
          const ids = renderMarkdown(resolveSection(target, locale).markdown).headings.map(h => h.id)
          if (!ids.includes(anchor)) broken.push(`${locale}/${id}: ${target}#${anchor}`)
        }
      }
    }
    expect(broken).toEqual([])
  })
})

// Seitenaufbau der Doku. Die Seite setzt den Titel selbst (h1 aus der
// ersten Überschrift der Datei); darunter dürfen nur h2/h3 stehen, ohne Sprung
// (kein h2 -> h4), sonst bricht die Gliederung für Screenreader und das
// Inhaltsverzeichnis. Zeilen in Code-Blöcken (Shell-Kommentare mit #) zählen nicht.
describe('Seitenaufbau der Doku', () => {
  const headingLevels = (md) => {
    let inFence = false
    const levels = []
    for (const line of md.split('\n')) {
      if (/^```/.test(line)) { inFence = !inFence; continue }
      const m = !inFence && line.match(/^(#{1,6})\s+\S/)
      if (m) levels.push(m[1].length)
    }
    return levels
  }

  it('jede Datei beginnt mit genau einer h1 und gliedert danach lückenlos mit h2/h3', () => {
    for (const locale of Object.keys(DOC_SOURCES)) {
      for (const [id, md] of Object.entries(DOC_SOURCES[locale])) {
        const levels = headingLevels(md)
        const where = `${locale}/${id}.md`
        expect(levels[0], `${where}: erste Überschrift`).toBe(1)
        expect(levels.filter(l => l === 1), `${where}: Anzahl h1`).toHaveLength(1)
        expect(Math.max(...levels), `${where}: tiefer als h3`).toBeLessThanOrEqual(3)
        levels.slice(1).forEach((l, i) => {
          const prev = levels[i]
          expect(l - prev, `${where}: Sprung bei der ${i + 2}. Überschrift`).toBeLessThanOrEqual(1)
        })
      }
    }
  })

  it('deutsche und englische Fassung haben dieselbe Gliederung (Zahl der h2/h3)', () => {
    for (const id of SECTION_IDS) {
      const count = (loc, level) => headingLevels(DOC_SOURCES[loc][id]).filter(l => l === level).length
      expect(count('en', 2), `${id}: h2`).toBe(count('de', 2))
      expect(count('en', 3), `${id}: h3`).toBe(count('de', 3))
    }
  })

  it('beide Sprachen haben gleich viele Tabellen und Codeblöcke', () => {
    const tables = (md) => (md.match(/^\|[-| :]+\|$/gm) || []).length
    const fences = (md) => (md.match(/^```/gm) || []).length
    for (const id of SECTION_IDS) {
      expect(tables(DOC_SOURCES.en[id]), `${id}: Tabellen`).toBe(tables(DOC_SOURCES.de[id]))
      expect(fences(DOC_SOURCES.en[id]), `${id}: Codeblöcke`).toBe(fences(DOC_SOURCES.de[id]))
    }
  })

  it('enthält keine Hex-Farben außer in Beispielen mit Code-Auszeichnung', () => {
    for (const locale of Object.keys(DOC_SOURCES)) {
      for (const [id, md] of Object.entries(DOC_SOURCES[locale])) {
        const withoutCode = md.replace(/```[\s\S]*?```/g, '').replace(/`[^`]*`/g, '')
        expect(withoutCode.match(/#[0-9a-fA-F]{6}\b/g), `${locale}/${id}.md`).toBeNull()
      }
    }
  })
})

// Jede Aussage unten war in der Doku falsch oder veraltet und ist
// inzwischen korrigiert; die Tests halten das fest.
describe('Korrigierte Doku-Aussagen', () => {
  const text = (loc, id) => DOC_SOURCES[loc][id].replace(/\s+/g, ' ')

  it('beschreibt den Admin-Bereich mit Kopfzeile und Reitern, nicht mit einer Seitenleiste', () => {
    for (const locale of Object.keys(DOC_SOURCES)) {
      for (const [id, md] of Object.entries(DOC_SOURCES[locale])) {
        expect(md, `${locale}/${id}.md`).not.toMatch(/Seitenleiste|sidebar/i)
      }
    }
  })

  // Der Editor ist da; die Hilfe darf ihn nicht mehr als „später“ ankündigen.
  it('Formatierte Texte: beschreibt den Editor, nennt ihn nicht mehr „später“', () => {
    expect(text('de', 'formatierte-texte')).toContain('Werkzeugleiste')
    expect(text('de', 'formatierte-texte')).toContain('Vorschau')
    expect(text('de', 'formatierte-texte')).not.toContain('späteren Version')
    expect(text('en', 'formatierte-texte')).toContain('toolbar')
    expect(text('en', 'formatierte-texte')).toContain('Preview')
    expect(text('en', 'formatierte-texte')).not.toContain('later version')
  })

  it('Ankündigungen: Formatierung statt „Markdown wird nicht dargestellt“ und die Ankündigung steht nicht "oben"', () => {
    expect(text('de', 'ankuendigungen')).not.toMatch(/Markdown ist erlaubt|oben auf der Statusseite/)
    expect(text('en', 'ankuendigungen')).not.toMatch(/Markdown is allowed|top of the status page/)
    // Seit der Umstellung werden Nachrichten formatiert (eingeschränktes Markdown); die
    // alte Aussage „Markdown wird nicht dargestellt“ wäre jetzt falsch.
    expect(text('de', 'ankuendigungen')).not.toContain('Markdown wird nicht dargestellt')
    expect(text('en', 'ankuendigungen')).not.toContain('Markdown is not rendered')
    expect(text('de', 'ankuendigungen')).toContain('/docs/formatierte-texte')
    expect(text('en', 'ankuendigungen')).toContain('/docs/formatierte-texte')
  })

  it('Vorfälle: Ein Update übernimmt nur den Status, die Erstmeldung bleibt', () => {
    expect(text('de', 'vorfaelle')).not.toContain('Status und Nachricht in den Vorfall')
    expect(text('en', 'vorfaelle')).not.toContain('status and message into the incident')
    expect(text('de', 'vorfaelle')).toContain('Erstmeldung')
    expect(text('en', 'vorfaelle')).toContain('initial report')
  })

  it('Abonnements: Sprachwahl wird als "Seitenstandard" gespeichert und folgt dem Wechsel der Standardsprache', () => {
    expect(text('de', 'abonnements')).toContain('folgt damit einem späteren Wechsel der Standardsprache')
    expect(text('de', 'abonnements')).not.toContain('wirkt erneut')
    expect(text('en', 'abonnements')).toContain('follows a later change of the page')
    expect(text('en', 'abonnements')).not.toContain('takes effect again')
  })

  it('Abonnements: Ohne Komponentenauswahl gilt "alle", nicht "mindestens eine"', () => {
    expect(text('de', 'abonnements')).not.toContain('(mindestens eine)')
    expect(text('en', 'abonnements')).not.toContain('(at least one)')
    expect(text('de', 'abonnements')).toContain('alle Komponenten')
    expect(text('en', 'abonnements')).toContain('all components')
  })

  it('Rechte: create/update/delete schließen read ein, globale Tabs sind Global Admins vorbehalten', () => {
    expect(text('de', 'benutzer-und-rollen')).toContain('Anlegen, Ändern und Löschen schließen Lesen ein')
    expect(text('en', 'benutzer-und-rollen')).toContain('Create, update and delete include read')
    expect(text('de', 'benutzer-und-rollen')).toContain('Globale Ressourcen')
    expect(text('en', 'benutzer-und-rollen')).toContain('Global resources')
    expect(text('de', 'benutzer-und-rollen')).toContain('`permissions.crud_backup`')
  })

  it('Statusseite: Verfügbarkeit statt "Verlauf der letzten 90 Tage" je Komponente', () => {
    expect(text('de', 'was-ist-statushafen')).not.toContain('Verlauf der letzten 90 Tage')
    expect(text('en', 'was-ist-statushafen')).not.toContain('history for the last 90 days')
  })

  it('API: nennt X-Auth-Token und das Fehlerformat mit code', () => {
    for (const locale of ['de', 'en']) {
      expect(text(locale, 'api-und-webhooks'), locale).toContain('X-Auth-Token')
      expect(text(locale, 'api-und-webhooks'), locale).toContain('"code"')
    }
  })

  it('Webhook: beschreibt den Weg per Kennung', () => {
    for (const locale of ['de', 'en']) {
      expect(text(locale, 'api-und-webhooks'), locale).toContain('/api/webhooks/component-slugs/')
    }
  })

  it('Einrichtungsanleitung für Google- und Provider-SSO steht nicht mehr in den Locale-Dateien (toter Block "sso")', () => {
    expect(de.sso).toBeUndefined()
    expect(en.sso).toBeUndefined()
  })
})

describe('resolveSection', () => {
  const sources = { de: { a: '# A de', b: '# B de' }, en: { a: '# A en' } }

  it('liefert die gewünschte Sprache ohne Hinweis', () => {
    expect(resolveSection('a', 'en', sources)).toEqual({ markdown: '# A en', locale: 'en', fallback: false })
    expect(resolveSection('a', 'de', sources)).toEqual({ markdown: '# A de', locale: 'de', fallback: false })
  })

  it('fällt bei fehlender englischer Seite auf Deutsch mit Hinweis zurück', () => {
    expect(resolveSection('b', 'en', sources)).toEqual({ markdown: '# B de', locale: 'de', fallback: true })
    expect(resolveSection('b', 'fr', sources)).toEqual({ markdown: '# B de', locale: 'de', fallback: true })
  })

  it('liefert null für unbekannte Abschnitte', () => {
    expect(resolveSection('gibt-es-nicht', 'de', sources)).toBeNull()
  })
})

// Der Schweregrad eines offenen Vorfalls bestimmt den angezeigten
// Komponentenstatus (nicht mehr pauschal "Beeinträchtigt"); die Hilfe sagt das.
describe('Doku: offener Vorfall und Komponentenstatus', () => {
  it('Vorfälle und FAQ nennen den Schweregrad statt pauschal "Beeinträchtigt"', () => {
    expect(DOC_SOURCES.de.vorfaelle).toMatch(/mit dem Status seines Schweregrads/)
    expect(DOC_SOURCES.de.vorfaelle).toMatch(/schlimmste Schweregrad/)
    expect(DOC_SOURCES.de.faq).toMatch(/Status seines Schweregrads/)
    expect(DOC_SOURCES.en.vorfaelle).toMatch(/status of its severity/)
    expect(DOC_SOURCES.en.vorfaelle).toMatch(/worst\s+severity/)
    expect(DOC_SOURCES.en.faq).toMatch(/status of its severity/)
    expect(DOC_SOURCES.de.vorfaelle).not.toMatch(/mindestens als „Beeinträchtigt“/)
    expect(DOC_SOURCES.en.vorfaelle).not.toMatch(/at least as "Degraded Performance"/)
    // Nachbesserung: Komponenten-Seite und FAQ-Überschrift nicht mehr pauschal "Beeinträchtigt".
    expect(DOC_SOURCES.de['komponenten-und-tags']).toMatch(/Status seines Schweregrads/)
    expect(DOC_SOURCES.de['komponenten-und-tags']).not.toMatch(/mindestens „Beeinträchtigt“/)
    expect(DOC_SOURCES.en['komponenten-und-tags']).toMatch(/status of its severity/)
    expect(DOC_SOURCES.en['komponenten-und-tags']).not.toMatch(/at least "Degraded Performance"/)
    expect(DOC_SOURCES.de.faq).not.toMatch(/## Warum steht eine Komponente auf „Beeinträchtigt“/)
    expect(DOC_SOURCES.en.faq).not.toMatch(/## Why does a component show "Degraded Performance"/)
  })
})

describe('renderMarkdown', () => {
  it('rendert Überschriften mit Anker-IDs und sammelt h2/h3 für das Inhaltsverzeichnis', () => {
    const { html, headings } = renderMarkdown('# Titel\n\n## Status-Ablauf\n\nText\n\n### Unterpunkt\n\n#### Tief')
    expect(html).toContain('<h1 id="titel">Titel</h1>')
    expect(html).toContain('<h2 id="status-ablauf">Status-Ablauf</h2>')
    expect(headings).toEqual([
      { id: 'status-ablauf', text: 'Status-Ablauf', depth: 2 },
      { id: 'unterpunkt', text: 'Unterpunkt', depth: 3 },
    ])
  })

  it('vergibt einer Überschrift ohne Buchstaben eine Ersatz-ID', () => {
    expect(renderMarkdown('## !!!').headings).toEqual([{ id: 'abschnitt', text: '!!!', depth: 2 }])
  })

  it('vergibt bei doppelten Überschriften eindeutige IDs', () => {
    const { headings } = renderMarkdown('## Felder\n\n## Felder')
    expect(headings.map(h => h.id)).toEqual(['felder', 'felder-1'])
  })

  it('rendert Tabellen, Code und Links', () => {
    const { html } = renderMarkdown('| a | b |\n|---|---|\n| 1 | 2 |\n\n`code`\n\n[Link](/docs/faq)')
    expect(html).toContain('<table>')
    expect(html).toContain('<code>code</code>')
    expect(html).toContain('<a href="/docs/faq">Link</a>')
  })

  // XSS: marked lässt Roh-HTML durch, DOMPurify muss es entfernen.
  it('entfernt Skripte, Event-Handler und javascript:-Links', () => {
    const md = [
      '# Titel',
      '<script>window.__xss = 1</script>',
      '<img src="x" onerror="window.__xss = 2">',
      '[klick](javascript:alert(1))',
      '<a href="#" onclick="alert(1)">a</a>',
      '<iframe src="https://example.com"></iframe>',
    ].join('\n\n')
    const { html } = renderMarkdown(md)
    expect(html).not.toMatch(/<script/i)
    expect(html).not.toMatch(/onerror/i)
    expect(html).not.toMatch(/onclick/i)
    expect(html).not.toMatch(/javascript:/i)
    expect(html).not.toMatch(/<iframe/i)
    expect(html).toContain('<img src="x">')
  })

  it('verkraftet leeren Inhalt', () => {
    expect(renderMarkdown('')).toEqual({ html: '', headings: [] })
    expect(renderMarkdown(undefined)).toEqual({ html: '', headings: [] })
  })
})

describe('Hilfsfunktionen', () => {
  it('byLocaleAndSlug ordnet Dateien nach Sprache und Abschnitt und überspringt fremde Namen', () => {
    expect(byLocaleAndSlug({
      './de/faq.md': 'de-faq',
      './en/faq.md': 'en-faq',
      './de/vorfaelle.md': 'de-v',
      './de/Gross_Schreibung.md': 'x',
      './readme.md': 'y',
    })).toEqual({ de: { faq: 'de-faq', vorfaelle: 'de-v' }, en: { faq: 'en-faq' } })
  })

  it('slugify schreibt Umlaute aus und entfernt Sonderzeichen', () => {
    expect(slugify('Vorfälle & Wartung')).toBe('vorfaelle-wartung')
    expect(slugify('Größe <b>fett</b>')).toBe('groesse-fett')
    expect(slugify('Straße &amp; Weg')).toBe('strasse-weg')
    expect(slugify('  --Ö--  ')).toBe('oe')
  })

  it('extractTitle liefert die erste h1 oder einen leeren Text', () => {
    expect(extractTitle('Intro\n# Titel \n## Sub')).toBe('Titel')
    expect(extractTitle('## nur h2')).toBe('')
    expect(extractTitle(undefined)).toBe('')
  })

  it('stripTitle entfernt nur die erste h1', () => {
    expect(stripTitle('# Titel\n\nText\n\n# Zweite')).toBe('\n\nText\n\n# Zweite')
    expect(stripTitle('## h2\nText')).toBe('## h2\nText')
    expect(stripTitle(undefined)).toBe('')
  })

  it('plainText entfernt Markdown-Syntax und behält Code-Inhalte durchsuchbar', () => {
    const text = plainText('# Titel\n\n**fett** `X-Webhook-Token`\n\n```bash\ncurl -X POST\n```')
    expect(text).toContain('titel')
    expect(text).toContain('fett')
    expect(text).toContain('x webhook token')
    expect(text).toContain('curl')
    expect(text).not.toContain('**')
    expect(plainText(undefined)).toBe('')
  })

  // Review Ein fehlendes Metrik-Verzeichnis bei gesetztem Job ist ein Fehlschlag.
  it('Selbstüberwachung beschreibt das Verhalten bei fehlendem Metrik-Verzeichnis', () => {
    const de = DOC_SOURCES.de.selbstueberwachung
    const en = DOC_SOURCES.en.selbstueberwachung
    expect(de).toContain('Metrik-Verzeichnis')
    expect(de).not.toContain('zeigen die Komponente nicht als gestört')
    expect(en).toContain('metrics directory')
    expect(en).not.toContain('do not show the')
    // Re-Review: alle Audit-Aktionen des Monitors sind dokumentiert, die Frist ist präzise.
    for (const action of ['component_failed', 'incident_opened', 'component_recovered', 'component_released']) {
      expect(de).toContain(`selfmonitor.${action}`)
      expect(en).toContain(`selfmonitor.${action}`)
    }
    expect(de).toContain('25 bis 30 Minuten')
    expect(en).toContain('25 to 30')
  })

  // Seit der Umstellung gibt der Monitor eigene Störungen beim Abschalten frei (releaseAll).
  // verschobene Komponenten werden nicht mehr angefasst.
  it('Selbstüberwachung: Abschalten gibt eigene Störungen frei, verschobene Komponenten bleiben unangetastet', () => {
    const de = DOC_SOURCES.de.selbstueberwachung
    const en = DOC_SOURCES.en.selbstueberwachung
    expect(de).not.toContain('bleiben beim Abschalten bestehen')
    expect(en).not.toContain('remain when you switch it off')
    expect(de).toContain('Beim Abschalten gibt die Überwachung')
    expect(de).toContain('Mailversand')
    expect(en).toContain('When you switch it off, monitoring releases')
    expect(en).toContain('email sending')
    expect(de).toContain('Wird eine überwachte Komponente (z. B. per Datenbankeingriff) einer anderen Statusseite zugeordnet')
    expect(de).not.toContain('Verschiebt ein Admin')
    expect(de).toContain('geht sie zurück auf „in Betrieb“')
    expect(en).toContain('If a monitored component is assigned to another status page (e.g. by a database intervention)')
    expect(en).not.toContain('If an admin moves')
    expect(en).toContain('returns to\n"operational"')
  })
})
