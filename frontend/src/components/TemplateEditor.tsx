import { useImperativeHandle, useLayoutEffect, useRef, useState, type KeyboardEvent, type Ref } from 'react'

import { caretPosition } from '../lib/caret'
import { completionAt, highlight, matchVariables, offsetOf, type Completion, type TokenKind } from '../lib/template'
import { remToPx } from '../lib/textSize'
import type { Diagnostic, PromptVariable } from '../platform'

// TemplateEditorHandle lets the page around the editor insert text and point at a spot.
export interface TemplateEditorHandle {
  // insertVariable adds {{.Name}} at the caret, or just .Name inside an open action.
  insertVariable: (name: string) => void
  // insertText adds text at the caret, replacing any selection.
  insertText: (text: string) => void
  // reveal selects a 1-based line and column (and length) and scrolls it into view.
  reveal: (line: number, column: number, length: number) => void
}

export interface TemplateEditorProps {
  id: string
  value: string
  onChange: (value: string) => void
  variables: readonly PromptVariable[]
  diagnostics: readonly Diagnostic[]
  label: string
  describedBy?: string
  placeholder?: string
  ref?: Ref<TemplateEditorHandle>
}

// Every text style the editor uses changes only color, never a glyph's width, so the
// colored copy behind the text area lines up with it exactly.
const kindClass: Readonly<Record<TokenKind, string>> = {
  text: 'text-text',
  delim: 'text-muted',
  keyword: 'text-accent',
  variable: 'rounded-sm bg-info/10 text-info',
  unknown: 'text-danger',
  function: 'text-accent',
  string: 'text-success',
  comment: 'text-muted',
  code: 'text-muted',
}

const underlineClass = {
  '': '',
  error: 'underline decoration-danger decoration-wavy underline-offset-4',
  warning: 'underline decoration-warning decoration-wavy underline-offset-4',
} as const

// The text area and the colored copy behind it share every metric.
const surfaceClass =
  'absolute inset-0 m-0 overflow-y-scroll px-3.5 py-3 font-mono text-[0.8125rem] leading-6 break-words whitespace-pre-wrap'

// popupWidth is the variables popup's width, in rem, so it follows the text size.
const popupWidth = 18

// insertText types text at the focused text area's selection through the browser's own
// editing, which keeps its undo history, and reports whether that worked.
function insertText(text: string): boolean {
  try {
    // eslint-disable-next-line @typescript-eslint/no-deprecated -- the only edit that keeps the text area's undo history
    return document.execCommand('insertText', false, text)
  } catch {
    return false
  }
}

// insertAt replaces [from, to) in the text area with text, through the browser's own
// editing so undo keeps working, falling back to setting the value where that editing
// is not available.
function insertAt(
  textarea: HTMLTextAreaElement,
  from: number,
  to: number,
  text: string,
  onChange: (value: string) => void,
) {
  textarea.focus()
  textarea.setSelectionRange(from, to)

  if (!insertText(text)) {
    const value = textarea.value.slice(0, from) + text + textarea.value.slice(to)

    onChange(value)
    requestAnimationFrame(() => {
      textarea.setSelectionRange(from + text.length, from + text.length)
    })
  }
}

