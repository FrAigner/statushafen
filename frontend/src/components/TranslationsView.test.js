import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import i18n from '../i18n'
import TranslationsView from './TranslationsView.vue'

const base = () => ({
  title: 'Komponenten-Übersetzungen',
  subtitle: 'Komponente: API',
  breadcrumbs: [{ label: 'Acme' }, { label: 'Komponenten-Übersetzungen' }],
  canCreate: true,
  canUpdate: true,
  canDelete: true,
  translations: [
    { ID: 1, LanguageCode: 'de', Title: 'Titel', Description: 'Text', Status: 'confirmed' },
    { ID: 2, LanguageCode: 'fr', Title: 'Titre', Description: 'Texte', Status: 'draft' },
  ],
  languages: [{ Code: 'en', Name: 'English' }],
  contentKey: 'Description',
  contentFormKey: 'description',
  contentColumnLabel: 'Beschreibung',
  contentFieldLabel: 'Beschreibung',
})
const mountView = (props = {}) => mount(TranslationsView, {
  props: { ...base(), ...props },
  global: { plugins: [i18n] },
})

describe('TranslationsView', () => {
  beforeEach(() => { i18n.global.locale.value = 'de' })

  it('meldet confirm und delete mit der ID aus dem Zeilenmenü', async () => {
    const w = mountView()
    const items = w.findAll('tbody tr')[1].findAll('[role="menuitem"]')
    await items[0].trigger('click')
    await items[1].trigger('click')
    expect(w.emitted('confirm')).toEqual([[2]])
    expect(w.emitted('delete')).toEqual([[2]])
  })

  // Nachrichten (richText) bekommen den Editor, Beschreibungen bleiben ein einfaches Feld.
  it('richText: Inhaltsfeld ist der Editor und schreibt ins Formular; ohne richText ein normales textarea', async () => {
    const form = { language_code: 'en', title: 'T', description: '' }
    const w = mountView({ isEditing: true, richText: true, form })
    expect(w.find('.rte textarea').exists()).toBe(true)
    await w.find('.rte textarea').setValue('**x**')
    expect(form.description).toBe('**x**')
    const plain = mountView({ isEditing: true, form: { language_code: 'en', title: 'T', description: '' } })
    expect(plain.find('.rte').exists()).toBe(false)
    expect(plain.find('textarea').exists()).toBe(true)
  })

  it('blendet ohne Schreibrecht Menü und Neu-Knopf aus', () => {
    const w = mountView({ canCreate: false, canUpdate: false, canDelete: false })
    expect(w.find('.row-menu').exists()).toBe(false)
    expect(w.find('th.actions-col').exists()).toBe(false)
    expect(w.text()).not.toContain('Neue Übersetzung')
  })

  it('sendet save erst, wenn alle Pflichtfelder gefüllt sind', async () => {
    const w = mountView({ isEditing: true, form: { language_code: '', title: '', description: '' } })
    await w.find('form').trigger('submit')
    expect(w.emitted('save')).toBeUndefined()
    expect(w.findAll('.form-field__message--error')).toHaveLength(3)

    await w.setProps({ form: { language_code: 'en', title: 'T', description: '  ' } })
    await w.find('form').trigger('submit')
    expect(w.emitted('save')).toBeUndefined()
    expect(w.findAll('.form-field__message--error')).toHaveLength(1)

    await w.setProps({ form: { language_code: 'en', title: 'T', description: 'D' } })
    await w.find('form').trigger('submit')
    expect(w.emitted('save')).toHaveLength(1)
  })

  it('meldet dismiss über das Schließen der Meldung und cancel über Abbrechen und ×', async () => {
    const w = mountView({ alertMsg: 'Hinweis', alertType: 'warning', isEditing: true, form: {} })
    await w.find('.alert-close').trigger('click')
    expect(w.emitted('dismiss')).toHaveLength(1)
    await w.find('.form-panel__close').trigger('click')
    await w.findAll('form button').find(b => b.text() === 'Abbrechen').trigger('click')
    expect(w.emitted('cancel')).toHaveLength(2)
  })

  it('meldet new vom Seitenkopf und blendet den Knopf beim Bearbeiten aus', async () => {
    const w = mountView()
    await w.findAll('button').find(b => b.text().includes('Neue Übersetzung')).trigger('click')
    expect(w.emitted('new')).toHaveLength(1)
    await w.setProps({ isEditing: true })
    expect(w.findAll('.page-head__actions button')).toHaveLength(0)
  })

  // Bestätigen braucht update, Löschen delete - jedes Recht einzeln.
  it('zeigt Bestätigen nur mit update und Löschen nur mit delete', () => {
    const onlyDelete = mountView({ canUpdate: false })
    expect(onlyDelete.findAll('tbody tr')[1].findAll('[role="menuitem"]').map(i => i.text())).toEqual(['Löschen'])
    const onlyUpdate = mountView({ canDelete: false })
    expect(onlyUpdate.findAll('tbody tr')[1].findAll('[role="menuitem"]').map(i => i.text())).toEqual(['Bestätigen'])
    expect(mountView({ canCreate: false }).text()).not.toContain('Neue Übersetzung')
  })
})
