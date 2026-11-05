import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { fieldByLabel } from './fieldByLabel'

const mountHtml = (template) => mount({ template })

describe('fieldByLabel (Stufe 4b)', () => {
  it('findet das Feld über label[for]', () => {
    const w = mountHtml('<div><label for="a">Name</label><input id="a"></div>')
    expect(fieldByLabel(w, 'Name').element.id).toBe('a')
  })

  it('wirft, wenn es das Label nicht gibt', () => {
    const w = mountHtml('<div><label for="a">Name</label><input id="a"></div>')
    expect(() => fieldByLabel(w, 'Anders')).toThrow('Kein Label "Anders" gefunden')
  })

  it('wirft, wenn das Label mit keinem Feld verbunden ist (ohne for und mit totem for)', () => {
    expect(() => fieldByLabel(mountHtml('<div><label>Name</label></div>'), 'Name')).toThrow('mit keinem Feld verbunden')
    expect(() => fieldByLabel(mountHtml('<div><label for="x">Name</label></div>'), 'Name')).toThrow('mit keinem Feld verbunden')
  })
})
