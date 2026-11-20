// The inspector's agent in the window's page, in a build from source alone
// (internal/devtools). The app runs it on every page load and before each evaluation; it
// installs once. It posts to the app through the web view's message handler, each message
// prefixed as Page.Receive expects: the page describing itself ("hello"), each console line
// and each error nothing caught ("console"), each thing the user does ("trail"), each note
// the user leaves on an element they point at ("note"), the user's choices in the
// inspector's part of Settings ("inspector"), and each evaluation's result ("result").
// What the user types into the app is never posted, only the field it went into; a note's
// words are, since the user typed them into the agent's own box to say them. Nothing
// leaves the Mac: the app keeps it for the inspector's socket, the notes in the data
// folder. It is window.__appDevtools: run and nb for an evaluation, noted and sync for
// the app, and inspector for the window's Settings, which find nothing there in a release.
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

  // labelOf is what labels el: its aria-label, or else the text of the elements its
  // aria-labelledby names.
  const labelOf = (el) => {
    const label = squash(el.getAttribute('aria-label') ?? '')

    if (label !== '') {
      return label
    }

    const ids = (el.getAttribute('aria-labelledby') ?? '').split(/\s+/).filter(Boolean)

    return squash(ids.map((id) => document.getElementById(id)?.innerText ?? '').join(' '))
  }

  // targetText names what the user acted on: a control by its name, anything else by its
  // label (labelOf) or title, or its text when it holds no other element, so a container
  // never reads out the page.
  const targetText = (el) => {
    const plain = !el.matches(controls) && el.childElementCount > 0
    const name = clip(plain ? labelOf(el) || squash(el.getAttribute('title') ?? '') : nameOf(el), 80)

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
      const label = clip(labelOf(dialog), 80)
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

  // React's development build (app:dev) keeps on each element the fiber that rendered it,
  // and on each fiber the component it ran, its owner (_debugOwner), and the stack its
  // element was made on (_debugStack). A production build keeps no _debugOwner, no stacks,
  // and no names worth reading, so it is read only under the development build.
  const fiberOf = (el) => {
    for (let node = el; node; node = node.parentElement) {
      const key = Object.keys(node).find((k) => k.startsWith('__reactFiber$'))

      if (key) {
        return node[key]
      }
    }

    return null
  }

  // componentName is the name of the component a fiber ran (a forwardRef's or a memo's
  // inner one's), or '' for an element, a provider, or anything else React made.
  const componentName = (fiber) => {
    const type = fiber?.type
    const fn = typeof type === 'function' ? type : (type?.render ?? type?.type)

    return typeof fn === 'function' ? String(type.displayName || fn.displayName || fn.name || '') : ''
  }

  // sourceOf is a stack's first frame in the frontend's own sources, as the file in the
  // repository and its line, or ''.
  const sourceOf = (stack) => {
    const text = typeof stack === 'string' ? stack : stack?.stack

    if (typeof text !== 'string') {
      return ''
    }

    for (const frame of text.split('\n')) {
      const found = frame.includes('/node_modules/')
        ? null
        : /\/(src\/[^\s?#:()]+\.[cm]?[jt]sx?)(?:\?[^\s:()]*)?:(\d+)/.exec(frame)

      if (found) {
        return `frontend/${found[1]}:${found[2]}`
      }
    }

    return ''
  }

  // reactOf is, under React's development build, the components that render el, the
  // nearest few of them outermost first, and the file and line whose JSX made it, the line
  // approximate (the stack's lines are the compiled module's); nothing under a production
  // build.
  const reactOf = (el, few = 6) => {
    const fiber = fiberOf(el)

    if (!fiber || !('_debugOwner' in fiber)) {
      return {}
    }

    const names = []
    let source = ''

    for (let f = fiber; f && names.length < few; f = f.return) {
      source ||= sourceOf(f._debugStack)

      const name = componentName(f)

      if (name !== '' && name !== names.at(-1)) {
        names.push(name)
      }
    }

    return { components: names.reverse().join(' › ') || undefined, source: source || undefined }
  }

  // viewOf names what the window shows: the items its navigation marks as current, outside
  // any dialog, as each reads.
  const viewOf = () =>
    [...document.querySelectorAll('nav [aria-pressed="true"], nav [aria-current="page"], nav [aria-current="true"]')]
      .filter((e) => !e.closest('dialog, [role=dialog]') && drawn(e) !== false)
      .map((e) => clip(nameOf(e), 40))
      .filter((name) => name !== '')
      .join(' › ')

  // nearbyText is the text around el: its row's, list item's, form's, or section's, cut
  // short.
  const around =
    'li, tr, [role=row], [role=listitem], article, fieldset, form, header, footer, section, dialog, [role=dialog]'
  const nearbyText = (el) => clip(squash((el.closest(around) ?? el.parentElement ?? el).innerText), 300)

  // Pointing (⌥⌘P, or nb.point): the user points at an element and says what is wrong with
  // it. While it is on, the agent outlines what the pointer is over and takes the clicks, so
  // the app hears none; a click opens a box for a note on what was clicked, and Enter posts
  // the note ("note"): the user's words, the element as the trail names it and as describe
  // has it, the text around it, the view, and what React's development build says of it.
  // Escape drops the note being written, or ends pointing, and leaves any dialog open.
  //
  // What the agent draws is one element, the layer: a popover in the open dialog the
  // pointer is over, or else the last modal one, or else the body. A modal dialog leaves the
  // rest of the page inert, and the top layer draws the popover over the dialog. Nothing the
  // user does in the layer reaches the app: every event from it stops at the window.
  const pointColor = '#2563eb'
  const surface = 'var(--color-surface, #18181b)'
  const ink = 'var(--color-text, #fafafa)'
  const muted = 'var(--color-muted, #a1a1aa)'
  const rule = 'var(--color-border, #3f3f46)'
  const idleWords = 'Pointing: click what you mean · ↑ ↓ wider or narrower · Esc ends it'

  const make = (tag, style, text) => {
    const node = document.createElement(tag)
    node.style.cssText = style

    if (text !== undefined) {
      node.textContent = text
    }

    return node
  }

  const layer = make(
    'div',
    'position: fixed; inset: 0; width: auto; height: auto; max-width: none; max-height: none; margin: 0; padding: 0; ' +
      `border: 0; background: transparent; overflow: visible; pointer-events: none; z-index: 2147483647; color: ${ink}; ` +
      'font: 500 0.75rem/1.4 -apple-system, BlinkMacSystemFont, system-ui, sans-serif; text-align: left',
  )
  layer.setAttribute('data-devtools-point', '')

  const cursor = make(
    'style',
    '',
    '*, *::before, *::after { cursor: crosshair !important } [data-devtools-point] textarea { cursor: text !important }',
  )
  const box = make(
    'div',
    `position: absolute; display: none; box-sizing: border-box; border: 2px solid ${pointColor}; ` +
      'border-radius: 0.1875rem; background: rgb(37 99 235 / 0.12)',
  )
  const caption = make(
    'div',
    'position: absolute; display: none; max-width: 40rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; ' +
      `padding: 0.125rem 0.375rem; border-radius: 0.25rem; background: ${pointColor}; color: #fff; font-size: 0.6875rem`,
  )
  const badge = make(
    'div',
    'position: absolute; left: 50%; bottom: 1rem; transform: translateX(-50%); white-space: nowrap; ' +
      `padding: 0.375rem 0.75rem; border-radius: 999px; border: 1px solid ${pointColor}; background: ${surface}; ` +
      'box-shadow: 0 0.25rem 1rem rgb(0 0 0 / 0.3)',
    idleWords,
  )
  const form = make(
    'div',
    `position: absolute; width: 20rem; box-sizing: border-box; padding: 0.5rem; border-radius: 0.5rem; ` +
      `border: 1px solid ${rule}; background: ${surface}; box-shadow: 0 0.5rem 1.5rem rgb(0 0 0 / 0.35); pointer-events: auto`,
  )
  const heading = make(
    'div',
    `margin: 0 0 0.375rem; color: ${muted}; white-space: nowrap; overflow: hidden; text-overflow: ellipsis`,
  )
  const field = make(
    'textarea',
    'display: block; box-sizing: border-box; width: 100%; min-height: 4.5rem; margin: 0; padding: 0.375rem 0.5rem; ' +
      `resize: vertical; border-radius: 0.375rem; border: 1px solid ${pointColor}; background: transparent; color: ${ink}; ` +
      'font: inherit; outline: none',
  )
  field.rows = 3
  field.placeholder = 'What’s wrong here?'
  field.setAttribute('aria-label', 'Note')
  form.append(
    heading,
    field,
    make(
      'div',
      `margin: 0.375rem 0 0; color: ${muted}; font-size: 0.6875rem`,
      'Enter saves · ⇧Enter for a new line · Esc cancels',
    ),
  )
  layer.append(cursor, box, caption, badge)

  // pointing is whether pointing is on; hovered is the element outlined, base the one under
  // the pointer, and depth how many levels above base hovered is (↑ and ↓); picked is the
  // element the note being written is on; focusBack had focus before its box opened;
  // saving is a note on its way, the layer hidden for the window's picture the app takes;
  // badgeWords are what the badge says, before the count of open notes.
  let pointing = false
  let hovered = null
  let base = null
  let depth = 0
  let picked = null
  let focusBack = null
  let saving = false
  let keeper = 0
  let revealTimer = 0
  let idleTimer = 0
  let badgeWords = idleWords

  // state is the inspector as the window's Settings show it (internal/devtools'
  // InspectorState: whether the user lets pointing start, and how many notes are open and
  // kept), and whether pointing is on now. The app tells the agent the rest (sync) as the
  // page loads and after each change. Each change makes a new object, so a store that
  // reads it sees the change; watchers hear each one.
  let state = Object.freeze({ enabled: true, pointing: false, open: 0, kept: 0 })
  const watchers = new Set()

  const update = (patch) => {
    state = Object.freeze({ ...state, ...patch })

    for (const watcher of watchers) {
      watcher()
    }
  }

  const inLayer = (node) => node instanceof Node && layer.contains(node)

  const popoverOpen = (el) => {
    try {
      return el.matches(':popover-open')
    } catch {
      return false
    }
  }

  const modals = () => {
    try {
      return [...document.querySelectorAll('dialog:modal')]
    } catch {
      return []
    }
  }

  // mount puts the layer in host and shows it over everything there: as a popover, in the
  // top layer, or else fixed over the page, where a page without popovers leaves it.
  const mount = (host) => {
    const popover = typeof layer.showPopover === 'function'

    if (layer.parentElement !== host) {
      if (popoverOpen(layer)) {
        layer.hidePopover()
      }

      if (popover) {
        layer.setAttribute('popover', 'manual')
      }

      host.append(layer)
    }

    if (popover && layer.isConnected && !popoverOpen(layer)) {
      layer.showPopover()
    }
  }

  // settle moves the layer where it must be once what it was in has closed, or a modal
  // dialog has opened over it.
  const settle = () => {
    const host = layer.parentElement
    const open = modals()
    const fine =
      layer.isConnected &&
      host !== null &&
      !host.matches('dialog:not([open])') &&
      (open.length === 0 || open.some((d) => d.contains(host)))

    mount(fine ? host : (open.at(-1) ?? document.body))
  }

  // showBadge has the badge say its words, then how many notes are open.
  const showBadge = () => {
    const open = state.open
    badge.textContent = open > 0 ? `${badgeWords} · ${open} open note${open === 1 ? '' : 's'}` : badgeWords
  }

  // say puts words in the badge, which goes back to saying pointing is on after a while.
  const say = (words) => {
    badgeWords = words
    showBadge()
    clearTimeout(idleTimer)

    if (words !== idleWords) {
      idleTimer = setTimeout(() => {
        badgeWords = idleWords
        showBadge()
      }, 3000)
    }
  }

  // outlined is the element depth levels above base: what ↑ and ↓ outline.
  const outlined = () => {
    let el = base

    for (let i = 0; i < depth && el; i++) {
      el = el.parentElement
    }

    return el
  }

  // widens reports whether ↑ may outline el's parent: not the body, nor above a dialog.
  const widens = (el) =>
    el?.parentElement != null &&
    el.parentElement !== document.body &&
    el.parentElement !== document.documentElement &&
    !el.matches('dialog')

  // hover outlines what the pointer is over: target, or, after ↑, the element outlined
  // still, while the pointer stays inside it.
  const hover = (target) => {
    if (target !== base) {
      const kept = depth > 0 && hovered?.contains(target) ? hovered : null

      base = target
      depth = 0

      for (let el = target; kept && el && el !== kept; el = el.parentElement) {
        depth++
      }
    }

    outline(outlined())
  }

  // outline outlines el, its name and its components above it, or nothing for none.
  const outline = (el) => {
    hovered = el

    if (!el?.isConnected) {
      box.style.display = 'none'
      caption.style.display = 'none'

      return
    }

    const r = el.getBoundingClientRect()
    const chain = reactOf(el, 3).components
    Object.assign(box.style, {
      display: 'block',
      left: `${r.x - 2}px`,
      top: `${r.y - 2}px`,
      width: `${r.width + 4}px`,
      height: `${r.height + 4}px`,
    })
    caption.textContent = chain ? `${targetText(el)} · ${chain}` : targetText(el)
    caption.style.display = 'block'

    const above = r.y - caption.offsetHeight - 4
    Object.assign(caption.style, { left: `${Math.max(0, r.x - 2)}px`, top: `${above >= 0 ? above : r.bottom + 4}px` })
  }

  // placeForm puts the note's box under el, or over it when there is no room below, inside
  // the window.
  const placeForm = (el) => {
    const r = el.getBoundingClientRect()
    const width = form.offsetWidth || 320
    const height = form.offsetHeight || 140
    const below = r.bottom + 8
    const top = below + height <= innerHeight - 8 ? below : Math.max(8, r.y - 8 - height)
    const left = Math.min(Math.max(8, r.x), Math.max(8, innerWidth - width - 8))
    Object.assign(form.style, { left: `${left}px`, top: `${top}px` })
  }

  // pick opens the note's box on el, or moves it there, keeping what was typed.
  const pick = (el) => {
    if (!picked) {
      focusBack = document.activeElement
    }

    picked = el
    outline(el)
    heading.textContent = `Note on ${caption.textContent}`
    layer.append(form)
    placeForm(el)
    field.focus({ preventScroll: true })
  }

  // closeNote closes the note's box, dropping what was typed, and gives focus back.
  const closeNote = () => {
    if (!picked) {
      return
    }

    picked = null
    field.value = ''

    if (focusBack instanceof HTMLElement && focusBack !== document.body && focusBack.isConnected) {
      focusBack.focus({ preventScroll: true })
    } else {
      field.blur()
    }

    focusBack = null
    form.remove()
    outline(null)
  }

  // reveal shows the layer again once a note is on its way, saying it was kept as note n,
  // or only sent when the app never said (n 0).
  const reveal = (n) => {
    clearTimeout(revealTimer)
    saving = false
    layer.style.visibility = ''
    say(n > 0 ? `Note ${n} saved` : 'Note sent')
  }

  // noteStyles are the computed styles a note keeps of its element, as CSS names them, and
  // unset the values that say nothing about it: a default, or nothing at all.
  const noteStyles = [
    'display',
    'position',
    'color',
    'background-color',
    'font-size',
    'font-weight',
    'line-height',
    'padding',
    'margin',
    'gap',
    'border-width',
    'border-style',
    'border-color',
    'border-radius',
    'opacity',
    'overflow',
    'text-align',
    'white-space',
  ]
  const unset = new Set(['', 'none', 'normal', 'auto', '0px', 'visible', 'static', 'start', 'rgba(0, 0, 0, 0)'])

  // stylesOf is el's computed styles that say something about it, as name and value pairs,
  // in noteStyles' order: a border's style and color only with a width.
  const stylesOf = (el) => {
    const style = getComputedStyle(el)
    const set = noteStyles
      .map((name) => [name, squash(style.getPropertyValue(name))])
      .filter(([name, value]) => !unset.has(value) && !(name === 'opacity' && value === '1'))
    const bordered = set.some(([name]) => name === 'border-width')

    return set.filter(([name]) => bordered || (name !== 'border-style' && name !== 'border-color'))
  }

  const afterPaint = (fn) =>
    typeof requestAnimationFrame === 'function'
      ? requestAnimationFrame(() => requestAnimationFrame(fn))
      : setTimeout(fn, 50)

  // saveNote posts the note being written, once the layer is hidden from the picture of the
  // window the app takes; the app then says it has the note (noted), which shows the layer
  // again, as a while without word does. The layer is hidden before the note reads the
  // page, too, so its words are none of the element's nor of the text around it: it sits
  // in the dialog the element is in.
  const saveNote = () => {
    const text = field.value.trim()

    if (!picked || text === '') {
      return
    }

    const el = picked
    closeNote()
    saving = true
    layer.style.visibility = 'hidden'

    const hit = control(el)
    const note = {
      t: 'note',
      text: clip(text, 4000),
      target: targetText(hit),
      where: where(hit),
      view: viewOf(),
      element: describe(el),
      styles: stylesOf(el),
      nearby: nearbyText(el),
      ...reactOf(el),
      width: innerWidth,
      height: innerHeight,
    }

    clearTimeout(revealTimer)
    revealTimer = setTimeout(() => reveal(0), 5000)
    afterPaint(() => post(note))
  }

  // setPointing starts or ends pointing, and reports whether it is on. It never starts
  // while the user has pointing turned off.
  const setPointing = (on) => {
    if (on === pointing || (on && !state.enabled)) {
      return pointing
    }

    pointing = on
    update({ pointing: on })

    if (on) {
      layer.style.visibility = ''
      say(idleWords)
      mount(document.activeElement?.closest('dialog[open]') ?? modals().at(-1) ?? document.body)
      keeper = setInterval(() => {
        settle()

        if (picked) {
          outline(picked)
        }
      }, 250)
    } else {
      closeNote()
      clearInterval(keeper)
      clearTimeout(revealTimer)
      clearTimeout(idleTimer)
      saving = false
      outline(null)
      base = null
      depth = 0

      if (popoverOpen(layer)) {
        layer.hidePopover()
      }

      layer.remove()
    }

    post({ t: 'trail', kind: 'point', target: on ? 'on' : 'off', where: '' })

    return pointing
  }

  // sync takes in the inspector's state the app tells the agent: as the page loads, and
  // after each change. Pointing turned off ends it.
  const sync = (next) => {
    update({ enabled: next?.enabled !== false, open: Number(next?.open) || 0, kept: Number(next?.kept) || 0 })

    if (!state.enabled) {
      setPointing(false)
    }

    showBadge()
  }

  // inspector is the inspector's part of the window's Settings (About → Inspector): its
  // state, a way to hear it change, and the user's choices, which go to the app, which
  // keeps them and tells the agent the state they leave (sync).
  const inspector = Object.freeze({
    state: () => state,
    subscribe: (watcher) => {
      watchers.add(watcher)

      return () => {
        watchers.delete(watcher)
      }
    },
    setEnabled: (on) => {
      update({ enabled: on === true })

      if (!state.enabled) {
        setPointing(false)
      }

      post({ t: 'inspector', enabled: on === true })
    },
    clearNotes: () => {
      post({ t: 'inspector', clear: true })
    },
  })

  const swallow = (e) => {
    e.preventDefault()
    e.stopImmediatePropagation()
  }

  // The keys, heard before anything in the page hears them: ⌥⌘P starts and ends pointing
  // anywhere (matched by the key's place, since ⌥P types π), unless the user has pointing
  // turned off, when the key is the app's; while pointing, ↑ and ↓ outline the element
  // around the one outlined, or back in, Escape drops the note being written, or ends
  // pointing; and in the note's box, Enter saves and every key is the box's alone.
  window.addEventListener(
    'keydown',
    (e) => {
      if (e.code === 'KeyP' && e.altKey && e.metaKey && !e.ctrlKey && !e.shiftKey) {
        if (state.enabled) {
          swallow(e)
          setPointing(!pointing)
        }

        return
      }

      if (!pointing) {
        return
      }

      if (inLayer(e.target)) {
        e.stopImmediatePropagation()

        if (e.isComposing) {
          return
        }

        if (e.key === 'Escape') {
          e.preventDefault()
          closeNote()
        } else if (e.key === 'Enter' && !e.shiftKey) {
          e.preventDefault()
          saveNote()
        } else if (e.key === 'Tab') {
          e.preventDefault()
        }

        return
      }

      const arrow = e.key === 'ArrowUp' || e.key === 'ArrowDown'
      const plain = !e.altKey && !e.metaKey && !e.ctrlKey && !e.shiftKey

      if (arrow && plain && !picked && !saving && hovered) {
        swallow(e)

        if (e.key === 'ArrowUp' && widens(hovered)) {
          depth++
        } else if (e.key === 'ArrowDown' && depth > 0) {
          depth--
        }

        outline(outlined())

        return
      }

      if (e.key === 'Escape') {
        swallow(e)

        if (picked) {
          closeNote()
        } else {
          setPointing(false)
        }
      }
    },
    true,
  )

  for (const type of [
    'keyup',
    'keypress',
    'beforeinput',
    'input',
    'change',
    'select',
    'paste',
    'copy',
    'cut',
    'compositionstart',
    'compositionupdate',
    'compositionend',
    'wheel',
  ]) {
    window.addEventListener(
      type,
      (e) => {
        if (inLayer(e.target)) {
          e.stopImmediatePropagation()
        }
      },
      true,
    )
  }

  // Focus moving into the layer, or out of it, is the layer's alone: the app never hears
  // that it lost focus to the note's box, nor that it got it back.
  for (const type of ['focus', 'blur', 'focusin', 'focusout']) {
    window.addEventListener(
      type,
      (e) => {
        if (inLayer(e.target) || inLayer(e.relatedTarget)) {
          e.stopImmediatePropagation()
        }
      },
      true,
    )
  }

  // While pointing, a press is the agent's: the app hears none, nor does its default
  // happen (focus, selection, a drag), except in the layer, where it stays.
  for (const type of ['pointerdown', 'mousedown', 'pointerup', 'mouseup', 'dblclick', 'auxclick', 'contextmenu']) {
    window.addEventListener(
      type,
      (e) => {
        if (!pointing) {
          return
        }

        if (inLayer(e.target)) {
          e.stopImmediatePropagation()
        } else {
          swallow(e)
        }
      },
      true,
    )
  }

  window.addEventListener(
    'click',
    (e) => {
      if (!pointing) {
        return
      }

      if (inLayer(e.target)) {
        e.stopImmediatePropagation()

        return
      }

      swallow(e)

      // The click picks what is outlined, which ↑ may have widened to around it.
      if (e.target instanceof Element && !saving) {
        pick(hovered?.contains(e.target) ? hovered : e.target)
      }
    },
    true,
  )

  window.addEventListener(
    'pointermove',
    (e) => {
      if (!pointing || saving || picked || inLayer(e.target) || !(e.target instanceof Element)) {
        return
      }

      mount(e.target.closest('dialog[open]') ?? modals().at(-1) ?? document.body)
      hover(e.target)
    },
    true,
  )

  const redraw = () => {
    if (pointing && hovered) {
      outline(hovered)

      if (picked) {
        placeForm(picked)
      }
    }
  }

  window.addEventListener('scroll', redraw, true)
  window.addEventListener('resize', redraw)

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
    point: (on = true) => setPointing(on !== false),
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

  window.__appDevtools = Object.freeze({ run, nb, noted: reveal, sync, inspector })

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
