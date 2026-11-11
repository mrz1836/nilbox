import type { ReactNode } from 'react'

import type { ScriptAsk } from '../../lib/useScriptRun'
import type { RunsIn, ScriptEntry } from '../../platform'
import { buttonClass } from '../controls'
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

// EntryIcon marks a script: ◌ greyed, ⇡ Release does it, or else its t3.json icon, ▶ by
// default. The words beside it say the same, so none is told by its icon alone.
function EntryIcon({ entry }: { entry: ScriptEntry }) {
  if (!entry.runnable) {
    return <RingIcon className={`${iconClass} text-muted`} />
  }

  if (entry.release) {
    return <TagIcon className={`${iconClass} text-accent`} />
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
export const releaseWords = 'Release does this, with checks'

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
  // onRun asks for a run; onOpenRelease opens Release, or is null while it isn't on.
  onRun: (ask: ScriptAsk) => void
  onOpenRelease: (() => void) | null
}

// argsFor are the arguments entry runs with: the Arguments field's, for a magex target
// that takes them, and none for any other.
export function argsFor(entry: ScriptEntry, args: string[]): string[] {
  return entry.args ? args : []
}

// scriptMenu is a script's ⋯ menu: Run in the other place, or, for one that bumps a
// version, Run it anyway, here or there. withRelease adds Open Release first, for a chip,
// which has no line of its own to say Release does it.
export function scriptMenu(
  { entry, args, other, otherName, pending, onRun, onOpenRelease }: ScriptMenuProps,
  withRelease = false,
): MenuItem[] {
  const run = (place: RunsIn | '', anyway: boolean) => {
    onRun({ entry, runsIn: place, anyway, args: argsFor(entry, args) })
  }

  if (!entry.release) {
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

  const openRelease: MenuItem[] =
    withRelease && onOpenRelease !== null ? [{ label: 'Open Release', onSelect: onOpenRelease }] : []

  return [
    ...openRelease,
    {
      label: 'Run it anyway',
      divided: openRelease.length > 0,
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
}

// ScriptRow is one script: its name, what it does or runs, and Run, with a menu that runs
// it in the other place. A script the app can't run is greyed, with why. One that bumps a
// version points to Release, which does it with its checks, and its menu runs it anyway.
// It says when a command changed since the user last ran it, when a task runs until it is
// stopped, and anything else about how it runs.
export function ScriptRow({ command, ...menu }: ScriptRowProps) {
  const { entry, args, pending, onRun, onOpenRelease } = menu

  return (
    <li className="flex items-start gap-3 rounded-md px-3 py-2 hover:bg-raised/60">
      <EntryIcon entry={entry} />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <p className="flex flex-wrap items-baseline gap-x-2 text-sm">
          <span className="font-medium">{entry.name}</span>
          {entry.detail !== '' && <span className="text-xs text-muted">{entry.detail}</span>}
        </p>
        {command && <p className="font-mono text-xs break-all text-muted">{entry.command}</p>}
        {!entry.runnable && (
          <p className="text-xs text-muted">
            <span className="sr-only">Can’t run: </span>
            {entry.why}
          </p>
        )}
        {entry.runnable && entry.release && (
          <p className="text-xs text-text">
            {releaseWords}
            {onOpenRelease !== null && (
              <>
                {' · '}
                <button type="button" onClick={onOpenRelease} className="font-medium text-accent hover:underline">
                  Open Release
                </button>
              </>
            )}
          </p>
        )}
        {entry.trust === 'changed' && (
          <Note icon={<AlertIcon className={`${noteIcon} text-warning`} />}>Changed since you last ran it</Note>
        )}
        {entry.background && (
          <Note icon={<ClockIcon className={`${noteIcon} text-muted`} />}>Runs until you stop it.</Note>
        )}
        {entry.note !== '' && <Note icon={<InfoIcon className={`${noteIcon} text-info`} />}>{entry.note}</Note>}
      </div>
      {entry.runnable && (
        <div className="flex shrink-0 items-center gap-1">
          {!entry.release && (
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
          <MenuButton
            label={`More for ${entry.name}`}
            items={() => scriptMenu(menu)}
            className="flex size-7 items-center justify-center rounded-md text-muted transition-colors hover:bg-raised hover:text-text"
          >
            <MoreIcon className="size-4" />
          </MenuButton>
        </div>
      )}
    </li>
  )
}
