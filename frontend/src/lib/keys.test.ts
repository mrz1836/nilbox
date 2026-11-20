import { renderHook } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { isFindKey, isPreviewKey, isTyping, useKeyDown } from './keys'

// keydownOn is a key event dispatched at target, as the document's listeners see it.
function keydownOn(target: EventTarget, init: KeyboardEventInit = {}): KeyboardEvent {
  const seen: KeyboardEvent[] = []
  const listen = (e: KeyboardEvent) => {
    seen.push(e)
  }

  document.addEventListener('keydown', listen)
  target.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, ...init }))
  document.removeEventListener('keydown', listen)

  const [event] = seen

  if (event === undefined) {
    throw new Error('the event never reached the document')
  }

  return event
}

// place puts an element into the document: a tag, with a type for an input.
function place(tag: string, type = ''): HTMLElement {
  const el = document.createElement(tag)

  if (type !== '') {
    el.setAttribute('type', type)
  }

  document.body.replaceChildren(el)

  return el
}

describe('isTyping', () => {
  it.each([
    ['input', 'text', true],
    ['input', 'search', true],
    ['textarea', '', true],
    ['input', 'checkbox', false],
    ['input', 'radio', false],
    ['button', 'button', false],
    ['div', '', false],
  ])('reads a key in a %s %s', (tag, type, want) => {
    expect(isTyping(keydownOn(place(tag, type)))).toBe(want)
  })

  it('counts any control in a form as typing', () => {
    const form = place('form')
    const button = document.createElement('button')

    form.append(button)

    expect(isTyping(keydownOn(button))).toBe(true)
  })

  it('counts a key mid-composition as typing', () => {
    expect(isTyping(keydownOn(place('div'), { isComposing: true }))).toBe(true)
  })

  it('reads a key sent to the document itself', () => {
    expect(isTyping(keydownOn(document))).toBe(false)
  })
})

describe('isFindKey', () => {
  it.each<[KeyboardEventInit, boolean]>([
    [{ key: 'f', metaKey: true }, true],
    [{ key: 'F', metaKey: true }, true],
    [{ key: 'f' }, false],
    [{ key: 'f', metaKey: true, ctrlKey: true }, false],
    [{ key: 'f', metaKey: true, altKey: true }, false],
    [{ key: 'f', metaKey: true, shiftKey: true }, false],
    [{ key: 'g', metaKey: true }, false],
  ])('reads %j', (init, want) => {
    expect(isFindKey(new KeyboardEvent('keydown', init))).toBe(want)
  })
})

describe('isPreviewKey', () => {
  it.each<[KeyboardEventInit, boolean]>([
    [{ key: 'p', metaKey: true, shiftKey: true }, true],
    [{ key: 'P', metaKey: true, shiftKey: true }, true],
    [{ key: 'p', metaKey: true }, false],
    [{ key: 'p', shiftKey: true }, false],
    [{ key: 'p', metaKey: true, shiftKey: true, ctrlKey: true }, false],
    [{ key: 'p', metaKey: true, shiftKey: true, altKey: true }, false],
    [{ key: 'o', metaKey: true, shiftKey: true }, false],
  ])('reads %j', (init, want) => {
    expect(isPreviewKey(new KeyboardEvent('keydown', init))).toBe(want)
  })
})

// press sends a key from target, bubbling, as a key pressed with focus there does.
function press(target: EventTarget, key = 'Escape') {
  target.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true }))
}

describe('useKeyDown', () => {
  it('hears each key that bubbles up to the document while mounted', () => {
    const heard = vi.fn()
    const { unmount } = renderHook(() => {
      useKeyDown(heard)
    })

    press(place('div'))

    expect(heard).toHaveBeenCalledOnce()
    expect(heard.mock.lastCall?.[0]).toMatchObject({ key: 'Escape' })

    unmount()
    press(place('div'))

    expect(heard).toHaveBeenCalledOnce()
  })

  it('listens as the key bubbles, so a control that stops it keeps it', () => {
    const heard = vi.fn()
    const el = place('div')

    el.addEventListener('keydown', (e) => {
      e.stopPropagation()
    })
    renderHook(() => {
      useKeyDown(heard)
    })

    press(el)

    expect(heard).not.toHaveBeenCalled()
  })

  it('calls the latest handler rendered, without listening again', () => {
    const added = vi.spyOn(document, 'addEventListener')
    const first = vi.fn()
    const second = vi.fn()
    const { rerender } = renderHook(
      ({ handler }: { handler: (e: KeyboardEvent) => void }) => {
        useKeyDown(handler)
      },
      { initialProps: { handler: first } },
    )

    rerender({ handler: second })
    press(document)

    expect(first).not.toHaveBeenCalled()
    expect(second).toHaveBeenCalledOnce()
    expect(added.mock.calls.filter(([type]) => type === 'keydown')).toHaveLength(1)
  })

  it('listens on the window when asked, which hears a key after the document', () => {
    const order: string[] = []
    const onDocument = vi.spyOn(document, 'addEventListener')
    const { unmount } = renderHook(() => {
      useKeyDown(() => {
        order.push('document')
      })
      useKeyDown(() => {
        order.push('window')
      }, window)
    })

    press(place('div'))

    expect(order).toEqual(['document', 'window'])
    expect(onDocument.mock.calls.filter(([type]) => type === 'keydown')).toHaveLength(1)

    unmount()
    press(place('div'))

    expect(order).toEqual(['document', 'window'])
  })
})
