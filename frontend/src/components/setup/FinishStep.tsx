import { useEffect, useEffectEvent, useRef, type CSSProperties, type RefObject } from 'react'

import { brand, palettes } from '../../generated/brand'
import { prefersReducedMotion } from '../../lib/motion'
import { earnedBurst } from '../../lib/nil'
import { finishLog, finishMarks, goDuration, inboxPreview, logName } from '../../lib/setup'
import type {
  AgentInfo,
  AgentVersion,
  BannerStatus,
  GitHubSignIn,
  IntegrationTools,
  LocationState,
  Settings,
  SetupStep,
  TerminalChoice,
} from '../../platform'
import { Confetti, useConfetti } from '../Confetti'
import { primaryButtonClass } from '../controls'
import { ChevronRightIcon, InboxIcon, TrophyIcon } from '../icons'
import { Spinner } from '../Spinner'
import type { SetupInbox } from './SetupView'

// terminalTokens set the log in the dark palette in either theme, as terminals are, as the
// inbox-nil party's terminal is.
const terminalTokens = {
  colorScheme: 'dark',
  ...Object.fromEntries(Object.entries(palettes.dark).map(([token, value]) => [`--color-${token}`, value])),
} as CSSProperties

// command is what the log runs.
const command = `${brand.slug} setup`

// markClass colors each kind of line's mark.
const markClass: Readonly<Record<'ok' | 'warn' | 'skip', string>> = {
  ok: 'text-success',
  warn: 'text-warning',
  skip: 'text-muted',
}

// lineStyle staggers a log line's entrance after the command is typed.
function lineStyle(at: number): CSSProperties {
  return { '--i': String(at) } as CSSProperties
}

export interface FinishStepProps {
  // headingId and headingRef are the setup's heading, which this step draws.
  headingId: string
  headingRef: RefObject<HTMLHeadingElement | null>
  settings: Settings | null
  github: GitHubSignIn | undefined
  root: LocationState | undefined
  agents: readonly AgentInfo[]
  versions: readonly AgentVersion[]
  terminals: readonly TerminalChoice[] | null
  banners: BannerStatus | ''
  // tools are the tools as listed when the Tools step showed, or null when it didn't.
  tools: IntegrationTools | null
  inbox: SetupInbox
  // elapsed is how long the setup took, in milliseconds.
  elapsed: number
  // onFix goes back to the step a line that needs attention names; onDone opens the inbox.
  onFix: (step: SetupStep) => void
  onDone: () => void
}

// InboxPreview says what the inbox holds by now: its lists' counts once the first refresh
// is in, that it is filling while it runs, or that GitHub must be connected to fill it.
function InboxPreview({ inbox, connected }: { inbox: SetupInbox; connected: boolean }) {
  const preview = inboxPreview(inbox.snapshot)

  let text = preview

  if (!connected) {
    text = 'Connect GitHub to fill it.'
  } else if (preview === '') {
    text = 'Filling your inbox…'
  }

  return (
    <p role="status" className="flex items-center gap-2 text-sm text-muted">
      {connected && preview === '' ? <Spinner /> : <InboxIcon className="size-4 text-accent" />}
      {text}
    </p>
  )
}

