import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { runInThisContext } from 'node:vm'
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest'

// The inspector's agent, internal/devtools/agent.js, is the one piece of the inspector a
// build from source runs in the window's page. It is plain JavaScript the app hands the
// web view, outside the frontend's own sources, so the Go tests can't run it: it runs
// here, in jsdom, as the page would run it.
const agentSource = readFileSync(resolve(import.meta.dirname, '../../../internal/devtools/agent.js'), 'utf8')

// The prefix every message the agent posts starts with (devtools.Prefix).
const prefix = 'devtools:'

interface Message {
  t: string
  id?: number
  ok?: boolean
  value?: unknown
  error?: string
  level?: string
  text?: string
  kind?: string
  target?: string
  where?: string
  url?: string
  view?: string
  element?: Record<string, unknown>
  nearby?: string
  components?: string
  source?: string
  width?: number
}

interface Agent {
  run: (id: number, code: string) => void
  nb: { point: (on?: boolean) => boolean }
  noted: (n: number) => void
}

// posted holds what the agent posted to the app, newest last.
const posted: Message[] = []

const agent = () => (window as unknown as { __appDevtools: Agent }).__appDevtools

// install runs the agent's source in the page, as the app does on every load.
const install = () => {
  runInThisContext(agentSource)
}

// el makes an element with attributes and children, as markup would.
const el = (tag: string, attrs: Record<string, string> = {}, ...children: (Node | string)[]) => {
  const node = document.createElement(tag)

  for (const [name, value] of Object.entries(attrs)) {
    node.setAttribute(name, value)
  }

  node.append(...children)

  return node
}

// latest is the newest message of type t.
const latest = (t: string) => posted.filter((m) => m.t === t).at(-1)

// evaluate runs code as Page.Eval does and waits for its result.
const evaluate = async (code: string) => {
  const id = posted.length + 1000
  agent().run(id, code)

  let result: Message | undefined

  await expect.poll(() => (result = posted.find((m) => m.t === 'result' && m.id === id))).toBeDefined()

  return result as Message
}

// pointLayer is what the agent draws while the user points, or null when they don't.
const pointLayer = () => document.querySelector<HTMLElement>('[data-devtools-point]')

// press presses a key on target as the user would, and reports whether nothing prevented
// it.
const press = (target: EventTarget, init: KeyboardEventInit) =>
  target.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, cancelable: true, ...init }))

// pointKey is ⌥⌘P, which types π.
const pointKey = { code: 'KeyP', key: 'π', altKey: true, metaKey: true }

// component is a component named name, as React's development build keeps it.
const component = (name: string) => Object.defineProperty(() => null, 'name', { value: name })

// devFiber is the fiber React's development build keeps on a button TriageControls
// rendered: its element's stack starts in React's own module, then in the component's
// file. Above it are a provider, which has no name, ItemRow under memo, a list item, and
// InboxView under forwardRef.
const devFiber = {
  type: 'button',
  _debugOwner: null,
  _debugStack: {
    stack:
      'react-stack-top-frame\n' +
      'jsxDEV@wails://localhost/node_modules/.vite/deps/react_jsx-dev-runtime.js:250:12\n' +
      'TriageControls@wails://localhost/src/components/TriageControls.tsx?t=1700:120:9',
  },
  return: {
    type: component('TriageControls'),
    return: {
      type: { _context: {} },
      return: {
        type: { type: component('ItemRow') },
        return: { type: 'li', return: { type: { render: component('InboxView') }, return: null } },
      },
    },
  },
}

// noteOn points at el, writes words in the note's box, and presses Enter; it returns the
// note the agent then posts.
const noteOn = async (el: HTMLElement, words: string) => {
  const before = posted.length

  agent().nb.point(true)
  el.click()

  const field = pointLayer()?.querySelector('textarea') as HTMLTextAreaElement
  field.value = words
  press(field, { key: 'Enter' })

  let note: Message | undefined

  await expect.poll(() => (note = posted.slice(before).find((m) => m.t === 'note'))).toBeDefined()

  return note as Message
}

const originalConsole = { ...console }
const innerText = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'innerText')

