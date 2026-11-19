// The inspector's agent in the window's page, in a build from source alone
// (internal/devtools). The app runs it on every page load and before each evaluation; it
// installs once. It posts to the app through the web view's message handler, each message
// prefixed as Page.Receive expects: the page describing itself ("hello"), each console line
// and each error nothing caught ("console"), each thing the user does ("trail"), and each
// evaluation's result ("result"). What the user types is never posted, only the field it
// went into. Nothing leaves the Mac: the app keeps it in memory for the inspector's socket.
;(() => {
  'use strict'

  if (window.__appDevtools) {
    return
  }

  const prefix = 'devtools:'
  const handler = window.webkit?.messageHandlers?.external

  const post = (message) => {
    if (!handler) {
      return
    }

    try {
      handler.postMessage(prefix + JSON.stringify(message))
    } catch {
      // A message the web view can't take is dropped.
    }
  }

  const clip = (text, length) => (text.length > length ? `${text.slice(0, length - 1)}…` : text)
  const squash = (text) => String(text ?? '').replace(/\s+/g, ' ').trim()

  // nameOf is what an element is called: its label, its title, its text, or its value.
  const nameOf = (el) =>
    squash(
      el.getAttribute('aria-label') ||
        el.getAttribute('title') ||
        el.innerText ||
        el.getAttribute('placeholder') ||
        (el instanceof HTMLSelectElement ? '' : el.value) ||
        '',
    )

  const rectOf = (el) => {
    const r = el.getBoundingClientRect()

    return { x: Math.round(r.x), y: Math.round(r.y), width: Math.round(r.width), height: Math.round(r.height) }
  }

  const onScreen = (r) => r.width > 0 && r.height > 0 && r.y < innerHeight && r.y + r.height > 0 && r.x < innerWidth && r.x + r.width > 0

  // drawn is whether the web view renders el: false when it is hidden, see-through, or
  // inside an ancestor whose contents content-visibility skips; null where WebKit can't
  // say.
  const drawn = (el) =>
    el?.checkVisibility
      ? el.checkVisibility({ contentVisibilityAuto: true, opacityProperty: true, visibilityProperty: true })
      : null

  const styleKeys = [
    'display',
    'visibility',
    'opacity',
    'contentVisibility',
    'containIntrinsicSize',
    'overflow',
    'position',
    'zIndex',
    'transform',
    'filter',
    'pointerEvents',
    'color',
    'backgroundColor',
  ]

  // marks are an element's data-* and aria-* attributes, and whether it is disabled,
  // hidden, or inert.
  const marks = (el) => {
    const out = {}

    for (const { name, value } of el.attributes) {
      if (name.startsWith('data-') || name.startsWith('aria-') || ['disabled', 'hidden', 'inert'].includes(name)) {
        out[name] = clip(value, 200)
      }
    }

    return Object.keys(out).length > 0 ? out : undefined
  }

  // describe is an element as the inspector shows it: what it is, where the web view
  // lays it out, whether it is on screen and drawn, and, when asked, its computed styles
  // and its HTML.
  const describe = (el, opts = {}) => {
    const rect = rectOf(el)
    const out = {
      tag: el.tagName.toLowerCase(),
      id: el.id || undefined,
      classes: typeof el.className === 'string' && el.className !== '' ? clip(el.className, 300) : undefined,
      role: el.getAttribute('role') || undefined,
      name: clip(nameOf(el), 200) || undefined,
      rect,
      onScreen: onScreen(rect),
      drawn: drawn(el),
      contentsDrawn: el.firstElementChild ? drawn(el.firstElementChild) : undefined,
      marks: marks(el),
      children: el.childElementCount,
    }

    if (opts.styles) {
      const style = getComputedStyle(el)
      out.styles = Object.fromEntries(styleKeys.map((key) => [key, style[key]]))
    }

    if (opts.html) {
      out.html = clip(el.outerHTML, 8000)
    }

    return out
  }

  // safe is value as JSON can carry it: an element as its description, an error as its
  // message and stack, a Map or a Set or a list of nodes as an array, a value inside
  // itself cut short, and its depth and size capped.
  const safe = (value, depth = 0, seen = new WeakSet()) => {
    switch (typeof value) {
      case 'undefined':
        return null
      case 'number':
        return Number.isFinite(value) ? value : String(value)
      case 'string':
        return clip(value, 20000)
      case 'bigint':
        return `${value}n`
      case 'function':
        return `ƒ ${value.name || 'anonymous'}()`
      case 'symbol':
        return value.toString()
      case 'boolean':
        return value
    }

    if (value === null) {
      return null
    }

    if (value instanceof Element) {
      return describe(value)
    }

    if (value instanceof Error) {
      return { error: value.name, message: value.message, stack: clip(value.stack ?? '', 4000) }
    }

    if (value instanceof Date) {
      return value.toISOString()
    }

    if (depth >= 6) {
      return '…'
    }

    if (seen.has(value)) {
      return '(itself)'
    }

    seen.add(value)

    try {
      if (value instanceof Map) {
        return safe([...value.entries()], depth + 1, seen)
      }

      if (value instanceof Set || value instanceof NodeList || value instanceof HTMLCollection || Array.isArray(value)) {
        return [...value].slice(0, 500).map((v) => safe(v, depth + 1, seen))
      }

      const out = {}

      // A field that is undefined is left out, as JSON leaves it out.
      for (const key of Object.keys(value).slice(0, 500)) {
        if (value[key] !== undefined) {
          out[key] = safe(value[key], depth + 1, seen)
        }
      }

      return out
    } finally {
      seen.delete(value)
    }
  }

  const errorText = (error) =>
    error instanceof Error ? `${error.name}: ${error.message}${error.stack ? `\n${error.stack}` : ''}` : String(error)

  const format = (args) =>
    clip(
      args
        .map((arg) => {
          if (typeof arg === 'string') {
            return arg
          }

          if (arg instanceof Error) {
            return errorText(arg)
          }

          try {
            return JSON.stringify(safe(arg))
          } catch {
            return String(arg)
          }
        })
        .join(' '),
      8000,
    )

  // The console: every line the page writes, and every error nothing caught.
  for (const level of ['debug', 'log', 'info', 'warn', 'error']) {
    const original = console[level]

    console[level] = (...args) => {
      post({ t: 'console', level, text: format(args) })
      original.apply(console, args)
    }
  }

  window.addEventListener('error', (e) => {
    post({ t: 'console', level: 'error', text: `Uncaught ${e.error ? errorText(e.error) : e.message} (${e.filename}:${e.lineno})` })
  })

  window.addEventListener('unhandledrejection', (e) => {
    post({ t: 'console', level: 'error', text: `Unhandled rejection: ${format([e.reason])}` })
  })

  // The trail: what the user clicks, the keys they press, the fields they type in (never
  // what they type), and the choices they change, with where in the window.
  const controls =
    'button, a[href], input, select, textarea, summary, label, [role=button], [role=link], [role=menuitem], ' +
    '[role=menuitemradio], [role=menuitemcheckbox], [role=tab], [role=option], [role=checkbox], [role=radio], ' +
    '[role=switch], [role=combobox]'

  const control = (el) => el.closest(controls) ?? el

  // targetText names what the user acted on: a control by its name, anything else by its
  // label or title, or its text when it holds no other element, so a container never reads
  // out the page.
  const targetText = (el) => {
    const plain = !el.matches(controls) && el.childElementCount > 0
    const name = clip(plain ? squash(el.getAttribute('aria-label') || el.getAttribute('title') || '') : nameOf(el), 80)

    return `${el.getAttribute('role') || el.tagName.toLowerCase()}${name === '' ? '' : ` “${name}”`}`
  }

  const where = (el) => {
    const parts = []
    const card = el.closest('[data-item-id]')

    if (card) {
      const title = clip(squash(card.querySelector('[data-card-title]')?.innerText ?? ''), 60)
      parts.push(`card ${card.getAttribute('data-item-id')}${title === '' ? '' : ` “${title}”`}`)
    }

    const dialog = el.closest('[role=dialog], dialog')

    if (dialog) {
      const label = squash(dialog.getAttribute('aria-label') ?? '')
      parts.push(label === '' ? 'dialog' : `dialog “${label}”`)
    }

    const section = el.closest('section[aria-label]')

    if (section) {
      parts.push(`in ${section.getAttribute('aria-label')}`)
    }

    return parts.join(' · ')
  }

  const trail = (kind, el, extra = '') => {
    if (!(el instanceof Element)) {
      return
    }

    const hit = control(el)
    post({ t: 'trail', kind, target: `${targetText(hit)}${extra === '' ? '' : ` ${extra}`}`, where: where(hit) })
  }

  document.addEventListener('click', (e) => trail('click', e.target), true)

  document.addEventListener(
    'change',
    (e) => {
      const el = e.target

      if (el instanceof HTMLSelectElement) {
        trail('choose', el, `→ ${clip(squash(el.selectedOptions[0]?.text ?? el.value), 60)}`)
      } else if (el instanceof HTMLInputElement && (el.type === 'checkbox' || el.type === 'radio')) {
        trail('toggle', el, el.checked ? 'on' : 'off')
      }
    },
    true,
  )

  // typedAt is when the user last typed in each field, so typing reads as one entry.
  const typedAt = new WeakMap()
  const modifierKeys = ['Shift', 'Meta', 'Control', 'Alt', 'CapsLock', 'Fn']

  document.addEventListener(
    'keydown',
    (e) => {
      if (modifierKeys.includes(e.key) || !(e.target instanceof Element)) {
        return
      }

      const field = e.target.closest('input, textarea, [contenteditable=""], [contenteditable=true]')
      const command = e.metaKey || e.ctrlKey || ['Enter', 'Escape', 'Tab'].includes(e.key)

      if (field && !command) {
        if (Date.now() - (typedAt.get(field) ?? 0) > 2000) {
          trail('type', field)
        }

        typedAt.set(field, Date.now())

        return
      }

      const mods = `${e.metaKey ? '⌘' : ''}${e.ctrlKey ? '⌃' : ''}${e.altKey ? '⌥' : ''}${e.shiftKey && e.key.length > 1 ? '⇧' : ''}`
      const on = control(e.target)
      post({ t: 'trail', kind: 'key', target: `${mods}${e.key}`, where: [targetText(on), where(on)].filter(Boolean).join(' · ') })
    },
    true,
  )

  // The helpers an evaluation gets as nb. A target is an element, a CSS selector, or the
  // name a control shows (its label or its text), matched whole first, then in part.
  const find = (target) => {
    if (target instanceof Element) {
      return target
    }

    try {
      const el = document.querySelector(target)

      if (el) {
        return el
      }
    } catch {
      // Not a selector: a name.
    }

    const wanted = squash(target).toLowerCase()
    const candidates = [...document.querySelectorAll(controls)]

    return (
      candidates.find((c) => nameOf(c).toLowerCase() === wanted) ??
      candidates.find((c) => nameOf(c).toLowerCase().includes(wanted)) ??
      null
    )
  }

  const must = (target) => {
    const el = find(target)

    if (!el) {
      throw new Error(`nothing matches ${JSON.stringify(String(target))}`)
    }

    return el
  }

  const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms))

  const card = (el) => {
    const rect = rectOf(el)

    return {
      id: Number(el.getAttribute('data-item-id')),
      title: squash(el.querySelector('[data-card-title]')?.innerText ?? ''),
      list: el.closest('section[aria-label]')?.getAttribute('aria-label') ?? undefined,
      rule: el.getAttribute('data-rule') ?? undefined,
      busy: el.getAttribute('aria-busy') === 'true',
      highlighted: el.hasAttribute('data-highlighted'),
      detailsOpen: el.querySelector('[aria-label="Show details"][aria-expanded="true"]') !== null,
      rect,
      onScreen: onScreen(rect),
      drawn: drawn(el),
      contentsDrawn: el.firstElementChild ? drawn(el.firstElementChild) : null,
      contentVisibility: getComputedStyle(el).contentVisibility || undefined,
    }
  }

  const nb = Object.freeze({
    $: (selector) => document.querySelector(selector),
    $$: (selector) => [...document.querySelectorAll(selector)],
    find,
    describe: (target, opts) => describe(must(target), opts),
    dom: (selector, opts = {}) =>
      [...document.querySelectorAll(selector)].slice(0, opts.limit ?? 50).map((el) => describe(el, opts)),
    text: (target) => squash(must(target).innerText),
    cards: () => [...document.querySelectorAll('li[data-item-id]')].map(card),
    click: (target) => {
      const el = must(target)
      el.scrollIntoView({ block: 'center' })
      el.click()

      return describe(el)
    },
    type: (target, text) => {
      const el = must(target)
      const proto = el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype
      el.focus()
      Object.getOwnPropertyDescriptor(proto, 'value').set.call(el, text)
      el.dispatchEvent(new Event('input', { bubbles: true }))

      return describe(el)
    },
    press: (key, target) => {
      const el = target === undefined ? (document.activeElement ?? document.body) : must(target)
      const init = { key, bubbles: true, cancelable: true }
      el.dispatchEvent(new KeyboardEvent('keydown', init))
      el.dispatchEvent(new KeyboardEvent('keyup', init))

      return key
    },
    scroll: (target) => {
      const el = must(target)
      el.scrollIntoView({ block: 'center' })

      return describe(el)
    },
    wait: sleep,
    waitFor: async (target, timeout = 5000) => {
      const end = Date.now() + timeout

      for (;;) {
        const found = typeof target === 'function' ? await target() : find(target)

        if (found) {
          return found
        }

        if (Date.now() > end) {
          throw new Error(`timed out waiting for ${String(target)}`)
        }

        await sleep(50)
      }
    },
  })

  // compile makes code a function of nb: an expression whose value it returns, or else
  // statements, which return what they return.
  const compile = (code) => {
    try {
      return new Function('nb', `return (async () => (${code}\n))()`)
    } catch {
      return new Function('nb', `return (async () => {${code}\n})()`)
    }
  }

  const run = (id, code) => {
    let fn

    try {
      fn = compile(code)
    } catch (error) {
      post({ t: 'result', id, ok: false, error: errorText(error) })

      return
    }

    Promise.resolve()
      .then(() => fn(nb))
      .then(
        (value) => post({ t: 'result', id, ok: true, value: safe(value) }),
        (error) => post({ t: 'result', id, ok: false, error: errorText(error) }),
      )
  }

  window.__appDevtools = Object.freeze({ run, nb })

  post({
    t: 'hello',
    url: location.href,
    title: document.title,
    width: innerWidth,
    height: innerHeight,
    scale: devicePixelRatio,
    ua: navigator.userAgent,
  })
})()
