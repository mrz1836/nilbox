import { useEffect, useEffectEvent, useId, useRef, useState, type CSSProperties, type ReactNode } from 'react'

import { brand, palettes } from '../generated/brand'
import { prefersReducedMotion } from '../lib/motion'
import {
  arrivalBurst,
  earnedBurst,
  earnedQuip,
  nextQuip,
  pickQuip,
  replayBurst,
  replayLabel,
  type NilStats,
} from '../lib/nil'
import { formatWhen } from '../lib/time'
import { useNowAs } from '../lib/useNow'
import { Confetti, useConfetti, type Point } from './Confetti'
import { PlayIcon, TrophyIcon } from './icons'
import { LogoMark } from './Logo'

export interface InboxNilProps {
  // stats are what was set aside to get there, from the inbox's triage.
  stats: NilStats
  // earned is true when the inbox emptied while the page was open, which throws the full
  // party; opening the page at nil only sprinkles.
  earned: boolean
}

// terminalTokens set the terminal in the dark palette in either theme, as terminals are.
// Every utility and style inside reads the theme's --color-* tokens, so overriding them
// here is enough. color-scheme alone would not do: the bundle's tokens resolve once, on
// the root, for the page's theme.
const terminalTokens = {
  colorScheme: 'dark',
  ...Object.fromEntries(Object.entries(palettes.dark).map(([token, value]) => [`--color-${token}`, value])),
} as CSSProperties

// lists are what the terminal checks off, by the names the inbox gives them.
const lists = ['review requested', 'authored', 'assigned'] as const

// command is what the terminal runs.
const command = `${brand.slug} status`

// lineStyle staggers a terminal line's entrance after the command is typed.
function lineStyle(at: number): CSSProperties {
  return { '--i': String(at) } as CSSProperties
}

interface RowProps {
  at: number
  label: string
  count: number
  note: ReactNode
}

// Row is one of the status lines: a label, a dotted leader, a count, and a note.
function Row({ at, label, count, note }: RowProps) {
  return (
    <p className="nil-line flex items-baseline gap-2 pl-4" style={lineStyle(at)}>
      <span className="shrink-0">{label}</span>
      <span aria-hidden="true" className="min-w-4 flex-1 translate-y-[-0.3em] border-b border-dotted border-border" />
      <span className="w-[3ch] shrink-0 text-right font-semibold tabular-nums">{count}</span>
      <span className="w-[30ch] min-w-0 shrink truncate">{note}</span>
    </p>
  )
}

// center is the middle of an element in the window, or a spot near the top middle of the
// window when there is no element to throw from.
function center(el: Element | null): Point {
  if (el === null) {
    return { x: window.innerWidth / 2, y: window.innerHeight / 3 }
  }

  const r = el.getBoundingClientRect()

  return { x: r.left + r.width / 2, y: r.top + r.height / 2 }
}

