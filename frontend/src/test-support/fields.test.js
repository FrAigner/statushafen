import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { fieldByLabel, hasLabel, chooseQuickStatus } from './fields.js'

describe('test-support/fields', () => {
  const wrapper = mount({ template: '<div><label for="a">Name</label><input id="a"></div>' })

  it('findet ein Feld über den Label-Text und meldet fehlende Labels', () => {
    expect(fieldByLabel(wrapper, 'Name').element.id).toBe('a')
    expect(hasLabel(wrapper, 'Name')).toBe(true)
    expect(hasLabel(wrapper, 'Fehlt')).toBe(false)
    expect(() => fieldByLabel(wrapper, 'Fehlt')).toThrow('Kein Label "Fehlt" gefunden')
  })

  it('chooseQuickStatus wählt eine Option und meldet eine unbekannte', async () => {
    let chosen = null
    const qs = mount({
      template: '<div class="quick-status"><button>x</button><ul><li role="option" @click="pick">Betrieb</li></ul></div>',
      methods: { pick() { chosen = 'Betrieb' } },
    })
    await chooseQuickStatus(qs.find('button'), 'Betrieb')
    expect(chosen).toBe('Betrieb')
    await expect(chooseQuickStatus(qs.find('button'), 'Nope')).rejects.toThrow('Keine Option "Nope"')
  })
})
