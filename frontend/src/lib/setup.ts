// The first-run setup: its steps and their order, where the user stands in them, whether
// the Tools step fits, and the words the frame and the Finish step show. Everything here
// is pure, so the frame, the rail, and every step read the same answers, and the tests hold
// them.

import { brand } from '../generated/brand'
import type {
  AgentInfo,
  AgentVersion,
  BannerStatus,
  GitHubSignIn,
  InboxSnapshot,
  IntegrationTools,
  LocationState,
  SetupSignals,
  SetupStep,
  TerminalID,
} from '../platform'
import { counted, formatCount } from './format'

// setupParam is the query parameter of the page's URL that says the setup shows, as
// setupValue (internal/desktop's SetupParam and SetupValue). The page reads it before its
// first render, so its first frame is the setup's, never a flash of the inbox; the stored
// state, read next, is what counts.
export const setupParam = 'setup'
export const setupValue = '1'

// setupFromURL reports whether a page URL's query string says the setup shows.
export function setupFromURL(search: string): boolean {
  return new URLSearchParams(search).get(setupParam) === setupValue
}

// urlWithoutSetup returns the URL href without the setup parameter, or null when it has
// none: once the setup ends, a reload must not open it again.
export function urlWithoutSetup(href: string): string | null {
  const url = new URL(href)

  if (!url.searchParams.has(setupParam)) {
    return null
  }

  url.searchParams.delete(setupParam)

  return url.toString()
}

// StepInfo is how a step names itself: on the rail, as its heading, and in its one-line
// lead.
export interface StepInfo {
  label: string
  title: string
  lead: string
}

// stepInfo is every step's names.
export const stepInfo: Readonly<Record<SetupStep, StepInfo>> = {
  welcome: { label: 'Welcome', title: `Welcome to ${brand.name}`, lead: brand.tagline },
  github: {
    label: 'Connect GitHub',
    title: 'Connect GitHub',
    lead: `${brand.name} reads GitHub through the GitHub CLI you already use. It borrows gh’s token, keeps it in memory, and never stores it.`,
  },
  clones: {
    label: 'Find your clones',
    title: 'Where do your clones live?',
    lead: 'Launches, worktrees, and the Scripts pages look one level inside your clone root, and in any other folders you add.',
  },
  agents: {
    label: 'Pick your agent',
    title: 'Pick your agent',
    lead: 'Choose the coding agent a launch starts, and the terminal it opens in.',
  },
  notifications: {
    label: 'Stay in the loop',
    title: 'Stay in the loop',
    lead: `Choose how ${brand.name} tells you that something needs you.`,
  },
  tools: {
    label: 'Add your tools',
    title: 'Add your tools',
    lead: 'mage-x releases your repositories in two clicks, and go-pre-commit runs your checks before each commit.',
  },
  finish: { label: 'Finish', title: 'You’re all set', lead: '' },
}

// stepsOf is every step in order: Welcome, the numbered steps, Tools only when it fits,
// and Finish.
export function stepsOf(tools: boolean): SetupStep[] {
  return ['welcome', ...numberedSteps(tools), 'finish']
}

// numberedSteps are the steps that count, Welcome and Finish aside.
export function numberedSteps(tools: boolean): SetupStep[] {
  return ['github', 'clones', 'agents', 'notifications', ...(tools ? (['tools'] as const) : [])]
}

// nextStep is the step after step, or step itself at the end.
export function nextStep(step: SetupStep, tools: boolean): SetupStep {
  const steps = stepsOf(tools)

  return steps[Math.min(steps.indexOf(step) + 1, steps.length - 1)] ?? 'finish'
}

// previousStep is the step before step, or step itself at the start.
export function previousStep(step: SetupStep, tools: boolean): SetupStep {
  const steps = stepsOf(tools)

  return steps[Math.max(steps.indexOf(step) - 1, 0)] ?? 'welcome'
}

