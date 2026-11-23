// jsdom matches :focus-visible wherever :focus matches, so a click rings the button it
// focuses as a key would. A browser rings focus only as the user's last input calls for: a
// key pressed without ⌘, ⌃, or ⌥ rings it, and a pointer pressed does not, unless what it
// focused takes typed text. Focus a script moves follows the same last input, so Escape
// closing a menu rings the button it gives focus back to, and a click closing it does not;
// before any input, a script's focus rings, as a page's first focus does.
//
// The stand-in follows that input and decides, as focus moves, whether the focused element
// is ringed. While it is not, every selector a script asks of the DOM (matches, closest,
// querySelector, and querySelectorAll) reads :focus-visible as :not(*), which nothing
// matches, inside :has() and :not() too; while it is, jsdom matches it as :focus. Style
// rules jsdom applies itself are not asked through these, so they still ring all focus.

// keyboard is whether the user's last input was a key; no pointer has pressed yet.
let keyboard = true

// ringed is the element focus last rang, or null.
let ringed: Element | null = null

// textTypes are the input types a browser rings however they were focused, as they take
// typed text.
const textTypes = new Set([
  'text',
  'search',
  'url',
  'tel',
  'email',
  'password',
  'number',
  'date',
  'month',
  'week',
  'time',
  'datetime-local',
])

// takesText reports whether el takes typed text: a text field or text area that can be
// typed in, or an element whose content can be edited.
function takesText(el: Element): boolean {
  if (el instanceof HTMLInputElement) {
    return textTypes.has(el.type) && !el.readOnly
  }

  if (el instanceof HTMLTextAreaElement) {
    return !el.readOnly
  }

  const editable = el.getAttribute('contenteditable')

  return editable !== null && editable !== 'false'
}

// focusVisible matches the stand-in's :focus-visible, outside quoted strings and escaped
// class names such as Tailwind's has-[:focus-visible]: variants.
const focusVisible = /(?<!\\):focus-visible(?![\w-])/g
const quoted = /("(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*')/

// asked is the selector a script asked, with :focus-visible matching nothing while the
// focused element is not ringed.
function asked(selectors: string): string {
  if (!selectors.includes(':focus-visible') || (ringed !== null && ringed === document.activeElement)) {
    return selectors
  }

  return selectors
    .split(quoted)
    .map((part, i) => (i % 2 === 1 ? part : part.replace(focusVisible, ':not(*)')))
    .join('')
}

// installFocusVisible follows the user's input in the window and has the DOM's selector
// methods read :focus-visible as the stand-in decides. src/test/setup.ts installs it once
// per test file, and resetFocusVisible forgets the input after each test.
export function installFocusVisible() {
  window.addEventListener(
    'keydown',
    (event) => {
      if (event.altKey || event.ctrlKey || event.metaKey) {
        return
      }

      keyboard = true
      // A key pressed rings the focus it finds, as well as the focus it moves.
      if (document.activeElement !== null && document.activeElement !== document.body) {
        ringed = document.activeElement
      }
    },
    true,
  )

  for (const type of ['pointerdown', 'mousedown', 'touchstart']) {
    window.addEventListener(
      type,
      () => {
        keyboard = false
      },
      true,
    )
  }

  window.addEventListener(
    'focus',
    (event) => {
      if (event.target instanceof Element) {
        ringed = keyboard || takesText(event.target) ? event.target : null
      }
    },
    true,
  )

  for (const [proto, names] of [
    [Element.prototype, ['matches', 'closest', 'querySelector', 'querySelectorAll']],
    [Document.prototype, ['querySelector', 'querySelectorAll']],
    [DocumentFragment.prototype, ['querySelector', 'querySelectorAll']],
  ] as const) {
    for (const name of names) {
      askThrough(proto, name)
    }
  }
}

// askThrough has proto's selector method name read the selector it is asked as asked
// rewrites it.
function askThrough(proto: object, name: string) {
  const original = Reflect.get(proto, name) as (this: unknown, selectors: string) => unknown

  Object.defineProperty(proto, name, {
    configurable: true,
    writable: true,
    value: function (this: unknown, selectors: string) {
      return original.call(this, asked(selectors))
    },
  })
}

// resetFocusVisible forgets the user's input, so each test starts as a page does, before
// any key or pointer.
export function resetFocusVisible() {
  keyboard = true
  ringed = null
}
