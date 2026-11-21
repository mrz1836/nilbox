import { useEffect, useEffectEvent, useId, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from 'react'

import { brand } from '../../generated/brand'
import { logFailure, type Result } from '../../lib/call'
import { useKeyDown } from '../../lib/keys'
import {
  agentsNeedAttention,
  announceText,
  eyebrowText,
  githubNeedsAttention,
  nextStep,
  numberedSteps,
  previousStep,
  progressOf,
  progressText,
  reachedThrough,
  rootNeedsAttention,
  stepInfo,
  toolsFit,
  type SetupInbox,
} from '../../lib/setup'
import type { Theme } from '../../lib/theme'
import type { IntegrationsState } from '../../lib/useIntegrations'
import { useClock } from '../../lib/useNow'
import { useQuery } from '../../lib/useQuery'
import type { SetupControl } from '../../lib/useSetup'
import {
  agentVersions,
  checkCloneLocations,
  checkGitHub,
  checkGitHubThreadsNow,
  getAppInfo,
  listTerminals,
  notificationBannerStatus,
  setupSignals,
  type AgentInfo,
  type Profile,
  type Settings,
  type SetupStep,
} from '../../platform'
import { ConfirmDialog } from '../ConfirmDialog'
import { buttonClass, primaryButtonClass, quietButtonClass } from '../controls'
import { ChevronLeftIcon, ChevronRightIcon } from '../icons'
import { Logo } from '../Logo'
import { AgentsStep } from './AgentsStep'
import { ClonesStep } from './ClonesStep'
import { FinishStep } from './FinishStep'
import { GitHubStep } from './GitHubStep'
import { NotificationsStep } from './NotificationsStep'
import { SetupRail } from './SetupRail'
import { ToolsStep } from './ToolsStep'
import { WelcomeStep } from './WelcomeStep'

export interface SetupViewProps {
  // control is where the setup stands, and the moves it makes.
  control: SetupControl
  // theme is the theme the window shows, and onThemeChange switches it and stores it.
  theme: Theme
  onThemeChange: (theme: Theme) => Promise<Result<void>>
  // settings are the stored settings, null until they load, and onSaved records what a
  // setter stored and returned.
  settings: Settings | null
  onSaved: (settings: Settings) => void
  // agents and profiles are the coding agents and their profiles, and onLaunchChanged
  // reads them again, with the prompts and launch defaults, as after a backup came back.
  agents: readonly AgentInfo[]
  profiles: readonly Profile[]
  onLaunchChanged: () => Promise<void>
  inbox: SetupInbox
  integrations: IntegrationsState
  // autopilot reports Autopilot (beta) on, and onTemplate sets up the template the last step
  // suggests, in the Autopilot tab; off, the setup says nothing of Autopilot.
  autopilot?: boolean
  onTemplate?: (key: string) => void
}

// interactive matches the elements whose own Enter must not also move the setup on.
const interactive = 'button, a, input, textarea, select, summary, [role="button"], [contenteditable="true"], dialog'

// SetupView is the first-run setup, over the whole window: a draggable top bar with the
// logo, where the user stands, and Skip setup; a bar of segments, one per step; a rail of
// the steps, each reached one a way back to it; and the step itself, its heading, its
// lead, what it sets, and Back and Continue. Enter continues while focus isn't in a field
// or on a control; Escape never skips. Every choice is saved as it is made, through the
// settings' own setters, and the step too, so a quit part way through comes back to it.
// The window beneath is inert while it shows (App).
export default function SetupView(props: SetupViewProps) {
  const { control, settings, agents, integrations } = props
  const headingId = useId()
  const headingRef = useRef<HTMLHeadingElement>(null)
  const clock = useClock()
  const { step } = control

  const info = useQuery(() => getAppInfo(), [], { log: 'could not read the app info' })
  const github = useQuery(() => checkGitHub(), [], { log: 'could not check the GitHub sign-in' })
  const locations = useQuery(
    () => checkCloneLocations(),
    [settings?.cloneRoot ?? '', JSON.stringify(settings?.cloneLocations ?? [])],
    { enabled: settings !== null, log: 'could not check the repository locations' },
  )
  const signals = useQuery(() => setupSignals(), [], { log: 'could not look at the clones' })
  const banners = useQuery(() => notificationBannerStatus(), [], { log: 'could not read the banners' })
  const versions = useQuery(() => agentVersions(), [], { log: 'could not read the agents’ versions' })
  const terminals = useQuery(() => listTerminals(), [], { log: 'could not list the terminals' })

  // The Tools step is decided as the setup opens, once the tools and the clones are known,
  // and checked again as the user leaves Clones, since a new clone root may hold Go
  // repositories. It is added only then, and never taken away once shown; a setup that
  // opens on it shows it.
  const [toolsShown, setToolsShown] = useState(step === 'tools')
  const [settled, setSettled] = useState(false)
  const [recheck, setRecheck] = useState(false)
  const fits = toolsFit(integrations.tools, signals.data ?? null)

  if (!settled && integrations.tools !== null && signals.data !== undefined) {
    setSettled(true)
    setToolsShown(toolsShown || fits)
  }

  if (recheck && !signals.pending) {
    setRecheck(false)
    setToolsShown(toolsShown || fits)
  }

  // reached are the steps the user has been on, each a way back from the rail: every step up
  // to each one shown, the stored step the setup resumes on included, which arrives after
  // the first frame. A setup that resumes on Tools shows it.
  const [reached, setReached] = useState(() => reachedThrough(step, true))
  const [seen, setSeen] = useState(step)

  if (step !== seen) {
    setSeen(step)
    setReached((was) => new Set([...was, ...reachedThrough(step, true)]))
    setToolsShown(toolsShown || step === 'tools')
  }
  const root = locations.data?.find((s) => s.primary)
  const attention = new Set<SetupStep>([
    ...(githubNeedsAttention(github.data) ? (['github'] as const) : []),
    ...(rootNeedsAttention(root) ? (['clones'] as const) : []),
    ...(agentsNeedAttention(agents) ? (['agents'] as const) : []),
  ])

  // The setup's time is measured from its first frame to Finish, as go test measures a
  // package's tests, on the window's clock.
  const started = useRef<number | null>(null)
  const [elapsed, setElapsed] = useState(0)

  useEffect(() => {
    started.current ??= clock.read().getTime()
  }, [clock])

  // The sign-in, once ready the first time, starts filling the inbox and the GitHub
  // notifications, so they are there by the end.
  const filled = useRef(false)
  const fill = useEffectEvent(() => {
    props.inbox.refresh()
    logFailure(checkGitHubThreadsNow(), 'could not read the GitHub notifications')
  })

  useEffect(() => {
    if (github.data?.state === 'ready' && !filled.current) {
      filled.current = true
      fill()
    }
  }, [github.data?.state])

  // Each step shows with its heading focused, so a screen reader starts there and Enter
  // continues.
  useEffect(() => {
    headingRef.current?.focus()
  }, [step])

  const go = (next: SetupStep) => {
    if (next === 'finish') {
      setElapsed(clock.read().getTime() - (started.current ?? clock.read().getTime()))
    }

    if (step === 'clones' && next !== 'clones') {
      setRecheck(true)
      void signals.refetch()
    }

    control.go(next)
  }

  const [skipping, setSkipping] = useState(false)
  const advance = () => {
    if (step === 'finish') {
      control.finish()

      return
    }

    go(nextStep(step, toolsShown))
  }

  // Enter continues from anywhere in the setup that takes no Enter of its own; the keys
  // stop here, so no page beneath, such as Settings run again from, hears them.
  const onKeyDown = (e: ReactKeyboardEvent<HTMLDivElement>) => {
    e.stopPropagation()

    if (e.key !== 'Enter' || e.defaultPrevented || e.nativeEvent.isComposing || e.metaKey || e.ctrlKey) {
      return
    }

    if (e.target instanceof Element && e.target.closest(interactive) !== null) {
      return
    }

    e.preventDefault()
    advance()
  }

  // A key pressed with focus on nothing at all reaches the document, not the setup.
  useKeyDown((e) => {
    if (e.key === 'Enter' && !e.defaultPrevented && !e.isComposing && e.target === document.body && !skipping) {
      e.preventDefault()
      advance()
    }
  })

  const { number, total } = progressOf(step, toolsShown)
  const hero = step === 'welcome' || step === 'finish'
  const names = stepInfo[step]
  const toolsFound = integrations.tools?.tools.some((t) => t.state !== 'missing') ?? false
  // The footer's Continue says what moving on leaves behind: GitHub not connected yet, or
  // the tools left as they are.
  const continueLabel =
    step === 'github' && github.data?.state !== 'ready'
      ? 'Continue without GitHub'
      : step === 'tools' && !toolsFound
        ? 'Skip'
        : 'Continue'

  return (
    // eslint-disable-next-line jsx-a11y/no-noninteractive-element-interactions -- the setup's own keys: Enter continues, and every key stops here, so nothing beneath hears it
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby={headingId}
      onKeyDown={onKeyDown}
      className="fixed inset-0 z-[55] flex flex-col bg-background text-text"
    >
      <div className="flex h-[52px] shrink-0 items-center gap-4 border-b border-border bg-surface pr-3 pl-[80px] select-none [--wails-draggable:drag]">
        <span className="flex shrink-0">
          <Logo />
        </span>
        <p className="min-w-0 truncate text-body font-medium text-muted" data-testid="setup-progress">
          {progressText(step, toolsShown)}
        </p>
        <button
          type="button"
          onClick={() => {
            setSkipping(true)
          }}
          className={`${quietButtonClass} ml-auto shrink-0 [--wails-draggable:no-drag]`}
        >
          Skip setup
        </button>
      </div>

      <div
        role="progressbar"
        aria-label="Setup progress"
        aria-valuemin={0}
        aria-valuemax={total}
        aria-valuenow={Math.min(Math.max(number - 1, 0), total)}
        aria-valuetext={progressText(step, toolsShown)}
        className="flex shrink-0 gap-1 px-4 pt-3"
      >
        {numberedSteps(toolsShown).map((s, i) => (
          <span
            key={s}
            className={`h-1 flex-1 rounded-full transition-colors duration-300 ${
              i + 1 < number ? 'bg-accent' : i + 1 === number ? 'bg-accent/60' : 'bg-border'
            }`}
          />
        ))}
      </div>

      <div className="flex min-h-0 flex-1">
        {!hero && (
          <SetupRail
            steps={numberedSteps(toolsShown)}
            current={step}
            reached={reached}
            attention={attention}
            onGo={go}
          />
        )}
        <div className="min-h-0 flex-1 overflow-y-auto">
          <div className={`mx-auto flex max-w-2xl flex-col px-8 ${hero ? 'py-12' : 'py-10'}`}>
            {!hero && (
              <>
                <p className="text-caption font-semibold tracking-[0.14em] text-accent uppercase">
                  {eyebrowText(step, toolsShown)}
                </p>
                <h2
                  ref={headingRef}
                  id={headingId}
                  tabIndex={-1}
                  className="mt-1.5 text-2xl font-semibold tracking-tight outline-none"
                >
                  {names.title}
                </h2>
                <p className="mt-2 text-sm text-pretty text-muted">{names.lead}</p>
              </>
            )}

            <div className={hero ? '' : 'mt-7'}>
              {step === 'welcome' && (
                <WelcomeStep
                  headingId={headingId}
                  headingRef={headingRef}
                  info={info.data ?? null}
                  rerun={control.rerun}
                  tools={toolsShown}
                  theme={props.theme}
                  onThemeChange={props.onThemeChange}
                  onStart={() => {
                    go('github')
                  }}
                  onSkip={() => {
                    setSkipping(true)
                  }}
                  onRestored={async (savedSettings) => {
                    props.onSaved(savedSettings)
                    await props.onLaunchChanged()
                  }}
                  onReview={() => {
                    go('github')
                  }}
                  onFinishNow={() => {
                    go('finish')
                  }}
                />
              )}
              {step === 'github' && <GitHubStep github={github} />}
              {step === 'clones' && settings !== null && (
                <ClonesStep settings={settings} onSaved={props.onSaved} states={locations} />
              )}
              {step === 'agents' && settings !== null && (
                <AgentsStep
                  settings={settings}
                  onSaved={props.onSaved}
                  agents={agents}
                  profiles={props.profiles}
                  versions={versions.data ?? []}
                  terminals={terminals.data ?? null}
                  onRediscovered={props.onLaunchChanged}
                />
              )}
              {step === 'notifications' && settings !== null && (
                <NotificationsStep settings={settings} onSaved={props.onSaved} banners={banners} />
              )}
              {step === 'tools' && (
                <ToolsStep
                  settings={settings}
                  onSaved={props.onSaved}
                  integrations={integrations}
                  signals={signals.data ?? null}
                />
              )}
              {step === 'finish' && (
                <FinishStep
                  headingId={headingId}
                  headingRef={headingRef}
                  settings={settings}
                  github={github.data}
                  root={root}
                  agents={agents}
                  versions={versions.data ?? []}
                  terminals={terminals.data ?? null}
                  banners={banners.data ?? ''}
                  tools={toolsShown ? integrations.tools : null}
                  inbox={props.inbox}
                  elapsed={elapsed}
                  onFix={go}
                  onDone={control.finish}
                  autopilot={props.autopilot ?? false}
                  {...(props.onTemplate === undefined ? {} : { onTemplate: props.onTemplate })}
                />
              )}
            </div>

            {!hero && (
              <div className="mt-10 flex items-center justify-between gap-4 border-t border-border pt-5">
                <button
                  type="button"
                  onClick={() => {
                    go(previousStep(step, toolsShown))
                  }}
                  className={buttonClass}
                >
                  <ChevronLeftIcon className="size-3.5 text-muted" />
                  Back
                </button>
                <button type="button" onClick={advance} className={primaryButtonClass}>
                  {continueLabel}
                  <ChevronRightIcon className="size-3.5" />
                </button>
              </div>
            )}
          </div>
        </div>
      </div>

      <p aria-live="polite" className="sr-only">
        {announceText(step, toolsShown)}
      </p>

      {skipping && (
        <ConfirmDialog
          title="Skip the setup?"
          confirmLabel="Skip setup"
          tone="primary"
          onConfirm={() => {
            setSkipping(false)
            control.finish()
          }}
          onCancel={() => {
            setSkipping(false)
          }}
        >
          You can run it again from Settings → About. What you set so far is saved. {brand.name} opens on your inbox.
        </ConfirmDialog>
      )}
    </div>
  )
}