beforeAll(() => {
  // jsdom lays nothing out, so it has no innerText; the text stands in, but for what a
  // page would not draw: a style sheet's, and what its own style hides.
  Object.defineProperty(HTMLElement.prototype, 'innerText', {
    configurable: true,
    get(this: HTMLElement) {
      const hidden = (el: HTMLElement | null) => {
        for (let at = el; at !== null && at !== this.parentElement; at = at.parentElement) {
          if (at.tagName === 'STYLE' || at.style.visibility === 'hidden' || at.style.display === 'none') {
            return true
          }
        }

        return false
      }

      const walker = document.createTreeWalker(this, NodeFilter.SHOW_TEXT)
      let text = ''

      for (let node = walker.nextNode(); node !== null; node = walker.nextNode()) {
        if (!hidden(node.parentElement)) {
          text += node.nodeValue ?? ''
        }
      }

      return text
    },
  })

  Object.assign(window, {
    webkit: {
      messageHandlers: {
        external: {
          postMessage: (message: string) => {
            if (message.startsWith(prefix)) {
              posted.push(JSON.parse(message.slice(prefix.length)) as Message)
            }
          },
        },
      },
    },
  })

  install()
})

afterAll(() => {
  Object.assign(console, originalConsole)

  if (innerText) {
    Object.defineProperty(HTMLElement.prototype, 'innerText', innerText)
  } else {
    Reflect.deleteProperty(HTMLElement.prototype, 'innerText')
  }
})

beforeEach(() => {
  document.body.replaceChildren(
    el(
      'section',
      { 'aria-label': 'Review requested' },
      el(
        'ul',
        {},
        el(
          'li',
          { 'data-item-id': '7', 'data-rule': 'review_requested' },
          el('button', { 'data-card-title': '' }, 'Fix the cache'),
          el('button', { 'aria-label': 'Snooze for 3 days' }, el('span', {}, 'zz')),
        ),
      ),
    ),
    el('input', { 'aria-label': 'Filter' }),
    el(
      'select',
      { 'aria-label': 'Sort' },
      el('option', { value: 'new' }, 'Newest'),
      el('option', { value: 'old' }, 'Oldest'),
    ),
    el('input', { type: 'checkbox', 'aria-label': 'Compact' }),
  )
})

afterEach(() => {
  agent().nb.point(false)
})

