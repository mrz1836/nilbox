import type { ReactNode } from 'react'

import { PicturesContext, usePictures } from '../lib/pictures'
import { PersonAvatar } from './Avatar'
import { chipClass, quietButtonClass } from './controls'

// The parts the sheets' lists of people share: the reviewers' and the assignees'.

// personLabel is a person's row in a picker: their login, then their name, quieter, and
// "(you)" for the user.
export function personLabel(login: string, name: string, mine = false) {
  return (
    <span className="min-w-0 truncate">
      {login}
      {name !== '' && name !== login && (
        <>
          {' '}
          <span className="text-muted">{name}</span>
        </>
      )}
      {mine && (
        <>
          {' '}
          <span className="text-muted">(you)</span>
        </>
      )}
    </span>
  )
}

// RowAction is the quiet button at the end of a person's line, shown while the line is
// hovered or holds focus: take a request off or an assignee, or ask someone again.
export function RowAction({
  label,
  title,
  disabled,
  onClick,
  children,
}: {
  label: string
  title: string
  disabled: boolean
  onClick: () => void
  children: ReactNode
}) {
  return (
    <button
      type="button"
      aria-label={label}
      title={title}
      disabled={disabled}
      onClick={onClick}
      className={`${quietButtonClass} shrink-0 p-0.5 opacity-0 group-focus-within:opacity-100 group-hover:opacity-100 focus-visible:opacity-100`}
    >
      {children}
    </button>
  )
}

// PersonChip is one person a list offers in one click: whose picture it shows (login), its
// words, its accessible name, and its title.
export interface PersonChip {
  login: string
  text: string
  label: string
  title: string
}

// SuggestionChips offers people in one click each, as a group named label, and More…,
// which opens the picker. While busy names a chip's login, that chip's edit runs, and every
// chip waits. It shows nothing with no chip.
export function SuggestionChips({
  label,
  chips,
  busy,
  onPick,
  onMore,
}: {
  label: string
  chips: readonly PersonChip[]
  busy: string
  onPick: (login: string) => void
  onMore: () => void
}) {
  const pictures = usePictures(chips.map((c) => c.login))

  if (chips.length === 0) {
    return null
  }

  return (
    <PicturesContext value={pictures}>
      <div aria-label={label} role="group" className="flex flex-wrap items-center gap-1.5">
        {chips.map((c) => (
          <button
            key={c.login}
            type="button"
            aria-label={c.label}
            aria-busy={busy === c.login || undefined}
            title={c.title}
            disabled={busy !== ''}
            onClick={() => {
              onPick(c.login)
            }}
            className={`${chipClass} inline-flex items-center gap-1.5 text-text enabled:hover:border-accent/60 enabled:hover:text-accent disabled:opacity-disabled`}
          >
            <PersonAvatar login={c.login} />
            {c.text}
          </button>
        ))}
        <button type="button" onClick={onMore} className={quietButtonClass}>
          More…
        </button>
      </div>
    </PicturesContext>
  )
}
