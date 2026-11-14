// Reading the keyboard shortcuts the window's pages share.

// Input types that take no typing, so a shortcut in them is free to act.
const untypedInputs = new Set(['checkbox', 'radio', 'button', 'submit', 'reset', 'range', 'color', 'file'])

// isTyping reports whether a key event belongs to editing: a text field or any control in
// a form, where leaving the page would drop an unsaved draft, or an input method
// mid-composition.
export function isTyping(e: KeyboardEvent): boolean {
  const target = e.target

  return (
    e.isComposing ||
    target instanceof HTMLTextAreaElement ||
    (target instanceof HTMLInputElement && !untypedInputs.has(target.type)) ||
    (target instanceof Element && target.closest('form') !== null)
  )
}

// isFindKey reports ⌘F, the Mac's Find: ⌘ alone, since ⌃⌘F is Full Screen and ⌥⌘F and
// ⇧⌘F belong to other commands.
export function isFindKey(e: KeyboardEvent): boolean {
  return e.metaKey && !e.ctrlKey && !e.altKey && !e.shiftKey && e.key.toLowerCase() === 'f'
}

// isPreviewKey reports ⇧⌘P, which switches the prompt editor between writing and
// previewing, as on GitHub: ⇧⌘ alone, so ⌘P, ⌃⇧⌘P, and ⌥⇧⌘P stay free.
export function isPreviewKey(e: KeyboardEvent): boolean {
  return e.metaKey && e.shiftKey && !e.ctrlKey && !e.altKey && e.key.toLowerCase() === 'p'
}
