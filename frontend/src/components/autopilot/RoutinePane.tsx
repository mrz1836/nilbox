import { useId, type ReactNode } from 'react'

import { canSwitch, editable, modeHelp, modeLabels, modeOrder, type StatusLine } from '../../lib/autopilot'
import type { AutopilotMode, AutopilotRoutine } from '../../platform'
import { captionClass, primaryButtonClass, quietButtonClass, Segmented, Switch } from '../controls'
import { MoreIcon, RoutineIcon, SparkIcon } from '../icons'
import { MenuButton } from '../Menu'
import { describeTitle, graduateText, routineMenu, Status, type RoutineActions } from './RoutineList'
import { WeekStats } from './WeekStats'

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
  // builder is the routine itself, edited in place, in place of its sentence; heldText
  // says why it stays in Dry run, when it does.
  builder?: ReactNode
  heldText?: string | undefined
  // onGraduate puts the routine on Autopilot, and onNotNow answers Not now, while it offers
  // to go there.
  onGraduate?: () => void
  onNotNow?: () => void
}

// RoutinePane shows one routine: its name, Change with Claude… (while it can change here),
// switch, mode, and ⋯ menu; its status line; the builder, the routine itself edited in
// place (or, without one, the sentence it reads as and what is wrong with it); then what it
// watches now, as the inbox's rows, and what it did, its own Activity.
export function RoutinePane({
  routine: r,
  status,
  busy,
  actions,
  onMode,
  watching,
  activity,
  builder,
  heldText = 'It has steps this version can only try in Dry run, so it stays there.',
  onGraduate,
  onNotNow,
}: RoutinePaneProps) {
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
          {editable(r) && (
            <button
              type="button"
              title={describeTitle(r)}
              onClick={() => {
                actions.onDescribe(r)
              }}
              className={`${quietButtonClass} shrink-0`}
            >
              <SparkIcon className="size-3.5 text-accent" />
              Change with Claude…
            </button>
          )}
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
        {r.figures.graduate && onGraduate !== undefined && (
          <div
            role="status"
            className="flex flex-wrap items-center gap-3 rounded-md border border-accent/45 bg-accent/10 px-3 py-2 text-sm"
          >
            <span className="min-w-0 flex-1">{graduateText}</span>
            <button type="button" disabled={busy} onClick={onGraduate} className={primaryButtonClass}>
              Put it on Autopilot
            </button>
            {onNotNow !== undefined && (
              <button type="button" disabled={busy} onClick={onNotNow} className={quietButtonClass}>
                Not now
              </button>
            )}
          </div>
        )}
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
              {held ? heldText : modeHelp[r.mode]}
            </p>
          </div>
        )}
        {builder === undefined && r.sentence !== '' && (
          <p className="text-sm leading-relaxed text-text">{r.sentence}</p>
        )}
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
      {builder}
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
      <section aria-labelledby={`${id}-week`} className="flex flex-col gap-2">
        <h3 id={`${id}-week`} className={captionClass}>
          This week
        </h3>
        <WeekStats routineID={r.id} version={weekVersion(r)} />
      </section>
    </div>
  )
}

// weekVersion changes whenever what the routine did may have, so its week is read again:
// its figures, which every match it records moves.
function weekVersion(r: AutopilotRoutine): string {
  const f = r.figures

  return [String(f.today), String(f.waiting), f.lastAt ?? '', r.updatedAt].join('|')
}