// Progress is where a step stands among the numbered steps: its number (0 for Welcome,
// one past the last for Finish), how many there are, and how many are left after it.
export interface Progress {
  number: number
  total: number
  left: number
}

// progressOf is where step stands.
export function progressOf(step: SetupStep, tools: boolean): Progress {
  const numbered = numberedSteps(tools)
  const total = numbered.length

  if (step === 'welcome') {
    return { number: 0, total, left: total }
  }

  if (step === 'finish' || !numbered.includes(step)) {
    return { number: total + 1, total, left: 0 }
  }

  const number = numbered.indexOf(step) + 1

  return { number, total, left: total - number }
}

// welcomeText says what lies ahead: "4 quick steps · about 2 minutes".
export function welcomeText(tools: boolean): string {
  const total = numberedSteps(tools).length

  return `${String(total)} quick steps · about ${String(tools ? 3 : 2)} minutes`
}

// progressText is the top bar's words for step: "Getting started" on Welcome, whose own
// page says what lies ahead, "Step 2 of 5 · 3 to go", "Step 5 of 5 · last one", or, at the
// end, "All 5 steps done".
export function progressText(step: SetupStep, tools: boolean): string {
  const { number, total, left } = progressOf(step, tools)

  if (number === 0) {
    return 'Getting started'
  }

  if (number > total) {
    return `All ${String(total)} steps done`
  }

  return `Step ${String(number)} of ${String(total)} · ${left === 0 ? 'last one' : `${String(left)} to go`}`
}

// eyebrowText is the line over a numbered step's heading: "STEP 2 OF 5", or '' for
// Welcome and Finish.
export function eyebrowText(step: SetupStep, tools: boolean): string {
  const { number, total } = progressOf(step, tools)

  return number === 0 || number > total ? '' : `Step ${String(number)} of ${String(total)}`
}

// announceText is what assistive technology hears as a step shows: "Step 2 of 5: Find
// your clones", or the step's heading for Welcome and Finish.
export function announceText(step: SetupStep, tools: boolean): string {
  const eyebrow = eyebrowText(step, tools)

  return eyebrow === '' ? stepInfo[step].title : `${eyebrow}: ${stepInfo[step].label}`
}

// StepStatus is how the rail shows a step: done, the one shown, still ahead, reached but
// needing attention, or, for Tools ahead, suggested.
export type StepStatus = 'done' | 'current' | 'upcoming' | 'attention' | 'suggested'

// stepStatus is how the rail shows step, while current shows, given the steps reached so
// far and those that need attention.
export function stepStatus(
  step: SetupStep,
  current: SetupStep,
  reached: ReadonlySet<SetupStep>,
  attention: ReadonlySet<SetupStep>,
): StepStatus {
  if (step === current) {
    return 'current'
  }

  if (reached.has(step)) {
    return attention.has(step) ? 'attention' : 'done'
  }

  return step === 'tools' ? 'suggested' : 'upcoming'
}

// reachedThrough is every step up to and including step, as reached when the setup opens
// on it: a quit part way through comes back with the steps before it done.
export function reachedThrough(step: SetupStep, tools: boolean): Set<SetupStep> {
  const steps = stepsOf(tools)

  return new Set(steps.slice(0, Math.max(steps.indexOf(step), 0) + 1))
}

// toolInstalled reports whether the tool with id was found on this Mac.
function toolInstalled(tools: IntegrationTools | null, id: string): boolean {
  return tools?.tools.some((t) => t.id === id && t.state !== 'missing') ?? false
}

// installedTools names the tools found on this Mac, in the list's order.
function installedTools(tools: IntegrationTools | null): string[] {
  return (tools?.tools ?? []).filter((t) => t.state !== 'missing').map((t) => t.name)
}

// toolsFit reports whether the Tools step fits: mage-x or go-pre-commit is on this Mac,
// or a clone uses one of them, runs go-fortress, or is a Go module.
export function toolsFit(tools: IntegrationTools | null, signals: SetupSignals | null): boolean {
  if (toolInstalled(tools, 'mage-x') || toolInstalled(tools, 'go-pre-commit')) {
    return true
  }

  return signals !== null && signals.mageX + signals.goPreCommit + signals.fortress + signals.goModules > 0
}

