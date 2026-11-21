import { useEffect, useEffectEvent, useId, useRef, type RefObject } from 'react'

import { brand } from '../../generated/brand'
import { throwsBursts, useCelebrations } from '../../lib/celebrations'
import { prefersReducedMotion } from '../../lib/motion'
import { earnedBurst } from '../../lib/nil'
import { finishLog, finishMarks, goDuration, inboxPreview, logName, type SetupInbox } from '../../lib/setup'
import { useQuery } from '../../lib/useQuery'
import { listTemplates } from '../../platform'
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
import { CardGuide } from '../card/CardGuide'
import { Confetti, useConfetti } from '../Confetti'
import { buttonClass, captionClass, primaryButtonClass } from '../controls'
import { ChevronRightIcon, InboxIcon, TrophyIcon } from '../icons'
import { Spinner } from '../Spinner'
import { lineStyle, Terminal } from '../Terminal'

// command is what the log runs.
const command = `${brand.slug} setup`

// markClass colors each kind of line's mark.
const markClass: Readonly<Record<'ok' | 'warn' | 'skip', string>> = {
  ok: 'text-success',
  warn: 'text-warning',
  skip: 'text-muted',
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
  // autopilot reports Autopilot (beta) on, which alone suggests a template, from what this
  // Mac already knows; onTemplate sets it up in the Autopilot tab.
  autopilot?: boolean
  onTemplate?: (key: string) => void
}

// SuggestedTemplate is the template the setup's clones and the inbox suggest, as the last
// step offers it: its name, why, and Set it up, which opens its quick setup.
function SuggestedTemplate({ onTemplate }: { onTemplate: (key: string) => void }) {
  const templates = useQuery(() => listTemplates(), [], { log: 'could not read the templates' })
  const t = templates.data?.find((tmpl) => tmpl.suggested !== '')

  if (t === undefined) {
    return null
  }

  return (
    <section
      aria-label="Suggested for you"
      className="flex w-full max-w-lg items-center gap-3 rounded-lg border border-accent/45 bg-accent/10 px-3 py-2 text-left"
    >
      <span aria-hidden="true" className="text-xl leading-none">
        {t.icon}
      </span>
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="text-sm font-medium text-text">{`Suggested for you: ${t.name}`}</span>
        <span className="text-xs text-muted">{t.suggested}</span>
      </span>
      <button
        type="button"
        onClick={() => {
          onTemplate(t.key)
        }}
        className={buttonClass}
      >
        Set it up
      </button>
    </section>
  )
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
// needs attention, what the inbox holds by now, a few tips, how to read a card (the
// compact card guide), and Open my inbox. With reduced motion nothing moves and no
// confetti flies, and with Celebrations quiet or off none flies either.
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
  autopilot = false,
  onTemplate,
}: FinishStepProps) {
  const guideId = useId()
  const { bursts, fire, land } = useConfetti()
  const celebrations = useCelebrations()
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

  // The confetti goes off once, from the heading, as the step shows, unless the user's
  // celebrations throw no bursts.
  const thrown = useRef(false)
  const throwConfetti = useEffectEvent(() => {
    const heading = headingRef.current

    if (heading === null || prefersReducedMotion() || !throwsBursts(celebrations)) {
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

      <Terminal label="Setup log" title={`~/${brand.slug}`} command={command} className="mt-8" delay={200}>
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
      </Terminal>

      <div className="nil-rise mt-6 flex w-full flex-col items-center gap-4" style={{ animationDelay: '300ms' }}>
        <InboxPreview inbox={inbox} connected={github?.state === 'ready'} />
        {autopilot && onTemplate !== undefined && <SuggestedTemplate onTemplate={onTemplate} />}
        <ul aria-label="Tips" className="flex flex-wrap justify-center gap-x-5 gap-y-1 text-xs text-muted">
          <li>
            <kbd className="font-mono">⌘F</kbd> filters the inbox
          </li>
          <li>
            <kbd className="font-mono">⌘,</kbd> opens Settings
          </li>
          <li>Open in {agentName} starts a session from any card</li>
        </ul>
        <section aria-labelledby={guideId} className="mt-2 w-full text-left">
          <h3 id={guideId} className={`${captionClass} mb-2`}>
            How to read a card
          </h3>
          <CardGuide variant="compact" />
        </section>
        <button type="button" onClick={onDone} className={`${primaryButtonClass} mt-2 px-5 py-2 text-base`}>
          Open my inbox
          <ChevronRightIcon className="size-4" />
        </button>
      </div>

      <Confetti bursts={bursts} onLand={land} />
    </section>
  )
}
