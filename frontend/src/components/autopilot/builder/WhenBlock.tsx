import { useId } from 'react'

import { defaultSchedule, newTrigger, omit, problemsAt, watchKinds } from '../../../lib/routineBuilder'
import type { AutopilotEvent, AutopilotScheduleStyle, AutopilotSpec, AutopilotTrigger } from '../../../platform'
import { fieldClass, Select } from '../../controls'
import { Block, Notes, NumberField, type BuilderKit } from './parts'

// onOffOptions are a trigger's yes-or-no options, as the spec spells their keys.
type OnOff = 'waitForChecks' | 'firstToo' | 'ownPushesToo'

// numberOptions are a trigger's number options.
type NumberOption = 'settleMinutes' | 'count' | 'days'

// everyLabels name a schedule's choices, and everyKeys lists them in the order the menu
// offers them.
const everyLabels = { day: 'Every day', weekday: 'Every weekday', week: 'Every week' } as const
const everyKeys: readonly (keyof typeof everyLabels)[] = ['day', 'weekday', 'week']

// dayLabels name the days of a weekly schedule, from Monday.
const dayLabels = {
  mon: 'Monday',
  tue: 'Tuesday',
  wed: 'Wednesday',
  thu: 'Thursday',
  fri: 'Friday',
  sat: 'Saturday',
  sun: 'Sunday',
} as const

// dayKeys lists the days in the order the menu offers them, from Monday.
const dayKeys: readonly (keyof typeof dayLabels)[] = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun']

// ScheduleNote says what a schedule's style does: each match told on its own, or one digest
// with the list, and when a time the Mac slept through runs.
function ScheduleNote({ kit, style }: { kit: BuilderKit; style: AutopilotScheduleStyle | undefined }) {
  const chosen = kit.catalog.scheduleStyles.find((s) => s.id === (style ?? 'each'))

  return (
    <p className="text-xs text-muted">
      {chosen === undefined ? '' : `${chosen.help} `}A time missed while this Mac slept runs once it wakes, the same
      day.
    </p>
  )
}

export interface WhenBlockProps {
  kit: BuilderKit
  spec: AutopilotSpec
  onChange: (trigger: AutopilotTrigger) => void
}