// signalParts are the clones' signs, each with how a sentence says a count of them: the
// verb for one clone, and for more.
const signalParts: readonly {
  key: keyof Omit<SetupSignals, 'clones'>
  one: string
  many: string
}[] = [
  { key: 'mageX', one: 'uses mage-x', many: 'use mage-x' },
  { key: 'goPreCommit', one: 'uses go-pre-commit', many: 'use go-pre-commit' },
  { key: 'fortress', one: 'runs go-fortress', many: 'run go-fortress' },
  { key: 'goModules', one: 'is a Go module', many: 'are Go modules' },
]

// toolsReason says why the Tools step is suggested: "Suggested because mage-x is
// installed · 12 clones use mage-x · 9 use go-pre-commit · 30 are Go modules.", or '' when
// nothing suggests it.
export function toolsReason(tools: IntegrationTools | null, signals: SetupSignals | null): string {
  const parts: string[] = []
  const installed = installedTools(tools)

  if (installed.length > 0) {
    parts.push(`${installed.join(' and ')} ${installed.length === 1 ? 'is' : 'are'} installed`)
  }

  let first = true

  for (const { key, one, many } of signalParts) {
    const n = signals?.[key] ?? 0

    if (n === 0) {
      continue
    }

    const subject = first ? counted(n, 'clone', 'clones') : formatCount(n)

    parts.push(`${subject} ${n === 1 ? one : many}`)
    first = false
  }

  return parts.length === 0 ? '' : `Suggested because ${parts.join(' · ')}.`
}

// githubNeedsAttention reports a sign-in read that isn't ready.
export function githubNeedsAttention(github: GitHubSignIn | undefined): boolean {
  return github !== undefined && github.state !== 'ready'
}

// rootNeedsAttention reports a clone root that isn't there, or can't be read.
export function rootNeedsAttention(root: LocationState | undefined): boolean {
  return root !== undefined && root.state !== 'ok' && root.state !== 'nowAClone'
}

// agentsNeedAttention reports that no coding agent is installed.
export function agentsNeedAttention(agents: readonly AgentInfo[]): boolean {
  return agents.length > 0 && !agents.some((a) => a.installed)
}

// agentRepos are the coding agents' GitHub repositories, where each is installed from.
export const agentRepos: Readonly<Partial<Record<string, string>>> = {
  claude: 'https://github.com/anthropics/claude-code',
  codex: 'https://github.com/openai/codex',
}

// FinishLine is one line of the Finish step's log: whether the part is set (ok), needs
// attention (warn), or was left out (skip); the part it is about, padded as a column; what
// it found; and the step a Fix opens.
export interface FinishLine {
  mark: 'ok' | 'warn' | 'skip'
  name: string
  text: string
  step: SetupStep
}

// FinishFacts is what the Finish step's log reports.
export interface FinishFacts {
  github: GitHubSignIn | undefined
  cloneRoot: string
  root: LocationState | undefined
  locations: number
  agents: readonly AgentInfo[]
  versions: readonly AgentVersion[]
  defaultAgent: string
  terminal: TerminalID | ''
  terminalName: string
  worktrees: boolean
  banners: BannerStatus | ''
  menuBar: boolean
  // tools is null when the Tools step didn't show, or else the tools found on this Mac.
  tools: readonly string[] | null
}

// githubLine is the log's line for the sign-in.
function githubLine(github: GitHubSignIn | undefined): FinishLine {
  if (github === undefined) {
    return { mark: 'skip', name: 'github', text: 'not checked', step: 'github' }
  }

  if (github.state !== 'ready') {
    return { mark: 'warn', name: 'github', text: 'not connected yet', step: 'github' }
  }

  const scopes = github.unverified ? 'scopes not reported' : github.scopes.join(', ')

  return { mark: 'ok', name: 'github', text: `@${github.login} · ${scopes}`, step: 'github' }
}

