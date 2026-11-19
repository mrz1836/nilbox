import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { runInThisContext } from 'node:vm'
import { afterAll, beforeAll, beforeEach, describe, expect, it } from 'vitest'

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
}

interface Agent {
  run: (id: number, code: string) => void
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

const originalConsole = { ...console }
const innerText = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'innerText')

beforeAll(() => {
  // jsdom lays nothing out, so it has no innerText; the text stands in.
  Object.defineProperty(HTMLElement.prototype, 'innerText', {
    configurable: true,
    get(this: HTMLElement) {
      return this.textContent
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
})
