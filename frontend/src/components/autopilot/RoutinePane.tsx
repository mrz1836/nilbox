import { useId, type ReactNode } from 'react'

import { canSwitch, modeHelp, modeLabels, modeOrder, type StatusLine } from '../../lib/autopilot'
import type { AutopilotMode, AutopilotRoutine } from '../../platform'
import { captionClass, Segmented, Switch } from '../controls'
import { MoreIcon, RoutineIcon } from '../icons'
import { MenuButton } from '../Menu'
import { routineMenu, Status, type RoutineActions } from './RoutineList'

export interface RoutinePaneProps {
  routine: AutopilotRoutine
  status: StatusLine
  busy: boolean
  actions: RoutineActions
  // onMode sets the routine's mode.
  onMode: (mode: AutopilotMode) => void
  // watching is its Watching list, and activity its own lines of Activity.
  watching: ReactNode
  activity: ReactNode
}

// RoutinePane shows one routine: its name, switch, mode, and ⋯ menu; the sentence it reads
// as, its status line, and what is wrong with it; then what it watches now, as the inbox's
// rows, and what it did, its own Activity.
export function RoutinePane({ routine: r, status, busy, actions, onMode, watching, activity }: RoutinePaneProps) {
  const id = useId()
  const switchable = canSwitch(r)
  const held = r.dryRunOnly.length > 0

  return (
    <div className="flex flex-col gap-6">
      <header className="flex flex-col gap-3">
        <div className="flex items-center gap-3">
          <RoutineIcon className={`size-5 shrink-0 ${r.enabled && switchable ? 'text-accent' : 'text-muted'}`} />
          <h2 id={`${id}-name`} className="min-w-0 flex-1 truncate text-lg font-semibold" title={r.name}>
            {r.name}
          </h2>
          <MenuButton
            label={`More for ${r.name}`}
            align="right"
            items={() => routineMenu(r, actions)}
            className="flex size-7 items-center justify-center rounded-md text-muted hover:bg-raised hover:text-text"
          >
            <MoreIcon className="size-4" />
          </MenuButton>
          {switchable && (
            <>
              <span aria-hidden="true" className="text-sm text-muted">
                {r.enabled ? 'On' : 'Off'}
              </span>
              <label htmlFor={`${id}-on`} className="sr-only">
                Run this routine
              </label>
              <Switch
                id={`${id}-on`}
                checked={r.enabled}
                disabled={busy}
                describedBy={`${id}-name`}
                onChange={(on) => {
                  actions.onSwitch(r, on)
                }}
              />
            </>
          )}
        </div>
        <Status line={status} />
        {switchable && (
          <div className="flex flex-wrap items-center gap-3">
            <Segmented
              label="Mode"
              value={r.mode}
              disabled={busy || held}
              options={modeOrder.map((mode) => ({ value: mode, label: modeLabels[mode], title: modeHelp[mode] }))}
              describedBy={`${id}-mode`}
              onChange={onMode}
            />
            <p id={`${id}-mode`} className="text-xs text-muted">
              {held ? 'It has steps this version can only try in Dry run, so it stays there.' : modeHelp[r.mode]}
            </p>
          </div>
        )}
        {r.sentence !== '' && <p className="text-sm leading-relaxed text-text">{r.sentence}</p>}
        {r.description !== '' && <p className="text-sm text-muted">{r.description}</p>}
        {r.problems.length > 0 && (
          <ul aria-label="What is wrong with it" className="flex flex-col gap-1 rounded-md border border-danger/40 p-3">
            {r.problems.map((p) => (
              <li key={`${p.part}:${p.field}`} className="text-xs text-danger">
                {p.message}
              </li>
            ))}
          </ul>
        )}
      </header>
      <section aria-labelledby={`${id}-watching`} className="flex flex-col gap-2">
        <h3 id={`${id}-watching`} className={captionClass}>
          Watching
        </h3>
        {watching}
      </section>
      <section aria-labelledby={`${id}-activity`} className="flex flex-col gap-2">
        <h3 id={`${id}-activity`} className={captionClass}>
          Activity
        </h3>
        {activity}
      </section>
    </div>
  )
}
