import { useId } from 'react'

import { defaultSchedule, newTrigger, omit, problemsAt, watchKinds } from '../../../lib/routineBuilder'
import type { AutopilotEvent, AutopilotSpec, AutopilotTrigger } from '../../../platform'
import { fieldClass } from '../../controls'
import { Block, Notes, NumberField, type BuilderKit } from './parts'

// onOffOptions are a trigger's yes-or-no options, as the spec spells their keys.
type OnOff = 'waitForChecks' | 'firstToo' | 'ownPushesToo'

// numberOptions are a trigger's number options.
type NumberOption = 'settleMinutes' | 'count' | 'days'

// everyLabels name a schedule's choices.
const everyLabels = { day: 'Every day', weekday: 'Every weekday', week: 'Every week' } as const

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
        <select
          id={`${id}-event`}
          value={trigger.event}
          onChange={(e) => {
            onChange(newTrigger(kit.catalog, e.target.value as AutopilotEvent))
          }}
          className={`${fieldClass} py-1`}
        >
          {offered.map((t) => (
            <option key={t.event} value={t.event}>
              {t.label}
            </option>
          ))}
        </select>
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
              <select
                id={`${optionId}-every`}
                value={schedule.every}
                onChange={(e) => {
                  const every = e.target.value as keyof typeof everyLabels

                  onChange({
                    ...trigger,
                    schedule:
                      every === 'week'
                        ? { ...schedule, every, on: schedule.on ?? 'mon' }
                        : omit({ ...schedule, every }, 'on'),
                  })
                }}
                className={`${fieldClass} py-1`}
              >
                {(Object.keys(everyLabels) as (keyof typeof everyLabels)[]).map((e) => (
                  <option key={e} value={e}>
                    {everyLabels[e]}
                  </option>
                ))}
              </select>
              {schedule.every === 'week' && (
                <>
                  <label htmlFor={`${optionId}-on`} className="sr-only">
                    On
                  </label>
                  <select
                    id={`${optionId}-on`}
                    value={schedule.on ?? 'mon'}
                    onChange={(e) => {
                      onChange({ ...trigger, schedule: { ...schedule, on: e.target.value as keyof typeof dayLabels } })
                    }}
                    className={`${fieldClass} py-1`}
                  >
                    {(Object.keys(dayLabels) as (keyof typeof dayLabels)[]).map((d) => (
                      <option key={d} value={d}>
                        {dayLabels[d]}
                      </option>
                    ))}
                  </select>
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
            </span>
          )
        })}
      </div>
      {info !== undefined && (
        <p className="text-xs text-muted">
          Fires when the item {info.help}, once per {info.oncePer}.
        </p>
      )}
      <Notes problems={problemsAt(kit.problems, 'trigger')} />
    </Block>
  )
}
