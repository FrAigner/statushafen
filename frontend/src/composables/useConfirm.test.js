import { describe, it, expect, afterEach } from 'vitest'
import { confirmDialog, answerConfirm, useConfirmState, setConfirmHandler } from './useConfirm'
import { stubConfirm, resetConfirm } from '../test-support/confirm'

// Promise-API des gemeinsamen Bestätigungsdialogs.
describe('useConfirm', () => {
  afterEach(() => { answerConfirm(false); resetConfirm() })

  it('öffnet den Zustand und löst beim Antworten das Promise auf', async () => {
    const state = useConfirmState()
    const p = confirmDialog({ message: 'Sicher?', title: 'T', confirmLabel: 'Ja', danger: false })
    expect(state).toMatchObject({ open: true, message: 'Sicher?', title: 'T', confirmLabel: 'Ja', danger: false })
    answerConfirm(true)
    expect(await p).toBe(true)
    expect(state.open).toBe(false)
  })

  it('Standardwerte: danger an, kein Titel, keine Beschriftung', () => {
    confirmDialog({ message: 'x' })
    expect(useConfirmState()).toMatchObject({ danger: true, title: '', confirmLabel: '' })
    confirmDialog()
    expect(useConfirmState().message).toBeUndefined()
  })

  it('answerConfirm ohne offenen Dialog ist wirkungslos', () => {
    expect(answerConfirm(true)).toBeNull()
  })

  it('der Test-Helfer antwortet fest und merkt Text und Optionen', async () => {
    const fn = stubConfirm(true)
    expect(await confirmDialog({ message: 'abc', confirmLabel: 'X' })).toBe(true)
    expect(fn).toHaveBeenCalledWith('abc')
    expect(fn.lastOptions).toMatchObject({ confirmLabel: 'X', danger: true })
    stubConfirm(false)
    expect(await confirmDialog({ message: 'abc' })).toBe(false)
    expect(useConfirmState().open).toBe(false)
  })

  it('setConfirmHandler(null) schaltet auf den echten Dialog zurück', () => {
    setConfirmHandler(() => true)
    setConfirmHandler(null)
    confirmDialog({ message: 'echt' })
    expect(useConfirmState().open).toBe(true)
  })
})
