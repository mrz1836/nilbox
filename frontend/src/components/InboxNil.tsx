import { useEffect, useEffectEvent, useId, useRef, useState, type CSSProperties, type ReactNode } from 'react'

import { brand, palettes } from '../generated/brand'
import { confettiGlyphs } from '../lib/confetti'
import { nilChip, type NilChip } from '../lib/milestones'
import { prefersReducedMotion } from '../lib/motion'
import { earnedBurst, nextQuip, openingQuip, replayBurst, replayLabel, type Flourish, type NilStats } from '../lib/nil'
import { formatWhen } from '../lib/time'
import { inboxLists, listTitles } from '../lib/triage'
import { useClock, useNowAs } from '../lib/useNow'
import type { Celebrations, NilMoment } from '../platform'
import { Confetti, useConfetti, type Point } from './Confetti'
import { ChevronDownIcon, PlayIcon, TrophyIcon } from './icons'
import { LogoMark } from './Logo'

export interface InboxNilProps {
  // stats are what was set aside to get there, from the inbox's triage.
  stats: NilStats
  // earned is true when the inbox emptied while the page was open, rather than being found
  // empty as the page opened.
  earned: boolean
  // moment is what this time at nil was worth (internal/milestones): the streak and the
  // count, what it unlocked, and whether confetti flies for it. null, when it could not be
  // had, shows no chip and throws no confetti.
  moment?: NilMoment | null
  // flourish is the small show the party plays as it starts when it throws no confetti of
  // its own (nextFlourish).
  flourish?: Flourish
  // mode is how the window celebrates, from Settings → Appearance.
  mode?: Celebrations
  // compact shows the party as a strip above the lists (partyCompact): the mark, the
  // heading, the chip, the one-liner, and the button, whose run asks for the party in full
  // (onOpen).
  compact?: boolean
  // onOpen hears the strip's button run the party, which then shows it in full.
  onOpen?: (() => void) | undefined
  // onCollapse folds the full party into its strip, from a button in its corner, which
  // shows only with it.
  onCollapse?: (() => void) | undefined
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
const lists = inboxLists.map((list) => listTitles[list].toLowerCase())

// command is what the terminal runs.
const command = `${brand.slug} status`

// sparks are the glyphs the sparks flourish sends up out of the mark, each with where it
// goes: its drift sideways (--sx), its rise (--sy), its turn (--sr), and its delay (--d),
// spread by its place rather than at random, so every party's sparks are the same.
const sparks = confettiGlyphs.slice(0, 10).map((glyph, i) => ({
  glyph,
  style: {
    '--sx': `${String((i % 2 === 0 ? -1 : 1) * (6 + ((i * 13) % 34)))}px`,
    '--sy': `${String(34 + ((i * 17) % 42))}px`,
    '--sr': `${String(((i % 3) - 1) * 28)}deg`,
    '--d': `${String(900 + i * 85)}ms`,
  } as CSSProperties,
}))

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

interface ChipProps {
  chip: NilChip
  // full sizes the chip for the full party, rather than the strip.
  full: boolean
  // stamp lands it like a rubber stamp, the stamp flourish.
  stamp: boolean
}

// Chip says what the time at nil was worth: an achievement unlocked, with a trophy, its
// title, and how many more came with it, every one named in its tooltip; or a plain note of
// the streak or the nil's number.
function Chip({ chip, full, stamp }: ChipProps) {
  const entrance = stamp ? 'nil-stamp' : full ? 'nil-rise' : ''
  const size = full ? 'mt-8 gap-1.5 px-3 py-1 text-xs' : 'gap-1 px-2 py-0.5 text-[0.625rem]'
  const shape = `${entrance} ${size} inline-flex items-center rounded-full border font-semibold tracking-[0.14em] uppercase`

  if (chip.kind === 'note') {
    return (
      <p data-testid="nil-chip" className={`${shape} border-border bg-raised text-muted`}>
        {chip.text}
      </p>
    )
  }

  return (
    <p
      data-testid="nil-chip"
      title={chip.titles.join(', ')}
      className={`${shape} border-warning/40 bg-warning/10 text-warning`}
    >
      <TrophyIcon className={full ? 'size-3.5' : 'size-3'} />
      Achievement unlocked · {chip.title}
      {chip.more > 0 && <span className="opacity-75"> +{chip.more} more</span>}
    </p>
  )
}

// Sparks are the glyphs the sparks flourish sends up out of the mark it sits in.
function Sparks() {
  return (
    <span aria-hidden="true" data-testid="nil-sparks">
      {sparks.map(({ glyph, style }, i) => (
        <span key={i} className="nil-spark" style={style}>
          {glyph}
        </span>
      ))}
    </span>
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
// a drifting grid; "inbox == nil" in glitching, shimmering type; a chip that says what the
// moment was worth; a one-liner; a terminal that checks off every list, counts what was set
// aside, keeps the streak, and panics with nothing left to do; and a button that runs the
// party again, a little harder each time.
//
// Confetti flies from the heading on its own only for a moment that unlocked an
// achievement, at most once a day (moment.confetti), and only in the Milestones mode. Every
// other party plays one flourish instead, never the same as the party before, and the
// button wiggles a couple of times, offering the confetti. Quiet, the party keeps its chip
// and its flourishes, and throws confetti only from the button. Off, it is a still strip:
// the mark, the heading, and the one-liner, with no chip, flourish, or button. With reduced
// motion everything holds still and no confetti flies.
//
// Compact, it is a strip that gives the page to the lists below it, still the mark, the
// heading, the chip, and the one-liner, with the confetti thrown from it the same; its
// button runs the party in full.
export function InboxNil({
  stats,
  earned,
  moment = null,
  flourish = 'glint',
  mode = 'milestones',
  compact = false,
  onOpen,
  onCollapse,
}: InboxNilProps) {
  const headingId = useId()
  const clock = useClock()
  // word is where the party goes off, and button where a replay does.
  const word = useRef<HTMLSpanElement>(null)
  const button = useRef<HTMLButtonElement>(null)
  const { bursts, fire, land } = useConfetti()
  const [runs, setRuns] = useState(0)
  const [quip, setQuip] = useState(() => openingQuip(earned, moment, clock.getSnapshot()))
  const off = mode === 'off'
  // loud is whether the party throws confetti on its own as it starts.
  const loud = mode === 'milestones' && moment?.confetti === true
  const chip = off ? null : nilChip(moment)
  // played is the flourish the party plays, if any: none with confetti or with
  // celebrations off, and a glint in place of a stamp with no chip to stamp.
  const played: Flourish | null = loud || off ? null : flourish === 'stamp' && chip === null ? 'glint' : flourish
  // nudge wiggles the button on a quiet entrance until the user runs the party.
  const nudge = !loud && runs === 0
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
    if (loud) {
      throwConfetti(word.current, earnedBurst, true)
    }
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

    if (compact) {
      onOpen?.()
    }
  }

  // The heading is the same in both forms; only one shows, so word marks the one there.
  // "nil" sits in a span of its own, which the glitch flourish bursts, leaving the word's
  // own shimmer and glitch to it.
  const heading = (
    <>
      <span>inbox</span> <span className="text-muted">==</span>{' '}
      <span className={played === 'glitch' ? 'nil-burst inline-block' : 'inline-block'}>
        <span ref={word} className="nil-word">
          nil
        </span>
      </span>
    </>
  )

  // logo is the mark, in a span of its own that the hop flourish jumps, holding the sparks.
  const logo = (size: string) => (
    <span className={`relative inline-grid place-items-center ${played === 'hop' ? 'nil-hop' : ''}`}>
      <LogoMark className={`relative ${size} w-auto`} />
      {played === 'sparks' && <Sparks />}
    </span>
  )

  // The glint flourish sweeps across the party once.
  const glint = played === 'glint' && (
    <div aria-hidden="true" data-testid="nil-glint" className="nil-glint pointer-events-none absolute inset-0 -z-10" />
  )
  // The button sits in a span that wiggles, leaving the button's own entrance to it.
  const nudgeClass = nudge ? 'nil-nudge inline-flex' : 'inline-flex'
  const stamp = played === 'stamp'

  if (compact) {
    return (
      <section
        aria-labelledby={headingId}
        data-flourish={played ?? undefined}
        className={`relative isolate overflow-hidden rounded-xl border border-border bg-surface px-5 py-3.5 ${off ? 'nil-still' : ''}`}
        style={{ '--nil-highlight': brand.mark.highlight } as CSSProperties}
      >
        <div aria-hidden="true" className="nil-grid nil-grid-strip pointer-events-none absolute inset-0 -z-10" />
        <div
          aria-hidden="true"
          className="nil-glow pointer-events-none absolute top-1/2 left-0 -z-10 h-40 w-72 -translate-x-1/4 -translate-y-1/2"
        />
        {glint}

        <div className="flex items-center gap-4">
          <span aria-hidden="true" className="nil-mark nil-mark-still grid shrink-0 place-items-center">
            {logo('h-9')}
          </span>

          <div className="nil-rise min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
              <h2 id={headingId} className="font-mono text-2xl font-bold tracking-tight">
                {heading}
              </h2>
              {chip !== null && <Chip chip={chip} full={false} stamp={stamp} />}
            </div>
            <p aria-live="polite" className="mt-0.5 truncate text-sm text-muted">
              {quip}
            </p>
          </div>

          {!off && (
            <span className={`${nudgeClass} shrink-0`}>
              <button
                ref={button}
                type="button"
                onClick={replay}
                className="nil-rise nil-replay group inline-flex items-center gap-2 rounded-lg border border-accent/40 bg-accent/10 px-3 py-1.5 font-mono text-xs font-medium text-text transition hover:border-accent hover:bg-accent/15 active:scale-[0.97]"
                style={{ animationDelay: '120ms' }}
              >
                <PlayIcon className="size-3 text-accent transition-transform group-hover:translate-x-0.5" />
                {replayLabel(runs)}
              </button>
            </span>
          )}
        </div>

        <Confetti bursts={bursts} onLand={land} />
      </section>
    )
  }

  // Each line's place in the entrance: the lists, what was set aside, the streak, then,
  // after a beat, the panic.
  let at = lists.length
  const snoozedAt = stats.snoozed > 0 ? at++ : -1
  const dismissedAt = stats.dismissed > 0 ? at++ : -1
  const comebackAt = comeback === '' ? -1 : at++
  const streakAt = moment === null ? -1 : at++
  const bestAt = moment !== null && moment.best > moment.streak ? at++ : -1
  const countAt = moment !== null && moment.count > 0 ? at++ : -1
  const panicAt = at + 1

  return (
    <section
      aria-labelledby={headingId}
      data-flourish={played ?? undefined}
      className="relative isolate overflow-hidden rounded-xl border border-border bg-surface px-6 pt-12 pb-10"
      style={{ '--nil-highlight': brand.mark.highlight } as CSSProperties}
    >
      <div aria-hidden="true" className="nil-grid pointer-events-none absolute inset-0 -z-10" />
      <div
        aria-hidden="true"
        className="nil-glow pointer-events-none absolute top-0 left-1/2 -z-10 h-80 w-[40rem] max-w-full -translate-x-1/2"
      />
      {glint}

      {onCollapse !== undefined && (
        <button
          type="button"
          onClick={onCollapse}
          aria-label="Collapse the party"
          title="Collapse"
          className="absolute top-3 right-3 rounded-md p-1 text-muted transition-colors hover:bg-raised hover:text-text"
        >
          <ChevronDownIcon className="size-4 rotate-180" />
        </button>
      )}

      <div className="mx-auto flex max-w-2xl flex-col items-center text-center">
        <span aria-hidden="true" className="nil-mark relative grid place-items-center">
          <span className="nil-ring" />
          <span className="nil-ring nil-ring-late" />
          {logo('h-20')}
        </span>

        {chip !== null ? (
          <Chip chip={chip} full stamp={stamp} />
        ) : (
          // The badge repeats the heading for the eye, so assistive technology skips it.
          <p
            aria-hidden="true"
            data-testid="nil-badge"
            className="nil-rise mt-8 inline-flex overflow-hidden rounded-md font-mono text-[0.6875rem] leading-5 font-semibold"
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
          {heading}
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
          className="nil-rise mt-9 w-full max-w-xl overflow-hidden rounded-lg border border-border bg-surface text-left font-mono text-[0.8125rem] leading-6 text-text shadow-2xl shadow-black/40"
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
            {moment !== null && (
              <Row
                at={streakAt}
                label="streak"
                count={moment.streak}
                note={
                  <span className="text-success">
                    {'// '}
                    {streakNote(moment)}
                  </span>
                }
              />
            )}
            {moment !== null && moment.best > moment.streak && (
              <Row
                at={bestAt}
                label="best"
                count={moment.best}
                note={<span className="text-muted">{'// '}to beat</span>}
              />
            )}
            {moment !== null && moment.count > 0 && (
              <Row
                at={countAt}
                label="nil"
                count={moment.count}
                note={<span className="text-accent">{'// '}and counting</span>}
              />
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

        <span className={`${nudgeClass} mt-9`}>
          <button
            ref={button}
            type="button"
            onClick={replay}
            className="nil-rise nil-replay group inline-flex items-center gap-2 rounded-lg border border-accent/40 bg-accent/10 px-4 py-2 font-mono text-sm font-medium text-text transition hover:border-accent hover:bg-accent/15 active:scale-[0.97]"
            style={{ animationDelay: '420ms' }}
          >
            <PlayIcon className="size-3.5 text-accent transition-transform group-hover:translate-x-0.5" />
            {replayLabel(runs)}
          </button>
        </span>
      </div>

      <Confetti bursts={bursts} onLand={land} />
    </section>
  )
}

// streakNote is what the terminal says of the streak: its first day, or the days running,
// a personal best from the third.
function streakNote({ streak, best }: NilMoment): string {
  if (streak <= 1) {
    return 'day one'
  }

  return streak >= 3 && streak === best ? 'days running, a new best' : 'days running'
}