// clonesLine is the log's line for the clone root and the other locations.
function clonesLine(f: FinishFacts): FinishLine {
  const others = f.locations > 0 ? ` + ${counted(f.locations, 'more location', 'more locations')}` : ''

  if (f.root === undefined) {
    return { mark: 'skip', name: 'clones', text: `${f.cloneRoot} not checked`, step: 'clones' }
  }

  if (rootNeedsAttention(f.root)) {
    return { mark: 'warn', name: 'clones', text: `${f.cloneRoot} not found${others}`, step: 'clones' }
  }

  const held = f.root.clones === 0 ? `${f.cloneRoot}, empty for now` : `${formatCount(f.root.clones)} in ${f.cloneRoot}`

  return { mark: 'ok', name: 'clones', text: `${held}${others}`, step: 'clones' }
}

// agentsLine is the log's line for the coding agents.
function agentsLine(f: FinishFacts): FinishLine {
  const installed = f.agents.filter((a) => a.installed)

  if (installed.length === 0) {
    return { mark: 'warn', name: 'agents', text: 'no coding agent found', step: 'agents' }
  }

  const text = installed
    .map((a) => {
      const version = f.versions.find((v) => v.agent === a.id)?.version ?? ''

      return `${a.name}${version === '' ? '' : ` ${version}`}${a.id === f.defaultAgent ? ' (default)' : ''}`
    })
    .join(' · ')

  return { mark: 'ok', name: 'agents', text, step: 'agents' }
}

// finishLog is the Finish step's log, a line for each part of the setup.
export function finishLog(f: FinishFacts): FinishLine[] {
  const banners = f.banners === 'on' ? 'banners on' : f.banners === '' ? 'banners not checked' : 'banners off'
  const lines: FinishLine[] = [
    githubLine(f.github),
    clonesLine(f),
    agentsLine(f),
    {
      mark: 'ok',
      name: 'terminal',
      text: `${f.terminalName === '' ? f.terminal : f.terminalName} · worktrees ${f.worktrees ? 'on' : 'off'}`,
      step: 'agents',
    },
    { mark: 'ok', name: 'notify', text: `${banners} · menu bar ${f.menuBar ? 'on' : 'off'}`, step: 'notifications' },
  ]

  if (f.tools !== null) {
    lines.push(
      f.tools.length === 0
        ? { mark: 'skip', name: 'tools', text: 'skipped', step: 'tools' }
        : { mark: 'ok', name: 'tools', text: f.tools.join(', '), step: 'tools' },
    )
  }

  return lines
}

// finishMarks are the marks the log prints for each kind of line.
export const finishMarks: Readonly<Record<FinishLine['mark'], string>> = { ok: '✓', warn: '⚠', skip: '–' }

// logName pads a part's name into the log's column.
export function logName(name: string): string {
  return name.padEnd(9, ' ')
}

// goDuration writes a duration in milliseconds as Go's time.Duration does, to the second:
// "42s", "1m42s", or "1h2m3s".
export function goDuration(ms: number): string {
  const total = Math.max(0, Math.round(ms / 1000))
  const hours = Math.floor(total / 3600)
  const minutes = Math.floor((total % 3600) / 60)
  const seconds = total % 60

  if (hours > 0) {
    return `${String(hours)}h${String(minutes)}m${String(seconds)}s`
  }

  return minutes > 0 ? `${String(minutes)}m${String(seconds)}s` : `${String(seconds)}s`
}

// inboxPreview says what the inbox holds: "5 review requests · 7 of yours", or '' before
// its first refresh.
export function inboxPreview(snapshot: InboxSnapshot | null): string {
  if (snapshot === null || snapshot.lastRefreshedAt === '') {
    return ''
  }

  return [
    counted(snapshot.reviewRequested.total, 'review request', 'review requests'),
    `${formatCount(snapshot.yours.total)} of yours`,
  ].join(' · ')
}