describe('the inspector’s agent', () => {
  it('says hello once, however often it runs', () => {
    expect(posted.filter((m) => m.t === 'hello')).toHaveLength(1)
    expect(latest('hello')?.url).toBe(location.href)

    install()

    expect(posted.filter((m) => m.t === 'hello')).toHaveLength(1)
  })

  it('posts the page’s console, and its errors nothing caught', () => {
    console.warn('slow', { ms: 900 })
    expect(latest('console')).toMatchObject({ level: 'warn', text: 'slow {"ms":900}' })

    console.error(new TypeError('nope'))
    expect(latest('console')?.text).toMatch(/^TypeError: nope\n/)

    window.dispatchEvent(new ErrorEvent('error', { message: 'bad', filename: 'app.js', lineno: 3 }))
    expect(latest('console')).toMatchObject({ level: 'error', text: 'Uncaught bad (app.js:3)' })
  })

  it('posts a click on what the user clicked, and where', () => {
    const snooze = document.querySelector('[aria-label="Snooze for 3 days"] span') as HTMLElement
    snooze.click()

    expect(latest('trail')).toEqual({
      t: 'trail',
      kind: 'click',
      target: 'button “Snooze for 3 days”',
      where: 'card 7 “Fix the cache” · in Review requested',
    })
  })

  it('names a dialog by its label, or plainly when it has none', () => {
    const named = el('div', { role: 'dialog', 'aria-label': 'Launch options' }, el('button', {}, 'Open'))
    const unnamed = el('dialog', { open: '' }, el('button', {}, 'Close'))
    document.body.append(named, unnamed)

    named.querySelector('button')?.click()
    expect(latest('trail')).toMatchObject({ target: 'button “Open”', where: 'dialog “Launch options”' })

    unnamed.querySelector('button')?.click()
    expect(latest('trail')).toMatchObject({ target: 'button “Close”', where: 'dialog' })
  })

  it('posts typing once per field, never what was typed, and a command key with where it went', () => {
    const before = posted.length
    const filter = document.querySelector('input[aria-label="Filter"]') as HTMLInputElement

    for (const key of ['s', 'e', 'c', 'r', 'e', 't']) {
      filter.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true }))
    }

    filter.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    document.body.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', metaKey: true, bubbles: true }))
    document.body.dispatchEvent(new KeyboardEvent('keydown', { key: 'Shift', bubbles: true }))

    const trail = posted.slice(before).filter((m) => m.t === 'trail')
    expect(trail).toEqual([
      { t: 'trail', kind: 'type', target: 'input “Filter”', where: '' },
      { t: 'trail', kind: 'key', target: 'Escape', where: 'input “Filter”' },
      { t: 'trail', kind: 'key', target: '⌘k', where: 'body' },
    ])
    expect(JSON.stringify(trail)).not.toContain('secret')
  })

  it('posts a choice changed', () => {
    const sort = document.querySelector('select') as HTMLSelectElement
    sort.value = 'old'
    sort.dispatchEvent(new Event('change', { bubbles: true }))
    expect(latest('trail')).toMatchObject({ kind: 'choose', target: 'select “Sort” → Oldest' })

    const compact = document.querySelector('input[type="checkbox"]') as HTMLInputElement
    compact.click()
    expect(latest('trail')).toMatchObject({ kind: 'toggle', target: 'input “Compact” on' })
  })

  it('runs an expression, or statements, and posts the value', async () => {
    expect(await evaluate('1 + 1')).toMatchObject({ ok: true, value: 2 })
    expect(await evaluate('const a = 2\nreturn a * 3')).toMatchObject({ ok: true, value: 6 })
    expect(await evaluate('new Promise((resolve) => setTimeout(() => resolve("later"), 5))')).toMatchObject({
      value: 'later',
    })
    expect(await evaluate('undefined')).toMatchObject({ ok: true, value: null })
  })

  it('posts what a script threw, or why it can’t run', async () => {
    expect(await evaluate('throw new Error("nope")')).toMatchObject({
      ok: false,
      error: expect.stringMatching(/^Error: nope/) as string,
    })
    expect(await evaluate('return )(')).toMatchObject({
      ok: false,
      error: expect.stringMatching(/^SyntaxError/) as string,
    })
  })

  it('carries any value back as JSON can hold it', async () => {
    const result = await evaluate(
      'const loop = { name: "loop" }; loop.self = loop; return { loop, map: new Map([["a", 1]]), set: new Set([1]), ' +
        'big: 5n, fn: function named() {}, nan: NaN, when: new Date(0), error: new RangeError("far"), el: nb.$("li") }',
    )

    expect(result.value).toMatchObject({
      loop: { name: 'loop', self: '(itself)' },
      map: [['a', 1]],
      set: [1],
      big: '5n',
      fn: 'ƒ named()',
      nan: 'NaN',
      when: '1970-01-01T00:00:00.000Z',
      error: { error: 'RangeError', message: 'far' },
      el: { tag: 'li', marks: { 'data-item-id': '7', 'data-rule': 'review_requested' }, children: 2 },
    })
    expect((result.value as { el: object }).el).not.toHaveProperty('id', 'a field it has no value for is left out')
  })

  it('describes the inbox’s cards', async () => {
    const result = await evaluate('nb.cards()')

    expect(result.value).toEqual([
      expect.objectContaining({
        id: 7,
        title: 'Fix the cache',
        list: 'Review requested',
        rule: 'review_requested',
        busy: false,
        detailsOpen: false,
        drawn: null,
      }),
    ])
  })

  it('finds a control by selector or by its name, and clicks it', async () => {
    let clicked = 0
    document.querySelector('[aria-label="Snooze for 3 days"]')?.addEventListener('click', () => (clicked += 1))

    expect(await evaluate('nb.click("Snooze for 3 days").name')).toMatchObject({ value: 'Snooze for 3 days' })
    expect(await evaluate('nb.click("snooze").tag')).toMatchObject({ value: 'button' })
    expect(await evaluate('nb.click("[data-item-id] button + button").tag')).toMatchObject({ value: 'button' })
    expect(clicked).toBe(3)

    expect(await evaluate('nb.click("Nothing like it")')).toMatchObject({
      ok: false,
      error: expect.stringContaining('nothing matches "Nothing like it"') as string,
    })
  })

  it('types into a field as the user would, and presses keys', async () => {
    const filter = document.querySelector('input[aria-label="Filter"]') as HTMLInputElement
    const typed: string[] = []
    filter.addEventListener('input', () => typed.push(filter.value))

    await evaluate('nb.type("Filter", "repo:acme")')
    expect(typed).toEqual(['repo:acme'])

    const keys: string[] = []
    filter.addEventListener('keyup', (e) => keys.push(e.key))
    expect(await evaluate('nb.press("Enter", "Filter")')).toMatchObject({ value: 'Enter' })
    expect(keys).toEqual(['Enter'])
  })

  it('describes elements, their styles and HTML when asked, and their text', async () => {
    const result = await evaluate('nb.dom("button", { styles: true, html: true, limit: 1 })')
    const [button] = result.value as { tag: string; styles: Record<string, string>; html: string }[]

    expect(button?.tag).toBe('button')
    expect(button?.styles).toHaveProperty('display')
    expect(button?.html).toBe('<button data-card-title="">Fix the cache</button>')
    expect(await evaluate('nb.text("[data-card-title]")')).toMatchObject({ value: 'Fix the cache' })
    expect(await evaluate('nb.describe("Sort").name')).toMatchObject({ value: 'Sort' })
  })

  it('waits for what isn’t there yet, for a while', async () => {
    setTimeout(() => {
      document.body.append(el('p', { id: 'late' }, 'here'))
    }, 20)

    expect(await evaluate('(await nb.waitFor("#late", 2000)).id')).toMatchObject({ value: 'late' })
    expect(await evaluate('nb.waitFor("#never", 60)')).toMatchObject({
      ok: false,
      error: expect.stringContaining('timed out waiting for #never') as string,
    })
    expect(await evaluate('nb.waitFor(() => 42)')).toMatchObject({ value: 42 })
  })

  it('names a dialog by the heading its aria-labelledby names', () => {
    const sheet = el(
      'dialog',
      { open: '', 'aria-labelledby': 'sheet-title' },
      el('h2', { id: 'sheet-title' }, 'Fix the cache'),
      el('button', {}, 'Merge'),
    )
    document.body.append(sheet)

    sheet.querySelector('button')?.click()
    expect(latest('trail')).toMatchObject({ target: 'button “Merge”', where: 'dialog “Fix the cache”' })
  })
})

