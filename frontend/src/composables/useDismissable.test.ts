import { describe, it, expect, vi, afterEach } from 'vitest'
import { defineComponent, h, ref, nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { useDismissable } from './useDismissable'

// Test host: a trigger button, a panel and an unrelated element outside both.
function mountHost({ initiallyOpen = true, options }: { initiallyOpen?: boolean; options?: Parameters<typeof useDismissable>[3] } = {}) {
  const onDismiss = vi.fn()
  const open = ref(initiallyOpen)
  const Host = defineComponent({
    setup() {
      const trigger = ref<Element | null>(null)
      const panel = ref<Element | null>(null)
      useDismissable(open, [trigger, panel], (reason) => {
        onDismiss(reason)
        open.value = false
      }, options)
      return () => h('div', [
        h('button', { ref: trigger, class: 'trigger' }, 'T'),
        h('div', { ref: panel, class: 'panel' }, [h('button', { class: 'item' }, 'I')]),
        h('p', { class: 'outside' }, 'O'),
      ])
    },
  })
  const wrapper = mount(Host, { attachTo: document.body })
  return { wrapper, open, onDismiss }
}

const pointerdown = (el: Element) => el.dispatchEvent(new Event('pointerdown', { bubbles: true }))
const keydown = (el: Element, key: string) => el.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true }))

let current: ReturnType<typeof mountHost> | null | undefined
afterEach(() => current?.wrapper.unmount())

describe('useDismissable', () => {
  it('dismisses on a pointerdown outside the given elements', () => {
    current = mountHost()
    pointerdown(current.wrapper.find('.outside').element)
    expect(current.onDismiss).toHaveBeenCalledWith('outside')
    expect(current.open.value).toBe(false)
  })

  it('treats a pointerdown on document.body as outside', () => {
    current = mountHost()
    pointerdown(document.body)
    expect(current.onDismiss).toHaveBeenCalledWith('outside')
  })

  it('keeps the popup open on a pointerdown inside the panel or on the trigger', () => {
    current = mountHost()
    pointerdown(current.wrapper.find('.item').element)
    pointerdown(current.wrapper.find('.trigger').element)
    expect(current.onDismiss).not.toHaveBeenCalled()
    expect(current.open.value).toBe(true)
  })

  // Keyboard users: moving focus to another control (e.g. the next header
  // popup) closes this one, focus moving inside keeps it open.
  it('dismisses when focus moves outside, not when it moves inside', () => {
    current = mountHost()
    current.wrapper.find<HTMLButtonElement>('.item').element.focus()
    expect(current.onDismiss).not.toHaveBeenCalled()
    const other = document.createElement('button')
    document.body.appendChild(other)
    other.focus()
    expect(current.onDismiss).toHaveBeenCalledWith('outside')
    other.remove()
  })

  it('dismisses on Escape and prevents the default action', () => {
    current = mountHost()
    const event = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })
    current.wrapper.find('.item').element.dispatchEvent(event)
    expect(current.onDismiss).toHaveBeenCalledWith('escape')
    expect(event.defaultPrevented).toBe(true)
  })

  it('ignores other keys', () => {
    current = mountHost()
    keydown(document.body, 'Enter')
    expect(current.onDismiss).not.toHaveBeenCalled()
  })

  it('leaves Escape alone when escape: false is passed', () => {
    current = mountHost({ options: { escape: false } })
    keydown(document.body, 'Escape')
    expect(current.onDismiss).not.toHaveBeenCalled()
    pointerdown(document.body)
    expect(current.onDismiss).toHaveBeenCalledWith('outside')
  })

  it('does not listen while closed and starts listening when opened', async () => {
    current = mountHost({ initiallyOpen: false })
    pointerdown(document.body)
    keydown(document.body, 'Escape')
    expect(current.onDismiss).not.toHaveBeenCalled()
    current.open.value = true
    await nextTick()
    pointerdown(document.body)
    expect(current.onDismiss).toHaveBeenCalledTimes(1)
  })

  it('removes its document listeners on close and on unmount', async () => {
    const remove = vi.spyOn(document, 'removeEventListener')
    current = mountHost()
    current.open.value = false
    await nextTick()
    expect(remove).toHaveBeenCalledWith('pointerdown', expect.any(Function), true)
    expect(remove).toHaveBeenCalledWith('focusin', expect.any(Function), true)
    expect(remove).toHaveBeenCalledWith('keydown', expect.any(Function), false)
    current.open.value = true
    await nextTick()
    remove.mockClear()
    current.wrapper.unmount()
    expect(remove).toHaveBeenCalledWith('pointerdown', expect.any(Function), true)
    expect(remove).toHaveBeenCalledWith('keydown', expect.any(Function), false)
    // After unmount nothing reacts any more.
    pointerdown(document.body)
    expect(current.onDismiss).not.toHaveBeenCalled()
    remove.mockRestore()
    current = null
  })

  it('accepts component refs and getter functions as inside elements', () => {
    const onDismiss = vi.fn()
    const open = ref(true)
    const Child = defineComponent({ render: () => h('span', { class: 'child' }, 'C') })
    const Host = defineComponent({
      setup() {
        const comp = ref(null)
        const extra = ref(null)
        useDismissable(open, [comp, () => extra.value, ref(null)], onDismiss)
        return () => h('div', [h(Child, { ref: comp }), h('em', { ref: extra, class: 'extra' }, 'E')])
      },
    })
    const wrapper = mount(Host, { attachTo: document.body })
    pointerdown(wrapper.find('.child').element)
    pointerdown(wrapper.find('.extra').element)
    expect(onDismiss).not.toHaveBeenCalled()
    pointerdown(document.body)
    expect(onDismiss).toHaveBeenCalledWith('outside')
    wrapper.unmount()
  })
})
