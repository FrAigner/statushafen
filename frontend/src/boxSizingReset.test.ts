import { describe, it, expect } from 'vitest'
import { readFileSync } from 'fs'
import { fileURLToPath } from 'url'
import { dirname, resolve } from 'path'

// Form fields use width: 100% plus padding and a border. Without border-box they end up wider
// than their card and stick out on the right (component form, SSO, status page settings, ...).
const root = resolve(dirname(fileURLToPath(import.meta.url)))
const css = readFileSync(resolve(root, 'style.css'), 'utf-8').replace(/\/\*[\s\S]*?\*\//g, '')

const rules = () => [...css.matchAll(/([^{};]+)\{([^{}]*)\}/g)]
  .map(([, selector, body]) => ({ selectors: selector.split(',').map(s => s.trim()), body }))

describe('form field sizing', () => {
  it('sizes the full-width form fields with border-box', () => {
    const field = rules().find(r => r.selectors.includes('.form-input') && /width:\s*100%/.test(r.body))
    expect(field, 'rule for .form-input with width: 100%').toBeTruthy()
    expect(field!.body).toMatch(/box-sizing:\s*border-box/)
    expect(field!.selectors.some(s => s.startsWith('.form-group input'))).toBe(true)
    expect(field!.selectors.some(s => s.startsWith('.form-group textarea'))).toBe(true)
  })

  it('sizes the FormField controls with border-box', () => {
    const control = rules().find(r => r.selectors.includes('.form-field__control textarea') && /inline-size:\s*100%/.test(r.body))
    expect(control, 'rule for .form-field__control with inline-size: 100%').toBeTruthy()
    expect(control!.body).toMatch(/box-sizing:\s*border-box/)
  })
})
