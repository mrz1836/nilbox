import { describe, expect, it } from 'vitest'

import { isFindKey, isTyping } from './keys'

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