// WhenBlock is a routine's When: its one trigger, and the trigger's own options (wait for
// every check to settle, the first commit too, how many comments, how many quiet days,
// which label, a schedule), each at its default until set.
export function WhenBlock({ kit, spec, onChange }: WhenBlockProps) {
  const id = useId()
  const trigger = spec.trigger
  const info = kit.catalog.triggers.find((t) => t.event === trigger.event)
  const kinds = watchKinds(kit.catalog, spec)
  // A trigger fits when it fires for an item the watch holds; the one chosen is always
  // listed, so a problem with it shows where it is.
  const offered = kit.catalog.triggers.filter(
    (t) => t.event === trigger.event || t.kinds.some((k) => kinds.includes(k)),
  )

  const setOnOff = (key: OnOff, on: boolean, fallback: boolean) => {
    onChange(on === fallback ? omit(trigger, key) : { ...trigger, [key]: on })
  }

  const setNumber = (key: NumberOption, n: number | null) => {
    onChange(n === null ? omit(trigger, key) : { ...trigger, [key]: n })
  }

  return (
    <Block title="When" hint="What makes it fire">
      <div className="flex flex-wrap items-center gap-3">
        <label htmlFor={`${id}-event`} className="sr-only">
          Trigger
        </label>
        <Select<AutopilotEvent>
          id={`${id}-event`}
          className={`${fieldClass} py-1`}
          value={trigger.event}
          options={offered.map((t) => ({ value: t.event, label: t.label }))}
          onChange={(event) => {
            onChange(newTrigger(kit.catalog, event))
          }}
        />
        {info?.options.map((o) => {
          const optionId = `${id}-${o.id}`

          if (o.id === 'waitForChecks' || o.id === 'firstToo' || o.id === 'ownPushesToo') {
            const key = o.id

            return (
              <label key={o.id} htmlFor={optionId} className="inline-flex items-center gap-1.5 text-sm">
                <input
                  id={optionId}
                  type="checkbox"
                  className="checkbox"
                  checked={trigger[key] ?? o.default}
                  onChange={(e) => {
                    setOnOff(key, e.target.checked, o.default)
                  }}
                />
                {o.label}
              </label>
            )
          }

          if ((o.id === 'settleMinutes' || o.id === 'count' || o.id === 'days') && o.range !== null) {
            const key = o.id

            return (
              <span key={o.id} className="inline-flex items-center gap-1.5 text-sm text-muted">
                {o.label}
                <NumberField
                  id={optionId}
                  label={o.label}
                  value={trigger[key]}
                  fallback={o.range.default}
                  min={o.range.min}
                  max={o.range.max}
                  unit={o.range.unit}
                  onChange={(n) => {
                    setNumber(key, n)
                  }}
                />
              </span>
            )
          }

          if (o.id === 'label') {
            return (
              <span key={o.id} className="inline-flex items-center gap-1.5">
                <label htmlFor={optionId} className="text-sm text-muted">
                  {o.label}
                </label>
                <input
                  id={optionId}
                  list={`${optionId}-labels`}
                  value={trigger.label ?? ''}
                  spellCheck={false}
                  onChange={(e) => {
                    onChange({ ...trigger, label: e.target.value })
                  }}
                  className={`${fieldClass} w-40 py-1`}
                />
                <datalist id={`${optionId}-labels`}>
                  {kit.suggestions.labels.map((l) => (
                    <option key={l} value={l} />
                  ))}
                </datalist>
              </span>
            )
          }

          // On a schedule, which starts with the schedule it shows stored (newTrigger).
          const schedule = trigger.schedule ?? defaultSchedule

          return (
            <span key={o.id} className="inline-flex flex-wrap items-center gap-1.5">
              <label htmlFor={`${optionId}-every`} className="sr-only">
                How often
              </label>
              <Select
                id={`${optionId}-every`}
                className={`${fieldClass} py-1`}
                value={schedule.every}
                options={everyKeys.map((e) => ({ value: e, label: everyLabels[e] }))}
                onChange={(every) => {
                  onChange({
                    ...trigger,
                    schedule:
                      every === 'week'
                        ? { ...schedule, every, on: schedule.on ?? 'mon' }
                        : omit({ ...schedule, every }, 'on'),
                  })
                }}
              />
              {schedule.every === 'week' && (
                <>
                  <label htmlFor={`${optionId}-on`} className="sr-only">
                    On
                  </label>
                  <Select
                    id={`${optionId}-on`}
                    className={`${fieldClass} py-1`}
                    value={schedule.on ?? 'mon'}
                    options={dayKeys.map((d) => ({ value: d, label: dayLabels[d] }))}
                    onChange={(on) => {
                      onChange({ ...trigger, schedule: { ...schedule, on } })
                    }}
                  />
                </>
              )}
              <label htmlFor={`${optionId}-at`} className="text-sm text-muted">
                at
              </label>
              <input
                id={`${optionId}-at`}
                type="time"
                value={schedule.at}
                onChange={(e) => {
                  onChange({ ...trigger, schedule: { ...schedule, at: e.target.value } })
                }}
                className={`${fieldClass} py-1`}
              />
              <label htmlFor={`${optionId}-style`} className="sr-only">
                How it tells you
              </label>
              <Select<AutopilotScheduleStyle>
                id={`${optionId}-style`}
                className={`${fieldClass} py-1`}
                value={schedule.style ?? 'each'}
                options={kit.catalog.scheduleStyles.map((s) => ({ value: s.id, label: s.label, title: s.help }))}
                onChange={(style) => {
                  // Each match is the default, written as none.
                  onChange({
                    ...trigger,
                    schedule: style === 'each' ? omit(schedule, 'style') : { ...schedule, style },
                  })
                }}
              />
            </span>
          )
        })}
      </div>
      {info !== undefined && (
        <p className="text-xs text-muted">
          Fires when the item {info.help}, once per {info.oncePer}.
        </p>
      )}
      {trigger.event === 'schedule' && <ScheduleNote kit={kit} style={trigger.schedule?.style} />}
      <Notes problems={problemsAt(kit.problems, 'trigger')} />
    </Block>
  )
}
