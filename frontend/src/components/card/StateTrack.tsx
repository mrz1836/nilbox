import type { ReactNode } from 'react'

import { mergeMethodText } from '../../lib/nextStep'
import { checksSignal, reviewsSignal } from '../../lib/signals'
import type { InboxItem, ItemDetails } from '../../platform'
import { AutopilotIcon, CheckIcon, ClockIcon, ConflictIcon, RereviewIcon, XIcon } from '../icons'
import { StatusDot } from '../StatusDot'

// Look is how a step's circle is drawn: in a color with a faint tint of it behind (ok,
// bad, rereview, held), a still ring (waiting, dashed; none, dotted), a dashed one in the
// success color (auto, for auto-merge), or a warning ring with a moving dot (running).
type Look = 'ok' | 'bad' | 'rereview' | 'held' | 'running' | 'waiting' | 'none' | 'auto'

// looks are each look's circle, spelled out in full so Tailwind finds them. A tint is mixed
// over the surface so the line joining the steps never shows through it.
const looks: Readonly<Record<Look, string>> = {
  ok: 'border-success text-success bg-[color-mix(in_oklab,var(--color-success)_16%,var(--color-surface))]',
  bad: 'border-danger text-danger bg-[color-mix(in_oklab,var(--color-danger)_16%,var(--color-surface))]',
  rereview: 'border-accent text-accent bg-[color-mix(in_oklab,var(--color-accent)_16%,var(--color-surface))]',
  held: 'border-warning text-warning bg-[color-mix(in_oklab,var(--color-warning)_14%,var(--color-surface))]',
  running: 'border-warning/30 bg-surface text-warning',
  waiting: 'border-dashed border-muted/75 bg-surface text-muted/75',
  none: 'border-dotted border-muted/45 bg-surface text-muted/45',
  auto: 'border-dashed border-success bg-surface text-success',
}

const iconClass = 'size-2.5 [stroke-width:2.4]'

// stepClass is a step's circle, before its look.
const stepClass = 'relative grid size-4.75 shrink-0 place-items-center rounded-full border-[1.5px]'

// TrackStep is one step of the track: how it looks, what it shows inside (nothing for a
// ring), and the sentence that says it. state names what it stands for, which data-state
// carries.
interface TrackStep {
  state: string
  look: Look
  icon: ReactNode
  sentence: string
}

// checksStep is the checks' step: the checks signal's kind, and its sentence.
function checksStep(d: ItemDetails): TrackStep {
  const signal = checksSignal(d.checks)

  switch (signal.kind) {
    case 'passed':
      return { state: 'passed', look: 'ok', icon: <CheckIcon className={iconClass} />, sentence: signal.long }
    case 'failed':
      return { state: 'failed', look: 'bad', icon: <XIcon className={iconClass} />, sentence: signal.long }
    case 'awaiting':
      return {
        state: 'awaiting',
        look: 'held',
        icon: (
          <span data-checks-held className="inline-flex">
            <ClockIcon className={iconClass} />
          </span>
        ),
        sentence: signal.long,
      }
    case 'pending':
      return {
        state: 'pending',
        look: 'running',
        icon: (
          <span data-checks-running className="inline-flex">
            <StatusDot color="bg-warning" ping />
          </span>
        ),
        sentence: signal.long,
      }
    case 'none':
      return { state: 'none', look: 'none', icon: null, sentence: signal.long }
  }
}

// reviewStepText says where a pull request's reviews stand: that the author pushed since
// the user asked for changes, before anything else, or else the reviews signal's sentence.
export function reviewStepText(d: ItemDetails): string {
  if (d.pushedSinceReview) {
    const again = d.reviewRequestedAgain ? ', and asked for your review again' : ''

    return `You asked for changes, and the author pushed since${again}`
  }

  return reviewsSignal(d.reviews).long
}

// reviewStep is the review's step: re-review first, then the reviews signal's kind.
function reviewStep(d: ItemDetails): TrackStep {
  const sentence = reviewStepText(d)

  if (d.pushedSinceReview) {
    return { state: 'rereview', look: 'rereview', icon: <RereviewIcon className={iconClass} />, sentence }
  }

  switch (reviewsSignal(d.reviews).kind) {
    case 'approved':
      return { state: 'approved', look: 'ok', icon: <CheckIcon className={iconClass} />, sentence }
    case 'changes':
      return { state: 'changes', look: 'bad', icon: <XIcon className={iconClass} />, sentence }
    case 'waiting':
      return { state: 'waiting', look: 'waiting', icon: null, sentence }
  }
}

// mergeStepText says whether a pull request can merge: its conflicts, its auto-merge, or
// what GitHub has worked out.
export function mergeStepText(d: ItemDetails): string {
  if (d.mergeable === 'CONFLICTING') {
    return 'Conflicts with its base branch'
  }

  if (d.autoMerge !== '') {
    return `Auto-merge is on: ${mergeMethodText(d.autoMerge)}`
  }

  return d.mergeable === 'MERGEABLE' ? 'No conflicts' : 'GitHub is still working out whether it can merge'
}

