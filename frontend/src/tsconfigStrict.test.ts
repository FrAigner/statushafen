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
// lang="ts", and the app configuration no longer accepts JavaScript at all (allowJs: false).
// Phase 3 migrated the rest: unit tests, test helpers, e2e specs, the Vite plugin and every
// configuration file are TypeScript. Neither configuration accepts JavaScript any more, and no
// .js/.cjs/.mjs/.jsx file may come back anywhere in frontend/ (generated directories aside).
import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync, type Dirent } from 'node:fs'
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

function readRaw(file: string): Record<string, unknown> {
  const { config, error } = ts.readConfigFile(file, ts.sys.readFile)
  if (error) throw new Error(ts.flattenDiagnosticMessageText(error.messageText, '\n'))
  return config
}

// vue-tsc registers .vue as an extra extension; without it TypeScript would not list any SFC.
const VUE_EXTENSION = [{ extension: '.vue', isMixedContent: true, scriptKind: ts.ScriptKind.Deferred }]

function parse(file: string) {
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
function referencedConfigs(): string[] {
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
    const broken: Record<string, unknown> = { project: { type: 'module' }, compileOptions: { strict: true } }
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
      expect(STRICT_FAMILY.filter((o) => options[o as keyof ts.CompilerOptions] === false), name).toEqual([])
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
      expect.arrayContaining(['src/App.test.ts', 'vite.config.ts', 'vitest.config.ts', 'playwright.config.ts']),
    )
    expect(node.rel.some((f) => f.startsWith('e2e/'))).toBe(true)
    expect(node.options.types).toContain('node')
  })

  it('has a typecheck script using vue-tsc, and CI runs it', () => {
    const pkg = JSON.parse(readFileSync(resolve(frontend, 'package.json'), 'utf-8')) as { scripts: Record<string, string> }
    expect(pkg.scripts.typecheck).toMatch(/^vue-tsc\b/)
    const ci = readFileSync(resolve(frontend, '..', '.github', 'workflows', 'ci.yml'), 'utf-8')
    expect(ci).toMatch(/cd frontend && npm run typecheck/)
  })
})

// Generated or installed directories (see .gitignore): their JavaScript is not ours.
const GENERATED_DIRS = new Set([
  'node_modules',
  'dist',
  'dist-ssr',
  'coverage',
  'reports',
  '.stryker-tmp',
  'playwright-report',
  'test-results',
])

// Walks a directory and returns paths relative to the frontend directory.
function walk(dir: string, skip: Set<string> = new Set()): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((e: Dirent) => {
    const path = resolve(dir, e.name)
    if (e.isDirectory()) return skip.has(e.name) ? [] : walk(path, skip)
    return [relative(frontend, path)]
  })
}

const JAVASCRIPT = /\.(js|cjs|mjs|jsx)$/

// JavaScript files that must stay JavaScript, each with the reason. Empty: everything is TypeScript.
const ALLOWED_JAVASCRIPT: string[] = []

describe('TypeScript migration (phase 2)', () => {
  const files = walk(resolve(frontend, 'src'))

  it('leaves no JavaScript application code in src/', () => {
    expect(files.filter((f) => JAVASCRIPT.test(f))).toEqual([])
  })

  it('uses <script setup lang="ts"> in every single-file component', () => {
    const vue = files.filter((f) => f.endsWith('.vue'))
    expect(vue.length).toBeGreaterThan(0)
    const withoutTs = vue.filter((f) => {
      const scripts = readFileSync(resolve(frontend, f), 'utf-8').match(/<script\b[^>]*>/gi) ?? []
      return scripts.length === 0 || scripts.some((tag) => !/\blang="ts"/.test(tag))
    })
    expect(withoutTs).toEqual([])
  })

  it('accepts JavaScript in no configuration, neither the app nor the Node one (tests, configs)', () => {
    for (const file of referencedConfigs()) {
      const { options, rel } = parse(file)
      const name = relative(frontend, file)
      expect(options.allowJs, name).toBe(false)
      expect(rel.filter((f) => JAVASCRIPT.test(f)), name).toEqual([])
    }
  })
})

describe('TypeScript migration (phase 3)', () => {
  const files = walk(frontend, GENERATED_DIRS)

  it('leaves no JavaScript file anywhere in frontend/ (tests, e2e, configuration)', () => {
    expect(files.filter((f) => JAVASCRIPT.test(f) && !ALLOWED_JAVASCRIPT.includes(f))).toEqual([])
  })

  it('recognises every JavaScript extension and skips only generated directories', () => {
    expect(['a.js', 'b.cjs', 'c.mjs', 'd.jsx'].every((f) => JAVASCRIPT.test(f))).toBe(true)
    expect(['a.ts', 'b.json', 'c.vue', 'd.d.ts'].some((f) => JAVASCRIPT.test(f))).toBe(false)
    expect(files.some((f) => f.startsWith('node_modules/'))).toBe(false)
    expect(files).toEqual(expect.arrayContaining(['e2e/rowMenu.ts', 'vite-plugins/cloudflareRocketLoaderExclude.ts']))
  })

  it('has TypeScript configuration files for Vite, Vitest, Playwright and ESLint', () => {
    expect(files).toEqual(
      expect.arrayContaining(['vite.config.ts', 'vitest.config.ts', 'playwright.config.ts', 'eslint.config.ts']),
    )
    expect(files).not.toContain('.eslintrc.cjs')
  })

  it('type-checks unit tests, e2e specs and configuration in the Node configuration', () => {
    const node = parse(resolve(frontend, 'tsconfig.node.json'))
    expect(node.rel).toEqual(
      expect.arrayContaining([
        'src/tsconfigStrict.test.ts',
        'e2e/a11y.spec.ts',
        'e2e/rowMenu.ts',
        'vite-plugins/cloudflareRocketLoaderExclude.ts',
        'eslint.config.ts',
      ]),
    )
  })
})