// TemplateEditor edits a prompt's Go template text. Behind a transparent text area it
// draws the same text colored: actions muted, variables tinted (unknown ones red),
// keywords and functions in the accent, strings green, and the backend's diagnostics
// underlined. Typing "{{" or a dot inside an action offers the variables, filtered as
// you type: arrows choose, Enter or Tab inserts, and Escape closes the list.
export function TemplateEditor({
  id,
  value,
  onChange,
  variables,
  diagnostics,
  label,
  describedBy,
  placeholder,
  ref,
}: TemplateEditorProps) {
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const backdropRef = useRef<HTMLPreElement>(null)
  const [completion, setCompletion] = useState<Completion | null>(null)
  const [active, setActive] = useState(0)
  const [popup, setPopup] = useState({ top: 0, left: 0 })

  const names = variables.map((v) => v.name)
  const known = new Set(names)
  const segments = highlight(value, known, diagnostics)
  const matches = completion === null ? [] : matchVariables(names, completion.prefix)
  const open = completion !== null && matches.length > 0
  const listId = `${id}-variables`

  // Keep the colored copy scrolled with the text area.
  const syncScroll = () => {
    const textarea = textareaRef.current
    const backdrop = backdropRef.current

    if (textarea !== null && backdrop !== null) {
      backdrop.scrollTop = textarea.scrollTop
      backdrop.scrollLeft = textarea.scrollLeft
    }
  }

  useLayoutEffect(syncScroll, [value])

  // updateCompletion looks at the caret and offers variables when it follows "{{" or a
  // dot inside an action.
  const updateCompletion = () => {
    const textarea = textareaRef.current

    if (textarea === null || textarea.selectionStart !== textarea.selectionEnd) {
      setCompletion(null)

      return
    }

    const next = completionAt(textarea.value, textarea.selectionStart)

    if (next === null) {
      setCompletion(null)

      return
    }

    const pos = caretPosition(textarea, next.from)

    setPopup({
      top: pos.top + pos.height + 2,
      left: Math.max(0, Math.min(pos.left, textarea.clientWidth - remToPx(popupWidth, textarea.ownerDocument))),
    })
    setActive(0)
    setCompletion(next)
  }

  const accept = (name: string) => {
    const textarea = textareaRef.current

    if (textarea === null || completion === null) {
      return
    }

    insertAt(textarea, completion.from, completion.to, `.${name}${completion.close ? '}}' : ''}`, onChange)
    setCompletion(null)
  }

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (!open) {
      return
    }

    const pick = matches[active]

    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault()
        setActive((at) => (at + 1) % matches.length)
        break
      case 'ArrowUp':
        e.preventDefault()
        setActive((at) => (at - 1 + matches.length) % matches.length)
        break
      case 'Enter':
      case 'Tab':
        if (pick !== undefined) {
          e.preventDefault()
          accept(pick)
        }

        break
      case 'Escape':
        // Closing the list must not also close the page or a menu around it.
        e.preventDefault()
        e.stopPropagation()
        setCompletion(null)
        break
    }
  }

  useImperativeHandle(
    ref,
    () => ({
      insertVariable: (name: string) => {
        const textarea = textareaRef.current

        if (textarea === null) {
          return
        }

        const at = textarea.selectionStart
        const inAction = completionAt(textarea.value, at) !== null || /\{\{[^}]*$/.test(textarea.value.slice(0, at))
        const text = inAction ? `.${name}` : `{{.${name}}}`

        insertAt(textarea, at, textarea.selectionEnd, text, onChange)
      },
      insertText: (text: string) => {
        const textarea = textareaRef.current

        if (textarea !== null) {
          insertAt(textarea, textarea.selectionStart, textarea.selectionEnd, text, onChange)
        }
      },
      reveal: (line: number, column: number, length: number) => {
        const textarea = textareaRef.current

        if (textarea === null) {
          return
        }

        const start = offsetOf(textarea.value, line, column)

        textarea.focus()
        textarea.setSelectionRange(start, Math.min(start + Math.max(length, 0), textarea.value.length))

        const pos = caretPosition(textarea, start)

        textarea.scrollTop += pos.top - textarea.clientHeight / 3
        syncScroll()
      },
    }),
    [onChange],
  )

  return (
    <div className="relative min-h-0 flex-1 overflow-hidden rounded-lg border border-border bg-background transition-colors focus-within:border-accent/60">
      <pre ref={backdropRef} aria-hidden="true" className={`${surfaceClass} pointer-events-none text-text`}>
        {segments.map((s, i) => (
          // Segments are rebuilt from the text on every change, so their order is their identity.
          <span key={i} className={`${kindClass[s.kind]} ${underlineClass[s.underline]}`}>
            {s.text}
          </span>
        ))}
        {/* A final newline keeps the copy as tall as the text area when the text ends in one. */}
        {'\n'}
      </pre>
      <textarea
        ref={textareaRef}
        id={id}
        value={value}
        aria-label={label}
        aria-describedby={describedBy}
        aria-autocomplete="list"
        aria-controls={open ? listId : undefined}
        aria-activedescendant={open ? `${listId}-${String(active)}` : undefined}
        spellCheck={false}
        autoCapitalize="off"
        autoCorrect="off"
        placeholder={placeholder}
        onChange={(e) => {
          onChange(e.target.value)
        }}
        onInput={updateCompletion}
        onKeyDown={onKeyDown}
        onKeyUp={(e) => {
          if (e.key === 'ArrowLeft' || e.key === 'ArrowRight' || e.key === 'Home' || e.key === 'End') {
            updateCompletion()
          }
        }}
        onClick={updateCompletion}
        onBlur={() => {
          setCompletion(null)
        }}
        onScroll={syncScroll}
        className={`${surfaceClass} resize-none bg-transparent text-transparent caret-text outline-none selection:bg-accent/25 placeholder:text-muted/70`}
      />
      {open && (
        <div
          id={listId}
          role="listbox"
          aria-label="Variables"
          style={{ top: popup.top, left: popup.left, width: `${String(popupWidth)}rem` }}
          className="absolute z-10 max-h-56 overflow-y-auto rounded-lg border border-border bg-surface p-1 text-sm shadow-xl shadow-black/20"
        >
          {matches.map((name, i) => {
            const v = variables.find((x) => x.name === name)

            return (
              <div
                key={name}
                id={`${listId}-${String(i)}`}
                role="option"
                tabIndex={-1}
                aria-selected={i === active}
                onMouseDown={(e) => {
                  // Keep the caret in the text area while choosing.
                  e.preventDefault()
                  accept(name)
                }}
                onMouseEnter={() => {
                  setActive(i)
                }}
                className="flex cursor-pointer items-baseline gap-2 rounded-md px-2 py-1 aria-selected:bg-raised"
              >
                <span className="font-mono text-[0.75rem] text-info">.{name}</span>
                <span className="truncate text-xs text-muted">{v?.description}</span>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