// InboxNil is the party the inbox throws when every list is empty: the mark, pulsing, over
// a drifting grid; "inbox == nil" in glitching, shimmering type; a one-liner; a terminal
// that checks off every list, counts what was set aside, and panics with nothing left to
// do; and a button that runs the party again, a little harder each time. Confetti flies on
// arrival, and far more of it when the inbox empties while you watch. With reduced motion
// everything holds still and no confetti flies.
export function InboxNil({ stats, earned }: InboxNilProps) {
  const headingId = useId()
  // word is where the party goes off, and button where a replay does.
  const word = useRef<HTMLSpanElement>(null)
  const button = useRef<HTMLButtonElement>(null)
  const { bursts, fire, land } = useConfetti()
  const [runs, setRuns] = useState(0)
  const [quip, setQuip] = useState(() => (earned ? earnedQuip : pickQuip()))
  const { nextBack } = stats
  const comeback = useNowAs((now) => (nextBack === null ? '' : formatWhen(now, nextBack)))

  const throwConfetti = (from: Element | null, count: number, flash: boolean) => {
    if (!prefersReducedMotion()) {
      fire(center(from), count, flash)
    }
  }

  // The party starts once, as the page reaches nil; a remount in development does not
  // start it twice.
  const started = useRef(false)
  const start = useEffectEvent(() => {
    throwConfetti(word.current, earned ? earnedBurst : arrivalBurst, earned)
  })

  useEffect(() => {
    if (started.current) {
      return
    }

    started.current = true
    start()
  }, [])

  const replay = () => {
    const next = runs + 1

    setRuns(next)
    setQuip(nextQuip)
    throwConfetti(button.current, replayBurst(next), next >= 3)
  }

  // Each line's place in the entrance: the lists, what was set aside, then, after a beat,
  // the panic.
  let at = lists.length
  const snoozedAt = stats.snoozed > 0 ? at++ : -1
  const dismissedAt = stats.dismissed > 0 ? at++ : -1
  const comebackAt = comeback === '' ? -1 : at++
  const panicAt = at + 1

  return (
    <section
      aria-labelledby={headingId}
      className="relative isolate overflow-hidden rounded-xl border border-border bg-surface px-6 pt-12 pb-10"
      style={{ '--nil-highlight': brand.mark.highlight } as CSSProperties}
    >
      <div aria-hidden="true" className="nil-grid pointer-events-none absolute inset-0 -z-10" />
      <div
        aria-hidden="true"
        className="nil-glow pointer-events-none absolute top-0 left-1/2 -z-10 h-80 w-[40rem] max-w-full -translate-x-1/2"
      />

      <div className="mx-auto flex max-w-2xl flex-col items-center text-center">
        <span aria-hidden="true" className="nil-mark relative grid place-items-center">
          <span className="nil-ring" />
          <span className="nil-ring nil-ring-late" />
          <LogoMark className="relative h-20 w-auto" />
        </span>

        {earned ? (
          <p className="nil-rise mt-8 inline-flex items-center gap-1.5 rounded-full border border-warning/40 bg-warning/10 px-3 py-1 text-xs font-semibold tracking-[0.14em] text-warning uppercase">
            <TrophyIcon className="size-3.5" />
            Achievement unlocked
          </p>
        ) : (
          // The badge repeats the heading for the eye, so assistive technology skips it.
          <p
            aria-hidden="true"
            data-testid="nil-badge"
            className="nil-rise mt-8 inline-flex overflow-hidden rounded-md font-mono text-[11px] leading-5 font-semibold"
          >
            <span className="bg-raised px-2 text-muted">inbox</span>
            <span className="bg-success px-2 text-background">nil</span>
          </p>
        )}

        <h2
          id={headingId}
          className="nil-rise mt-4 font-mono text-5xl font-bold tracking-tight sm:text-6xl"
          style={{ animationDelay: '120ms' }}
        >
          <span>inbox</span> <span className="text-muted">==</span>{' '}
          <span ref={word} className="nil-word">
            nil
          </span>
        </h2>

        <p
          aria-live="polite"
          className="nil-rise mt-5 max-w-md text-base text-pretty text-muted"
          style={{ animationDelay: '220ms' }}
        >
          {quip}
        </p>

        <figure
          aria-label="Terminal"
          className="nil-rise mt-9 w-full max-w-xl overflow-hidden rounded-lg border border-border bg-surface text-left font-mono text-[13px] leading-6 text-text shadow-2xl shadow-black/40"
          style={{ ...terminalTokens, animationDelay: '320ms' }}
        >
          <div className="flex items-center gap-1.5 border-b border-border bg-raised px-3 py-2">
            <span aria-hidden="true" className="size-2.5 rounded-full bg-danger" />
            <span aria-hidden="true" className="size-2.5 rounded-full bg-warning" />
            <span aria-hidden="true" className="size-2.5 rounded-full bg-success" />
            <span className="flex-1 pr-10 text-center text-xs text-muted">~/inbox</span>
          </div>
          <div className="px-4 py-3">
            <p>
              <span className="text-success">$</span>{' '}
              <span className="nil-type" style={{ '--n': String(command.length) } as CSSProperties}>
                {command}
              </span>
            </p>
            {lists.map((label, i) => (
              <Row key={label} at={i} label={label} count={0} note={<span className="text-success">✓ clear</span>} />
            ))}
            {stats.snoozed > 0 && (
              <Row
                at={snoozedAt}
                label="snoozed"
                count={stats.snoozed}
                note={<span className="text-warning">{'// '}they’ll be back</span>}
              />
            )}
            {stats.dismissed > 0 && (
              <Row
                at={dismissedAt}
                label="dismissed"
                count={stats.dismissed}
                note={<span className="text-muted">{'// '}rest in peace</span>}
              />
            )}
            {comeback !== '' && (
              <p className="nil-line pl-4 text-muted" style={lineStyle(comebackAt)}>
                {'// '}next one back {comeback}
              </p>
            )}
            <p className="nil-line mt-3 text-danger" style={lineStyle(panicAt)}>
              panic: runtime error: nothing left to do
            </p>
            <p className="nil-line mt-3 text-muted" style={lineStyle(panicAt + 1)}>
              goroutine 1 [vibing]:
            </p>
            <p className="nil-line text-muted" style={lineStyle(panicAt + 2)}>
              main.(*Developer).TouchGrass(0xc0ffee)
            </p>
            <p className="nil-line pl-8 text-muted" style={lineStyle(panicAt + 3)}>
              ~/outside/grass.go:42 +0x1337
            </p>
            <p className="nil-line mt-3" style={lineStyle(panicAt + 4)}>
              <span className="text-success">exit status 0</span> <span aria-hidden="true" className="nil-cursor" />
            </p>
          </div>
        </figure>

        <button
          ref={button}
          type="button"
          onClick={replay}
          className="nil-rise nil-replay group mt-9 inline-flex items-center gap-2 rounded-lg border border-accent/40 bg-accent/10 px-4 py-2 font-mono text-sm font-medium text-text transition hover:border-accent hover:bg-accent/15 active:scale-[0.97]"
          style={{ animationDelay: '420ms' }}
        >
          <PlayIcon className="size-3.5 text-accent transition-transform group-hover:translate-x-0.5" />
          {replayLabel(runs)}
        </button>
      </div>

      <Confetti bursts={bursts} onLand={land} />
    </section>
  )
}
