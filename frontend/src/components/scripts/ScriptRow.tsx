import type { ReactNode } from 'react'

import type { ScriptAsk } from '../../lib/useScriptRun'
import type { RunsIn, ScriptEntry } from '../../platform'
import { buttonClass, iconButtonClass, linkClass } from '../controls'
import {
  AlertIcon,
  BoxIcon,
  ChecklistIcon,
  ClockIcon,
  FlaskIcon,
  InfoIcon,
  MoreIcon,
  PlayIcon,
  RingIcon,
  SlidersIcon,
  TagIcon,
} from '../icons'
import { MenuButton, type MenuItem } from '../Menu'

// iconClass sizes a row's icon and lines it up with its name.
const iconClass = 'mt-0.5 size-4 shrink-0'

// EntryIcon marks a script: ◌ greyed, ⇡ Release does it, a package Deps does it, or else
// its t3.json icon, ▶ by default. The words beside it say the same, so none is told by its
// icon alone.
function EntryIcon({ entry }: { entry: ScriptEntry }) {
  if (!entry.runnable) {
    return <RingIcon className={`${iconClass} text-muted`} />
  }

  if (entry.release || entry.deps) {
    return <PageIcon page={entry.release ? 'Release' : 'Deps'} className={`${iconClass} text-accent`} />
  }

  switch (entry.icon) {
    case 'test':
      return <FlaskIcon className={`${iconClass} text-muted`} />
    case 'lint':
      return <ChecklistIcon className={`${iconClass} text-muted`} />
    case 'build':
      return <BoxIcon className={`${iconClass} text-muted`} />
    case 'configure':
      return <SlidersIcon className={`${iconClass} text-muted`} />
    default:
      return <PlayIcon className={`${iconClass} text-muted`} />
  }
}

// Note is a quieter line under a script's name, with its icon.
function Note({ icon, children }: { icon: ReactNode; children: ReactNode }) {
  return (
    <p className="flex items-start gap-1 text-xs text-muted">
      {icon}
      <span className="min-w-0">{children}</span>
    </p>
  )
}

const noteIcon = 'mt-px size-3.5 shrink-0'

// releaseWords say that Release bumps a version, with its checks, where a script would.
const releaseWords = 'Release does this, with checks'

// depsWords say that Deps upgrades the dependencies, where a script would in the clone
// itself, on whatever branch it is on.
const depsWords = 'Deps upgrades in a fresh worktree and opens a pull request'

// Page is a page of the Scripts tab that does what a script would, which the page points
// to in its place.
export type Page = 'Release' | 'Deps'

// PageIcon marks a page that does what a script would: ⇡ Release's tag, or Deps' package.
export function PageIcon({ page, className }: { page: Page; className: string }) {
  return page === 'Release' ? <TagIcon className={className} /> : <BoxIcon className={className} />
}

export interface ScriptMenuProps {
  entry: ScriptEntry
  // args are the Arguments field's words, which a magex target that takes them runs with.
  args: string[]
  // other is the place runs don't go, as set, and otherName its name, for the menu's way
  // to run there instead.
  other: RunsIn
  otherName: string
  // pending holds the buttons while a run is being asked for.
  pending: boolean
  // onRun asks for a run; onOpenRelease opens Release and onOpenDeps Deps, each null while
  // its page isn't on.
  onRun: (ask: ScriptAsk) => void
  onOpenRelease: (() => void) | null
  onOpenDeps: (() => void) | null
}

// Pointer is a page that does what a script would, as the page points there in its place:
// the page, the words that say so, and the way there, or null while the page isn't on.
export interface Pointer {
  page: Page
  words: string
  onOpen: (() => void) | null
}

// pointersOf are the pages that do what a script would, Release's first: Release for one
// that bumps a version, and Deps for one that upgrades the dependencies in the clone. A
// script either points to has no Run; its menu runs it anyway.
export function pointersOf({
  entry,
  onOpenRelease,
  onOpenDeps,
}: Pick<ScriptMenuProps, 'entry' | 'onOpenRelease' | 'onOpenDeps'>): Pointer[] {
  return [
    ...(entry.release ? [{ page: 'Release' as const, words: releaseWords, onOpen: onOpenRelease }] : []),
    ...(entry.deps ? [{ page: 'Deps' as const, words: depsWords, onOpen: onOpenDeps }] : []),
  ]
}

// argsFor are the arguments entry runs with: the Arguments field's, for a magex target
// that takes them, and none for any other.
export function argsFor(entry: ScriptEntry, args: string[]): string[] {
  return entry.args ? args : []
}