// mergeStep is the merge's step.
function mergeStep(d: ItemDetails): TrackStep {
  const sentence = mergeStepText(d)

  if (d.mergeable === 'CONFLICTING') {
    return { state: 'conflicts', look: 'bad', icon: <ConflictIcon className={iconClass} />, sentence }
  }

  if (d.autoMerge !== '') {
    return { state: 'auto', look: 'auto', icon: <AutopilotIcon className={iconClass} />, sentence }
  }

  return d.mergeable === 'MERGEABLE'
    ? { state: 'mergeable', look: 'ok', icon: <CheckIcon className={iconClass} />, sentence }
    : { state: 'unknown', look: 'waiting', icon: null, sentence }
}

// ring is a step a draft, or a card whose details haven't arrived, draws as a dotted
// ring, keeping its sentence.
function ring(sentence: string): TrackStep {
  return { state: 'none', look: 'none', icon: null, sentence }
}

const names = ['checks', 'review', 'merge'] as const

// trackOf is a pull request's three steps, and the sentence that says them all. A draft's
// review and merge are dotted rings; while the details load, all three are, and the
// sentence says they aren't read yet.
function trackOf(item: InboxItem): { shown: TrackStep[]; sentence: string } {
  const d = item.details
  const steps =
    d === null
      ? null
      : item.isDraft
        ? [checksStep(d), ring(reviewStepText(d)), ring(mergeStepText(d))]
        : [checksStep(d), reviewStep(d), mergeStep(d)]

  return {
    shown: steps ?? names.map(() => ring('')),
    sentence:
      steps === null ? 'Checks, review, and merge: not read yet' : `${steps.map((s) => s.sentence).join('. ')}.`,
  }
}

// StateTrack is where a pull request stands, as three steps in a row joined by a line:
// its checks, its review, and whether it can merge, each a circle with a mark in its
// color when it says something, and a ring when there is nothing yet to say. A draft shows
// its checks as they are and the other two as dotted rings; while the details load, all
// three are dotted rings, so the card keeps its shape. It has no tooltips, since the
// triage buttons cover it while the pointer is on the card: the peek has the numbers, and
// a screen reader reads each step's sentence.
export function StateTrack({ item }: { item: InboxItem }) {
  const { shown, sentence } = trackOf(item)

  return (
    <div data-guide="track" className="relative flex items-center gap-2.75">
      <span aria-hidden="true" className="absolute inset-x-2.5 top-1/2 h-px -translate-y-1/2 bg-border" />
      {shown.map((step, i) => (
        <span
          key={names[i]}
          aria-hidden="true"
          data-step={names[i]}
          data-state={step.state}
          className={`${stepClass} ${looks[step.look]}`}
        >
          {step.icon}
        </span>
      ))}
      <span className="sr-only">{sentence}</span>
    </div>
  )
}

// KeyLook is a look the card guide's key explains: every one a step of the track takes but
// the dotted ring of nothing to say yet, and auto-merge's bolt.
export type KeyLook = 'ok' | 'bad' | 'rereview' | 'running' | 'held' | 'waiting'

// keyIcons are what a step of each look the key explains shows inside its circle.
const keyIcons: Readonly<Record<KeyLook, ReactNode>> = {
  ok: <CheckIcon className={iconClass} />,
  bad: <XIcon className={iconClass} />,
  rereview: <RereviewIcon className={iconClass} />,
  running: <StatusDot color="bg-warning" ping />,
  held: <ClockIcon className={iconClass} />,
  waiting: null,
}

// TrackSymbol is one of the track's symbols alone, as the card guide's key explains it: a
// step's circle in its look, with what such a step shows inside.
export function TrackSymbol({ look }: { look: KeyLook }) {
  return (
    <span aria-hidden="true" className={`${stepClass} ${looks[look]}`}>
      {keyIcons[look]}
    </span>
  )
}

// miniLooks are each look's dot in a compact card's track, spelled out in full so Tailwind
// finds them: filled for a tick, a cross, or a re-review; half-filled for runs held; a ring
// for running checks, with a ping swelling out of it; dashed while waiting, dotted with
// nothing to say; and a ring in the success color for auto-merge.
const miniLooks: Readonly<Record<Look, string>> = {
  ok: 'border-success bg-success',
  bad: 'border-danger bg-danger',
  rereview: 'border-accent bg-accent',
  held: 'border-warning bg-warning/45',
  running: 'border-warning',
  waiting: 'border-dashed border-muted opacity-80',
  none: 'border-dotted border-muted opacity-35',
  auto: 'border-success',
}

// MiniTrack is a compact card's track: the same three steps as StateTrack, each a small
// dot, with the three sentences in its one tooltip and for a screen reader.
export function MiniTrack({ item }: { item: InboxItem }) {
  const { shown, sentence } = trackOf(item)

  return (
    <span
      role="img"
      aria-label={sentence}
      title={sentence}
      className="relative z-10 inline-flex items-center justify-end gap-1.25"
    >
      {shown.map((step, i) => (
        <span
          key={names[i]}
          aria-hidden="true"
          data-step={names[i]}
          data-state={step.state}
          data-moving={step.look === 'running' || undefined}
          className={`relative block size-2.25 shrink-0 rounded-full border-[1.5px] ${miniLooks[step.look]}`}
        >
          {step.look === 'running' && (
            <span className="absolute -inset-[1.5px] rounded-full bg-warning [animation-duration:1.7s] motion-safe:animate-ping" />
          )}
        </span>
      ))}
    </span>
  )
}
