// TypeScript migration, phase 1: guard for the TS configuration.
//
// Before phase 1, tsconfig.json said "compileOptions" instead of "compilerOptions" and had an invalid
// "project" key. TypeScript silently ignores unknown top-level keys, so the file had no effect at all and
// nobody noticed. This test therefore reads the configuration with the TypeScript API itself (like
// vue-tsc does) and requires:
//  - only known top-level keys and no errors while parsing,
//  - tsconfig.json references the app and the Node configuration, and both find their files,
//  - an explicit strict: true (TypeScript 6 defaults to it, but neither an older TypeScript nor a
//    downgrade option may switch it off silently) and no strict sub-option switched off on its own,
//  - `npm run typecheck` exists and runs in CI.
// Phase 2 migrated all application code: src/ has no JavaScript besides tests, every SFC uses
// lang="ts", and the app configuration no longer accepts JavaScript at all (allowJs: false). Tests stay
// JavaScript for now, so only the Node configuration keeps allowJs (without checkJs).
import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, resolve, relative } from 'node:path'
import ts from 'typescript'

const frontend = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const ROOT_CONFIG = resolve(frontend, 'tsconfig.json')

const TOP_LEVEL_KEYS = new Set([
  '$schema',
  'extends',
  'compilerOptions',
  'vueCompilerOptions',
  'include',
  'exclude',
  'files',
  'references',
  'watchOptions',
  'typeAcquisition',
])

// Sub-options of strict: none of them may be false, otherwise strict would only be on in name.
const STRICT_FAMILY = [
  'noImplicitAny',
  'noImplicitThis',
  'strictNullChecks',
  'strictFunctionTypes',
  'strictBindCallApply',
  'strictPropertyInitialization',
  'strictBuiltinIteratorReturn',
  'useUnknownInCatchVariables',
  'alwaysStrict',
]

function readRaw(file) {
  const { config, error } = ts.readConfigFile(file, ts.sys.readFile)
  if (error) throw new Error(ts.flattenDiagnosticMessageText(error.messageText, '\n'))
  return config
}

// vue-tsc registers .vue as an extra extension; without it TypeScript would not list any SFC.
const VUE_EXTENSION = [{ extension: '.vue', isMixedContent: true, scriptKind: ts.ScriptKind.Deferred }]

function parse(file) {
  const parsed = ts.parseJsonConfigFileContent(
    readRaw(file),
    ts.sys,
    dirname(file),
    undefined,
    file,
    undefined,
    VUE_EXTENSION,
  )
  return {
    ...parsed,
    messages: parsed.errors.map((d) => ts.flattenDiagnosticMessageText(d.messageText, '\n')),
    rel: (parsed.fileNames ?? []).map((f) => relative(frontend, f)),
  }
}

// All configurations that vue-tsc -b checks via tsconfig.json (the root itself lists no files).
function referencedConfigs() {
  return (parse(ROOT_CONFIG).projectReferences ?? []).map((r) => ts.resolveProjectReferencePath(r))
}

describe('TypeScript configuration (migration phase 1)', () => {
  it('uses only known top-level keys in every tsconfig file', () => {
    for (const file of [ROOT_CONFIG, ...referencedConfigs()]) {
      const unknown = Object.keys(readRaw(file)).filter((k) => !TOP_LEVEL_KEYS.has(k))
      expect(unknown, relative(frontend, file)).toEqual([])
    }
  })

  it('flags the former "compileOptions" typo as an unknown key', () => {
    const broken = { project: { type: 'module' }, compileOptions: { strict: true } }
    expect(Object.keys(broken).filter((k) => !TOP_LEVEL_KEYS.has(k))).toEqual(['project', 'compileOptions'])
    const parsed = ts.parseJsonConfigFileContent(broken, ts.sys, frontend)
    expect(parsed.options.strict).toBeUndefined()
  })

  it('references the app and the Node configuration', () => {
    const refs = referencedConfigs().map((f) => relative(frontend, f))
    expect(refs).toEqual(expect.arrayContaining(['tsconfig.app.json', 'tsconfig.node.json']))
  })

  it('parses every referenced configuration without errors and with strict: true', () => {
    for (const file of referencedConfigs()) {
      const { options, messages } = parse(file)
      const name = relative(frontend, file)
      expect(messages, name).toEqual([])
      expect(options.strict, name).toBe(true)
      expect(STRICT_FAMILY.filter((o) => options[o] === false), name).toEqual([])
      expect(options.noEmit, name).toBe(true)
    }
  })

  it('puts application code into the app configuration and tests into the Node configuration', () => {
    const app = parse(resolve(frontend, 'tsconfig.app.json'))
    expect(app.rel).toEqual(expect.arrayContaining(['src/main.ts', 'src/App.vue', 'env.d.ts']))
    expect(app.rel.filter((f) => /\.(test|spec)\./.test(f))).toEqual([])
    // Node globals (process, Buffer) do not belong in code that runs in the browser.
    expect(app.options.types).not.toContain('node')

    const node = parse(resolve(frontend, 'tsconfig.node.json'))
    expect(node.rel).toEqual(
      expect.arrayContaining(['src/App.test.js', 'vite.config.js', 'vitest.config.js', 'playwright.config.js']),
    )
    expect(node.rel.some((f) => f.startsWith('e2e/'))).toBe(true)
    expect(node.options.types).toContain('node')
  })

  it('has a typecheck script using vue-tsc, and CI runs it', () => {
    const pkg = JSON.parse(readFileSync(resolve(frontend, 'package.json'), 'utf-8'))
    expect(pkg.scripts.typecheck).toMatch(/^vue-tsc\b/)
    const ci = readFileSync(resolve(frontend, '..', '.github', 'workflows', 'ci.yml'), 'utf-8')
    expect(ci).toMatch(/cd frontend && npm run typecheck/)
  })
})

// Walks src/ and returns paths relative to the frontend directory.
function walk(dir) {
  return readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const path = resolve(dir, e.name)
    return e.isDirectory() ? walk(path) : [relative(frontend, path)]
  })
}

describe('TypeScript migration (phase 2)', () => {
  const files = walk(resolve(frontend, 'src'))

  it('leaves no JavaScript application code in src/', () => {
    expect(files.filter((f) => f.endsWith('.js') && !/\.(test|spec)\.js$/.test(f))).toEqual([])
  })

  it('uses <script setup lang="ts"> in every single-file component', () => {
    const vue = files.filter((f) => f.endsWith('.vue'))
    expect(vue.length).toBeGreaterThan(0)
    const withoutTs = vue.filter((f) => {
      const scripts = readFileSync(resolve(frontend, f), 'utf-8').match(/<script\b[^>]*>/g) ?? []
      return scripts.length === 0 || scripts.some((tag) => !/\blang="ts"/.test(tag))
    })
    expect(withoutTs).toEqual([])
  })

  it('accepts JavaScript only in the Node configuration (tests), never in the app', () => {
    const app = parse(resolve(frontend, 'tsconfig.app.json'))
    expect(app.options.allowJs).toBe(false)
    expect(app.rel.filter((f) => f.endsWith('.js'))).toEqual([])
    const node = parse(resolve(frontend, 'tsconfig.node.json'))
    expect(node.options.allowJs).toBe(true)
    expect(node.options.checkJs).toBe(false)
  })
})
