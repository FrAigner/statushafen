import { describe, it, expect } from 'vitest'
import { createRouter, createMemoryHistory } from 'vue-router'
import { safeReturnTo } from './safeReturnTo.js'

// returnTo kommt aus der Query und ist damit Angreifer-Eingabe.
const router = createRouter({
  history: createMemoryHistory(),
  routes: [
    { path: '/admin', component: { template: '<div/>' } },
    { path: '/admin/incidents', component: { template: '<div/>' } },
    { path: '/invitation', component: { template: '<div/>' } },
    { path: '/:slug', component: { template: '<div/>' } }, // wie die öffentliche Statusseite
  ],
})
const FALLBACK = '/admin'

// Die Angriffsmuster; jedes muss auf den Fallback fallen.
const ANGRIFFE = {
  'absolute URL https': 'https://angreifer.example/phish',
  'absolute URL http': 'http://angreifer.example',
  'protokoll-relativ': '//angreifer.example',
  'protokoll-relativ mit Pfad': '//angreifer.example/admin',
  'Slash + Backslash': '/\\angreifer.example',
  'doppelter Backslash': '\\\\angreifer.example',
  'Backslash im Pfad': '/admin\\..\\x',
  'javascript:': 'javascript:alert(1)',
  'JavaScript: gemischt': 'JaVaScRiPt:alert(1)',
  'data:': 'data:text/html,<script>alert(1)</script>',
  'kodiertes //': '%2F%2Fangreifer.example',
  'Slash + kodierter Slash': '/%2F/angreifer.example',
  'Slash + kodierter Backslash': '/%5Cangreifer.example',
  'doppelt kodiert': '/%252F/x',
  'kaputte Kodierung': '/%E0%A4%A',
  'führendes Leerzeichen': ' //angreifer.example',
  'Leerzeichen im Pfad': '/admin x',
  Tab: '/\t/angreifer.example',
  Zeilenumbruch: '/admin\n//angreifer.example',
  Wagenruecklauf: '/admin\r',
  Nullbyte: '/admin\u0000',
  'Steuerzeichen C1': '/admin\u0085',
  'Unicode-Zeilentrenner': '/admin x',
  'Schema ohne Slash': 'angreifer.example/admin',
  'relativer Pfad': 'admin',
  'sehr lang': '/' + 'a'.repeat(5000),
  leer: '',
  'Slash + Backslash allein': '/\\',
  undefined: undefined,
  null: null,
  Array: ['/admin', '/admin/incidents'],
  'Array mit Angriff': ['https://angreifer.example', '/admin'],
  Objekt: { toString: () => '/admin' },
  Zahl: 42,
}

describe('safeReturnTo', () => {
  for (const [name, wert] of Object.entries(ANGRIFFE)) {
    it(`weist "${name}" ab und liefert den Fallback`, () => {
      expect(safeReturnTo(wert, router, FALLBACK)).toBe(FALLBACK)
    })
  }

  it.each([
    ['/admin', '/admin'],
    ['/invitation', '/invitation'],
    ['/admin/incidents', '/admin/incidents'],
    ['/admin/incidents?x=1', '/admin/incidents?x=1'],
    ['/admin/incidents?x=1#z', '/admin/incidents?x=1#z'],
    ['/invitation?token=abc123', '/invitation?token=abc123'],
    ['/meine-seite', '/meine-seite'],
  ])('lässt den internen Pfad %s zu', (eingabe, erwartet) => {
    expect(safeReturnTo(eingabe, router, FALLBACK)).toBe(erwartet)
  })

  it('lässt kodierte Zeichen in der Query zu (Einladungs-Token)', () => {
    expect(safeReturnTo('/invitation?token=a%2Fb', router, FALLBACK)).toBe('/invitation?token=a%2Fb')
  })

  it('weist einen unbekannten Pfad mit mehreren Segmenten ab (keine Route)', () => {
    expect(safeReturnTo('/gibt/es/nicht', router, FALLBACK)).toBe(FALLBACK)
  })

  // Die Stringprüfung darf sich nicht auf den Router-Abgleich verlassen: eine Catch-all-Route
  // würde jede Adresse als "bekannte Route" durchwinken.
  it('weist //host und /%2F/host auch bei einer Catch-all-Route allein per Stringprüfung ab', () => {
    const offen = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div/>' } }],
    })
    expect(safeReturnTo('//angreifer.example', offen, FALLBACK)).toBe(FALLBACK)
    expect(safeReturnTo('/%2F/angreifer.example', offen, FALLBACK)).toBe(FALLBACK)
    expect(safeReturnTo('/egal/wohin', offen, FALLBACK)).toBe('/egal/wohin')
  })

  it('nutzt ohne Angabe den Fallback "/admin"', () => {
    expect(safeReturnTo('https://angreifer.example', router)).toBe('/admin')
  })

  it('akzeptiert genau die Maximallänge, aber nicht ein Zeichen mehr', () => {
    const ok = '/admin?q=' + 'a'.repeat(2048 - '/admin?q='.length)
    expect(ok).toHaveLength(2048)
    expect(safeReturnTo(ok, router, FALLBACK)).toBe(ok)
    expect(safeReturnTo(ok + 'a', router, FALLBACK)).toBe(FALLBACK)
  })

  // Gegenprobe: Die Matrix muss nachweislich anschlagen. Ein ungefiltertes Durchreichen
  // (reine Testfunktion) darf sie NICHT bestehen, safeReturnTo schon; eine leere Matrix
  // wäre trivial "bestanden" und wird deshalb ausgeschlossen.
  const matrixBesteht = (pruefer) =>
    Object.values(ANGRIFFE).every((wert) => pruefer(wert, router, FALLBACK) === FALLBACK)

  it('Gegenprobe: die Matrix ist nicht leer', () => {
    expect(Object.keys(ANGRIFFE).length).toBeGreaterThan(30)
  })

  it('Gegenprobe: ein Durchreicher besteht die Angriffsmatrix nicht, safeReturnTo schon', () => {
    const durchreicher = (raw, _router, fallback) => raw || fallback
    expect(matrixBesteht(durchreicher)).toBe(false)
    expect(matrixBesteht(safeReturnTo)).toBe(true)
  })
})