// FinishStep ends the setup: an achievement, confetti thrown from the heading, a log in a
// terminal that checks off each part of the setup as go test would, with a Fix beside what
// needs attention, what the inbox holds by now, a few tips, and Open my inbox. With
// reduced motion nothing moves and no confetti flies.
export function FinishStep({
  headingId,
  headingRef,
  settings,
  github,
  root,
  agents,
  versions,
  terminals,
  banners,
  tools,
  inbox,
  elapsed,
  onFix,
  onDone,
}: FinishStepProps) {
  const { bursts, fire, land } = useConfetti()
  const terminal = terminals?.find((t) => t.id === settings?.terminal)
  const lines = finishLog({
    github: github,
    cloneRoot: settings?.cloneRoot ?? '',
    root: root,
    locations: settings?.cloneLocations.length ?? 0,
    agents: agents,
    versions: versions,
    defaultAgent: settings?.defaultAgent ?? '',
    terminal: settings?.terminal ?? '',
    terminalName: terminal?.name ?? '',
    worktrees: settings?.worktreeDefault ?? false,
    banners: banners,
    menuBar: settings?.menuBar ?? false,
    tools: tools === null ? null : tools.tools.filter((t) => t.state !== 'missing').map((t) => t.name),
  })
  const warnings = lines.filter((l) => l.mark === 'warn').length
  const agentName = agents.find((a) => a.id === settings?.defaultAgent)?.name ?? 'your agent'

  // The confetti goes off once, from the heading, as the step shows.
  const thrown = useRef(false)
  const throwConfetti = useEffectEvent(() => {
    const heading = headingRef.current

    if (heading === null || prefersReducedMotion()) {
      return
    }

    const r = heading.getBoundingClientRect()

    fire({ x: r.left + r.width / 2, y: r.top + r.height / 2 }, earnedBurst, true)
  })

  useEffect(() => {
    if (!thrown.current) {
      thrown.current = true
      throwConfetti()
    }
  }, [])

  return (
    <section className="flex flex-col items-center text-center">
      <p className="nil-rise inline-flex items-center gap-1.5 rounded-full border border-warning/40 bg-warning/10 px-3 py-1 text-xs font-semibold tracking-[0.14em] text-warning uppercase">
        <TrophyIcon className="size-3.5" />
        Achievement unlocked
      </p>
      <h2
        ref={headingRef}
        id={headingId}
        tabIndex={-1}
        className="nil-rise mt-4 text-3xl font-bold tracking-tight outline-none"
        style={{ animationDelay: '100ms' }}
      >
        You’re all set
      </h2>

      <figure
        aria-label="Setup log"
        className="nil-rise mt-8 w-full overflow-hidden rounded-lg border border-border bg-surface text-left font-mono text-[0.8125rem] leading-6 text-text shadow-2xl shadow-black/40"
        style={{ ...terminalTokens, animationDelay: '200ms' }}
      >
        <div className="flex items-center gap-1.5 border-b border-border bg-raised px-3 py-2">
          <span aria-hidden="true" className="size-2.5 rounded-full bg-danger" />
          <span aria-hidden="true" className="size-2.5 rounded-full bg-warning" />
          <span aria-hidden="true" className="size-2.5 rounded-full bg-success" />
          <span className="flex-1 pr-10 text-center text-xs text-muted">~/{brand.slug}</span>
        </div>
        <div className="px-4 py-3">
          <p>
            <span className="text-success">$</span>{' '}
            <span className="nil-type" style={{ '--n': String(command.length) } as CSSProperties}>
              {command}
            </span>
          </p>
          {lines.map((line, i) => (
            <p key={line.name} className="nil-line flex items-baseline gap-3" style={lineStyle(i)}>
              <span className={`w-3 shrink-0 ${markClass[line.mark]}`}>{finishMarks[line.mark]}</span>
              <span className="shrink-0 whitespace-pre text-muted">{logName(line.name)}</span>
              <span className="min-w-0 flex-1 truncate">{line.text}</span>
              {line.mark === 'warn' && (
                <button
                  type="button"
                  aria-label={`Fix ${line.name}`}
                  onClick={() => {
                    onFix(line.step)
                  }}
                  className="shrink-0 rounded px-1.5 text-xs font-medium text-warning hover:bg-warning/15"
                >
                  Fix
                </button>
              )}
            </p>
          ))}
          <p className="nil-line mt-2" style={lineStyle(lines.length + 1)}>
            <span className={warnings === 0 ? 'text-success' : 'text-warning'}>ok</span>
            <span className="whitespace-pre">{`   ${brand.slug}/setup   ${goDuration(elapsed)}`}</span>
            {warnings > 0 && <span className="text-warning">{`   (${String(warnings)} to fix)`}</span>}{' '}
            <span aria-hidden="true" className="nil-cursor" />
          </p>
        </div>
      </figure>

      <div className="nil-rise mt-6 flex w-full flex-col items-center gap-4" style={{ animationDelay: '300ms' }}>
        <InboxPreview inbox={inbox} connected={github?.state === 'ready'} />
        <ul aria-label="Tips" className="flex flex-wrap justify-center gap-x-5 gap-y-1 text-xs text-muted">
          <li>
            <kbd className="font-mono">⌘F</kbd> filters the inbox
          </li>
          <li>
            <kbd className="font-mono">⌘,</kbd> opens Settings
          </li>
          <li>Open in {agentName} starts a session from any card</li>
        </ul>
        <button type="button" onClick={onDone} className={`${primaryButtonClass} mt-2 px-5 py-2 text-base`}>
          Open my inbox
          <ChevronRightIcon className="size-4" />
        </button>
      </div>

      <Confetti bursts={bursts} onLand={land} />
    </section>
  )
}