// scriptMenu is a script's ⋯ menu: Run in the other place, or, for one that bumps a
// version or upgrades the dependencies, Run it anyway, here or there. withPages adds the
// way to each page that does it first (Open Release, Open Deps), for a chip, which has no
// line of its own to say the page does it.
export function scriptMenu(menu: ScriptMenuProps, withPages = false): MenuItem[] {
  const { entry, args, other, otherName, pending, onRun } = menu
  const run = (place: RunsIn | '', anyway: boolean) => {
    onRun({ entry, runsIn: place, anyway, args: argsFor(entry, args) })
  }
  const pointers = pointersOf(menu)

  if (pointers.length === 0) {
    return [
      {
        label: `Run in ${otherName}`,
        disabled: pending,
        onSelect: () => {
          run(other, false)
        },
      },
    ]
  }

  const opens: MenuItem[] = withPages
    ? pointers.flatMap(({ page, onOpen }) => (onOpen === null ? [] : [{ label: `Open ${page}`, onSelect: onOpen }]))
    : []

  return [
    ...opens,
    {
      label: 'Run it anyway',
      divided: opens.length > 0,
      disabled: pending,
      onSelect: () => {
        run('', true)
      },
    },
    {
      label: `Run it anyway in ${otherName}`,
      disabled: pending,
      onSelect: () => {
        run(other, true)
      },
    },
  ]
}

export interface ScriptRowProps extends ScriptMenuProps {
  // command reports whether the row shows the command it runs, as a repository file's
  // scripts do; a magex target's is its name.
  command: boolean
  // about is a quiet line under the command, such as where one of the user's own scripts
  // shows, or ''.
  about?: string
  // more are the menu's items after the ways to run it, such as Edit for one of the
  // user's own scripts, which its menu holds even while it can't run.
  more?: readonly MenuItem[]
}

// rowMenu is a row's ⋯ menu: the ways to run its script, while it can run, then more, set
// apart from them.
function rowMenu(menu: ScriptMenuProps, more: readonly MenuItem[]): MenuItem[] {
  const runs = menu.entry.runnable ? scriptMenu(menu) : []

  return [...runs, ...more.map((item, i) => (i === 0 && runs.length > 0 ? { ...item, divided: true } : item))]
}

// ScriptRow is one script: its name, what it does or runs, and Run, with a menu that runs
// it in the other place. A script the app can't run is greyed, with why. One that bumps a
// version points to Release, which does it with its checks, and one that upgrades the
// dependencies in the clone points to Deps, which does it in a fresh worktree with a pull
// request; either has no Run, and its menu runs it anyway. It says when a command changed
// since the user last ran it, when a task runs until it is stopped, and anything else
// about how it runs. One of the user's own scripts says where it shows, and its menu edits
// it too.
export function ScriptRow({ command, about = '', more = [], ...menu }: ScriptRowProps) {
  const { entry, args, pending, onRun } = menu
  const pointers = pointersOf(menu)

  return (
    <li className="flex items-start gap-3 rounded-md px-3 py-2 hover:bg-raised/60">
      <EntryIcon entry={entry} />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <p className="flex flex-wrap items-baseline gap-x-2 text-sm">
          <span className="font-medium">{entry.name}</span>
          {entry.detail !== '' && <span className="text-xs text-muted">{entry.detail}</span>}
        </p>
        {command && <p className="font-mono text-xs break-all text-muted">{entry.command}</p>}
        {about !== '' && <p className="text-xs text-muted">{about}</p>}
        {!entry.runnable && (
          <p className="text-xs text-muted">
            <span className="sr-only">Can’t run: </span>
            {entry.why}
          </p>
        )}
        {entry.runnable &&
          pointers.map(({ page, words, onOpen }) => (
            <p key={page} className="text-xs text-text">
              {words}
              {onOpen !== null && (
                <>
                  {' · '}
                  <button type="button" onClick={onOpen} className={linkClass}>
                    {`Open ${page}`}
                  </button>
                </>
              )}
            </p>
          ))}
        {entry.trust === 'changed' && (
          <Note icon={<AlertIcon className={`${noteIcon} text-warning`} />}>Changed since you last ran it</Note>
        )}
        {entry.background && (
          <Note icon={<ClockIcon className={`${noteIcon} text-muted`} />}>Runs until you stop it.</Note>
        )}
        {entry.note !== '' && <Note icon={<InfoIcon className={`${noteIcon} text-info`} />}>{entry.note}</Note>}
      </div>
      {(entry.runnable || more.length > 0) && (
        <div className="flex shrink-0 items-center gap-1">
          {entry.runnable && pointers.length === 0 && (
            <button
              type="button"
              aria-label={`Run ${entry.name}`}
              disabled={pending}
              onClick={() => {
                onRun({ entry, runsIn: '', anyway: false, args: argsFor(entry, args) })
              }}
              className={buttonClass}
            >
              Run
            </button>
          )}
          <MenuButton label={`More for ${entry.name}`} items={() => rowMenu(menu, more)} className={iconButtonClass}>
            <MoreIcon className="size-4" />
          </MenuButton>
        </div>
      )}
    </li>
  )
}