describe('pointing at an element', () => {
  it('starts and ends on ⌥⌘P, which the page never hears', () => {
    const heard: string[] = []
    const listening = new AbortController()
    document.addEventListener('keydown', (e) => heard.push(e.key), { capture: true, signal: listening.signal })

    expect(press(document.body, pointKey)).toBe(false)
    expect(pointLayer()?.parentElement).toBe(document.body)
    expect(pointLayer()?.textContent).toContain('Pointing: click what you mean')
    expect(latest('trail')).toMatchObject({ kind: 'point', target: 'on' })

    press(document.body, pointKey)
    expect(pointLayer()).toBeNull()
    expect(latest('trail')).toMatchObject({ kind: 'point', target: 'off' })

    listening.abort()
    expect(heard).toEqual([])
  })

  it('draws inside the open dialog the pointer is over, which a modal leaves live', () => {
    const merge = el('button', {}, 'Merge')
    const sheet = el('dialog', { open: '' }, merge)
    document.body.append(sheet)

    expect(agent().nb.point(true)).toBe(true)
    expect(pointLayer()?.parentElement).toBe(document.body)

    merge.dispatchEvent(new PointerEvent('pointermove', { bubbles: true }))
    expect(pointLayer()?.parentElement).toBe(sheet)
    expect(pointLayer()?.textContent).toContain('button “Merge”')

    expect(agent().nb.point(false)).toBe(false)
    expect(pointLayer()).toBeNull()
  })

  it('shows what it draws as a popover where the page has them', () => {
    const shown: Element[] = []
    Object.assign(HTMLElement.prototype, {
      showPopover(this: HTMLElement) {
        shown.push(this)
      },
      hidePopover() {
        // Nothing shows in jsdom.
      },
    })

    try {
      const merge = el('button', {}, 'Merge')
      const sheet = el('dialog', { open: '' }, merge)
      document.body.append(sheet)
      merge.focus()

      agent().nb.point(true)

      expect(pointLayer()?.parentElement).toBe(sheet)
      expect(pointLayer()?.getAttribute('popover')).toBe('manual')
      expect(shown).toEqual([pointLayer()])
    } finally {
      // The layer is the agent's for good: as a popover never shown it would take no
      // focus in the tests that follow.
      pointLayer()?.removeAttribute('popover')
      Reflect.deleteProperty(HTMLElement.prototype, 'showPopover')
      Reflect.deleteProperty(HTMLElement.prototype, 'hidePopover')
    }
  })

  it('takes a click for itself, which the app never hears, and opens a note on what was clicked', () => {
    const snooze = document.querySelector('[aria-label="Snooze for 3 days"]') as HTMLElement
    const heard: string[] = []

    for (const type of ['mousedown', 'click']) {
      snooze.addEventListener(type, () => heard.push(type))
    }

    const before = posted.length

    agent().nb.point(true)
    expect(snooze.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, cancelable: true }))).toBe(false)
    snooze.click()

    expect(heard).toEqual([])
    expect(posted.slice(before).filter((m) => m.kind === 'click')).toEqual([])
    expect(document.activeElement).toBe(pointLayer()?.querySelector('textarea'))
    expect(pointLayer()?.textContent).toContain('Note on button “Snooze for 3 days”')

    // A click in the note's box is the box's.
    pointLayer()?.querySelector('textarea')?.click()
    expect(heard).toEqual([])
    expect(pointLayer()?.textContent).toContain('Note on button “Snooze for 3 days”')
  })

  it('keeps the keys typed in the note, and focus moving to it and back, from the app', () => {
    const filter = document.querySelector('input[aria-label="Filter"]') as HTMLInputElement
    filter.focus()

    const heard: string[] = []
    const listening = new AbortController()

    for (const type of ['keydown', 'keyup', 'input', 'focusin', 'focusout']) {
      document.addEventListener(type, () => heard.push(type), { capture: true, signal: listening.signal })
    }

    agent().nb.point(true)
    ;(document.querySelector('select') as HTMLElement).click()

    const field = pointLayer()?.querySelector('textarea') as HTMLTextAreaElement
    expect(document.activeElement).toBe(field)

    for (const k of ['s', 'e', 'c']) {
      press(field, { key: k })
      field.dispatchEvent(new KeyboardEvent('keyup', { key: k, bubbles: true }))
    }

    field.value = 'sec'
    field.dispatchEvent(new Event('input', { bubbles: true }))
    expect(press(field, { key: 'Tab' }), 'Tab stays in the box').toBe(false)
    expect(press(field, { key: 'Escape' })).toBe(false)

    listening.abort()
    expect(heard).toEqual([])
    expect(document.activeElement, 'focus goes back where it was').toBe(filter)
    expect(pointLayer()?.querySelector('textarea'), 'Escape drops the note').toBeNull()
    expect(pointLayer(), 'and leaves pointing on').not.toBeNull()
  })

  it('ends on Escape, after dropping the note being written, and leaves a dialog open', () => {
    const merge = el('button', {}, 'Merge')
    const sheet = el('dialog', { open: '' }, merge)
    document.body.append(sheet)

    // useModalDialog listens so, and closes its dialog on an Escape nothing prevented.
    const heard: string[] = []
    const listening = new AbortController()
    document.addEventListener('keydown', (e) => heard.push(e.key), { capture: true, signal: listening.signal })

    agent().nb.point(true)
    merge.click()
    expect(pointLayer()?.querySelector('textarea')).not.toBeNull()

    expect(press(merge, { key: 'Escape' })).toBe(false)
    expect(pointLayer()?.querySelector('textarea')).toBeNull()
    expect(pointLayer()).not.toBeNull()

    expect(press(merge, { key: 'Escape' })).toBe(false)
    expect(pointLayer()).toBeNull()

    listening.abort()
    expect(heard).toEqual([])
    expect(sheet.hasAttribute('open')).toBe(true)
  })

  it('names the element and its components as the pointer moves over it', () => {
    const snooze = document.querySelector('[aria-label="Snooze for 3 days"]') as HTMLElement
    Object.assign(snooze, { __reactFiber$test: devFiber })

    agent().nb.point(true)
    snooze.dispatchEvent(new PointerEvent('pointermove', { bubbles: true }))

    expect(pointLayer()?.textContent).toContain('button “Snooze for 3 days” · InboxView › ItemRow › TriageControls')
  })

  it('posts a note with what the user said, what the element is, and what React made it of', async () => {
    document.body.prepend(
      el(
        'nav',
        { 'aria-label': 'Views' },
        el('button', { 'aria-pressed': 'true' }, 'Inbox'),
        el('button', { 'aria-pressed': 'false' }, 'Notifications'),
      ),
    )

    const snooze = document.querySelector('[aria-label="Snooze for 3 days"]') as HTMLElement
    Object.assign(snooze, { __reactFiber$test: devFiber })

    const before = posted.length

    agent().nb.point(true)
    snooze.click()
    press(pointLayer()?.querySelector('textarea') as HTMLElement, { key: 'Enter' })
    expect(pointLayer()?.querySelector('textarea'), 'a note needs words').not.toBeNull()

    const note = await noteOn(snooze, '  Should say for how long  ')

    expect(note).toMatchObject({
      text: 'Should say for how long',
      target: 'button “Snooze for 3 days”',
      where: 'card 7 “Fix the cache” · in Review requested',
      view: 'Inbox',
      element: { tag: 'button', name: 'Snooze for 3 days', marks: { 'aria-label': 'Snooze for 3 days' } },
      nearby: 'Fix the cachezz',
      components: 'InboxView › ItemRow › TriageControls',
      source: 'frontend/src/components/TriageControls.tsx:120',
      width: innerWidth,
    })
    expect(
      posted.slice(before).filter((m) => m.kind === 'type'),
      'the note’s words are never typing on the trail',
    ).toEqual([])

    expect(pointLayer()?.style.visibility, 'hidden for the picture of the window').toBe('hidden')
    agent().noted(4)
    expect(pointLayer()?.style.visibility).toBe('')
    expect(pointLayer()?.textContent).toContain('Note 4 saved')
  })

  it('reads the text around an element in a dialog without its own, which is in the dialog too', async () => {
    const cancel = el('button', {}, 'Cancel')
    const confirm = el('dialog', { open: '', 'aria-label': 'Skip the setup?' }, el('p', {}, 'Skip the setup?'), cancel)
    document.body.append(confirm)

    agent().nb.point(true)
    cancel.dispatchEvent(new PointerEvent('pointermove', { bubbles: true }))
    expect(pointLayer()?.parentElement).toBe(confirm)

    const note = await noteOn(cancel, 'Say what is lost')

    expect(note).toMatchObject({
      target: 'button “Cancel”',
      where: 'dialog “Skip the setup?”',
      element: { name: 'Cancel' },
      nearby: 'Skip the setup?Cancel',
    })
  })

  it('names no components under a production build, which keeps no owners', async () => {
    const snooze = document.querySelector('[aria-label="Snooze for 3 days"]') as HTMLElement
    Object.assign(snooze, { __reactFiber$test: { type: 'button', return: { type: component('a') } } })

    const note = await noteOn(snooze, 'Grey?')

    expect(note.target).toBe('button “Snooze for 3 days”')
    expect(note).not.toHaveProperty('components')
    expect(note).not.toHaveProperty('source')
  })
})
