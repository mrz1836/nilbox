import { useId } from 'react'

import { canSwitch, editable, modeLabel, type StatusLine } from '../../lib/autopilot'
import { formatAgo } from '../../lib/time'
import { toneText } from '../../lib/tone'
import type { AutopilotRoutine } from '../../platform'
import { Switch } from '../controls'
import { CopyIcon, ExportIcon, MoreIcon, RingIcon, RoutineIcon, SparkIcon, TrashIcon } from '../icons'
import { MenuButton, type MenuItem } from '../Menu'
import { RelativeTime } from '../RelativeTime'

// Status is a routine's status line, the time it last ran shown against now.
export function Status({ line }: { line: StatusLine }) {
  return (
    <span className={`block truncate text-xs ${toneText[line.tone]}`} title={line.text}>
      {line.text}
      {line.since !== undefined && (
        <>
          {' '}
          <RelativeTime iso={line.since} show={formatAgo} />
        </>
      )}
    </span>
  )
}

export interface RoutineActions {
  // onSwitch switches a routine on or off; onDryRun puts it in Dry run, or takes it out;
  // onDescribe opens Describe it with the routine attached, to change it with Claude;
  // onDuplicate copies it, onExport writes it to a file, and onDelete deletes it, after
  // asking.
  onSwitch: (routine: AutopilotRoutine, on: boolean) => void
  onDryRun: (routine: AutopilotRoutine, on: boolean) => void
  onDescribe: (routine: AutopilotRoutine) => void
  onDuplicate: (routine: AutopilotRoutine) => void
  onExport: (routine: AutopilotRoutine) => void
  onDelete: (routine: AutopilotRoutine) => void
}

// describeTitle says what Change with Claude… does with a routine, or why it can't here.
export function describeTitle(r: AutopilotRoutine): string {
  return editable(r)
    ? 'Tell Claude Code what to change, and review the change it proposes before it is saved'
    : 'This routine can’t change here'
}

// graduateText says why a routine offers to go on Autopilot: a suggestion, never a change.
export const graduateText =
  'Its last 5 runs you approved all finished cleanly. Put it on Autopilot to let it run without asking; nothing changes until you do.'

// iconClass is a menu item's icon.
const iconClass = 'size-3.5 shrink-0 text-muted'

// routineMenu is a routine's ⋯ menu: Change with Claude…, Duplicate, Export, Dry run (or
// out of it), and Delete. A routine that is broken, or another account's, keeps its mode,
// and one that can't change here can't change with Claude either.
export function routineMenu(r: AutopilotRoutine, actions: RoutineActions): MenuItem[] {
  const dry = r.mode === 'dryRun'
  const held = r.dryRunOnly.length > 0

  return [
    {
      label: 'Change with Claude…',
      icon: <SparkIcon className={iconClass} />,
      title: describeTitle(r),
      disabled: !editable(r),
      onSelect: () => {
        actions.onDescribe(r)
      },
    },
    {
      label: 'Duplicate',
      icon: <CopyIcon className={iconClass} />,
      title: 'Make a copy, switched off, right after it',
      divided: true,
      onSelect: () => {
        actions.onDuplicate(r)
      },
    },
    {
      label: 'Export…',
      icon: <ExportIcon className={iconClass} />,
      title: 'Save it to a file, to keep or to use on another Mac',
      onSelect: () => {
        actions.onExport(r)
      },
    },
    {
      label: dry ? 'Leave Dry run' : 'Dry run',
      icon: <RingIcon className={iconClass} />,
      title: !canSwitch(r)
        ? 'This routine can’t change here'
        : dry
          ? held
            ? 'It has steps this version can only try in Dry run'
            : 'Back to Ask first: its local steps run at once, the rest wait for your OK'
          : 'Record what it would do, and do nothing else',
      disabled: !canSwitch(r) || (dry && held),
      onSelect: () => {
        actions.onDryRun(r, !dry)
      },
    },
    {
      label: 'Delete…',
      icon: <TrashIcon className={iconClass} />,
      danger: true,
      divided: true,
      onSelect: () => {
        actions.onDelete(r)
      },
    },
  ]
}

export interface RoutineListProps {
  routines: readonly AutopilotRoutine[]
  // selected is the routine shown, 0 while Activity is.
  selected: number
  onSelect: (id: number) => void
  // status is each routine's status line.
  status: (routine: AutopilotRoutine) => StatusLine
  // busy is the routine a change is under way for, whose switch waits.
  busy: number
  actions: RoutineActions
}

// RoutineList lists the routines in the user's order: each its name, its status line (its
// mode and what it did, or why it can't run), its switch, and its ⋯ menu. A routine of
// another account is greyed, with whose it is and no switch; a broken one says why, with
// no switch. Selecting one shows it.
export function RoutineList({ routines, selected, onSelect, status, busy, actions }: RoutineListProps) {
  const id = useId()

  return (
    <ul aria-label="Routines" className="flex flex-col gap-0.5">
      {routines.map((r) => {
        const switchable = canSwitch(r)
        const line = status(r)
        const current = r.id === selected

        return (
          <li
            key={r.id}
            className={`group relative flex items-start gap-2 rounded-md px-2 py-1.5 transition-colors hover:bg-raised ${current ? 'bg-raised' : ''} ${r.otherAccount ? 'opacity-60' : ''}`.trim()}
          >
            <RoutineIcon
              className={`mt-0.5 size-3.5 shrink-0 ${r.enabled && switchable ? 'text-accent' : 'text-muted'}`}
            />
            <button
              type="button"
              aria-current={current ? 'page' : undefined}
              onClick={() => {
                onSelect(r.id)
              }}
              className="min-w-0 flex-1 text-left after:absolute after:inset-0"
            >
              <span className="block truncate text-sm font-medium text-text" title={r.name}>
                {r.name}
              </span>
              <Status line={line} />
              {r.figures.graduate && (
                <span className="block truncate text-xs text-accent" title={graduateText}>
                  Put it on Autopilot?
                </span>
              )}
            </button>
            <span className="relative z-10 flex shrink-0 items-center gap-1">
              <MenuButton
                label={`More for ${r.name}`}
                align="right"
                items={() => routineMenu(r, actions)}
                className="flex size-6 items-center justify-center rounded-md text-muted opacity-0 transition-opacity group-focus-within:opacity-100 group-hover:opacity-100 hover:bg-surface hover:text-text aria-expanded:opacity-100"
              >
                <MoreIcon className="size-4" />
              </MenuButton>
              {switchable && (
                <>
                  <label htmlFor={`${id}-${String(r.id)}`} className="sr-only">
                    {r.name}
                  </label>
                  <Switch
                    id={`${id}-${String(r.id)}`}
                    checked={r.enabled}
                    disabled={busy === r.id}
                    title={`${r.enabled ? 'On' : 'Off'} · ${modeLabel(r.mode)}`}
                    onChange={(on) => {
                      actions.onSwitch(r, on)
                    }}
                    className="mt-0.5"
                  />
                </>
              )}
            </span>
          </li>
        )
      })}
    </ul>
  )
}
