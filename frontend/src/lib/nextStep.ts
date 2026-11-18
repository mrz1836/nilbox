// What an inbox card puts first: whose move it is (the edge), the one sentence that says
// why (the story), the human's next move (the primary verb), and which of the saved launch
// defaults' prompts the agent button beside it runs (the situation). The first rule that
// matches wins. It reads only what the card already has, so it costs no request, and it
// reads no clock: the caller works out every input that depends on the time.

import type { AgentSession, InboxItem } from '../platform'
import { counted, formatCount } from './format'
import { needsYou, whereWords } from './sessions'
import { checksSignal, reviewsSignal } from './signals'
import { wakeSummary, type TriageTab } from './triage'

// Edge is whose move a card is, as the color of its left edge: something waits on the
// user now (urgent), it is stuck (blocked), it is the user's (yours), it is ready to ship
// (ready), an agent is on it (agent), it waits on others (waiting), or none, in the tabs
// an item is set aside in.
export type Edge = 'urgent' | 'blocked' | 'yours' | 'ready' | 'agent' | 'waiting' | 'none'

// Situation picks the launch default's prompt the agent button runs: '' for the item
// kind's own, or one for a pull request's state.
export type Situation = '' | 'checks_failing' | 'conflicts' | 'changes_requested' | 'rereview'

// Verb is the human's next move, the card's primary button.
export type Verb =
  | 'review'
  | 'rereview'
  | 'read'
  | 'open'
  | 'merge'
  | 'decide'
  | 'terminal'
  | 'watch'
  | 'sessions'
  | 'unsnooze'
  | 'restore'
  | 'unmute'

// StoryIcon is the mark before the story's words.
export type StoryIcon = 'dot' | 'ping' | 'clock' | 'bolt' | 'archive'

// Rule names the rule that matched, as the card's data-rule.
export type Rule =
  | 'snoozed'
  | 'dismissed'
  | 'muted'
  | 'session_needs_you'
  | 'autopilot_needs_you'
  | 'session_failed'
  | 'agent_working'
  | 'loading'
  | 'runs_held'
  | 'conflicts'
  | 'checks_failing'
  | 'changes_requested'
  | 'my_draft'
  | 'rereview'
  | 'review_draft'
  | 'review'
  | 'issue'
  | 'ready'
  | 'auto_merge'
  | 'assigned_pr'
  | 'checks_running'
  | 'waiting'

// Story is the card's one sentence: its lead, in the edge's color, and a muted detail after
// " · ", '' for none. In the Snoozed and Dismissed tabs it is triage: the card shows when
// the item comes back, or when it was dismissed, instead.
export interface Story {
  text: string
  detail: string
  icon: StoryIcon
  triage: boolean
}

// Step is what a card puts first. quiet makes the primary a quiet button, outlined in the
// border color rather than the edge's.
export interface Step {
  rule: Rule
  edge: Edge
  story: Story
  primary: Verb
  quiet: boolean
  situation: Situation
}

// AutopilotState is where an Autopilot routine stands on the item: it needs the user, it
// is working, or neither.
export interface AutopilotState {
  state: '' | 'needs' | 'working'
  routine: string
}

// noAutopilot is a card no routine waits on or works on.
const noAutopilot: AutopilotState = { state: '', routine: '' }

// autopilotState reads a card's autopilot prop, as the lists word it (rowAutopilot):
// "needs:<routine>", "working:<routine>", or '' for neither.
export function autopilotState(row: string): AutopilotState {
  const at = row.indexOf(':')
  const state = row.slice(0, at)

  return state === 'needs' || state === 'working' ? { state, routine: row.slice(at + 1) } : noAutopilot
}

// StepInput is everything nextStep reads. session, sessionFresh, and backFromSnooze depend
// on the time, which the caller reads: session is the card's session (cardSession),
// sessionFresh reports a failed one that failed under a day ago, and backFromSnooze an item
// back from a snooze within the last day. agent names the session's agent, or else the
// agent a launch resolves to, '' while unknown; mutedTarget is the repository or org that
// mutes the item, '' outside the Muted tab.
export interface StepInput {
  item: InboxItem
  tab: TriageTab
  session: AgentSession | null
  sessionFresh: boolean
  backFromSnooze: boolean
  autopilot: AutopilotState
  agent: string
  mutedTarget: string
}

// mergeMethodWords name auto-merge's method.
const mergeMethodWords: Readonly<Record<string, string>> = { SQUASH: 'squash', MERGE: 'merge commit', REBASE: 'rebase' }

// mergeMethodText names the method a pull request's auto-merge merges with: "squash",
// "merge commit", or "rebase", or one this build doesn't know as GitHub spells it.
export function mergeMethodText(method: string): string {
  return mergeMethodWords[method] ?? method.toLowerCase()
}

// story builds a story outside the triage tabs.
function story(text: string, detail: string, icon: StoryIcon): Story {
  return { text, detail, icon, triage: false }
}

// wakeText says the item is back from a snooze: what ended it early, if anything did.
function wakeText(item: InboxItem): string {
  const woke = wakeSummary(item.wokeBy)

  return woke === '' ? 'back from snooze' : `back early: ${woke}`
}

// itemStep is the step rules 5 to 19 give: what the item itself asks for, whatever an
// agent or a routine is doing with it.
function itemStep({ item, backFromSnooze }: StepInput): Step {
  const d = item.details
  const asked = item.reviewRequest !== 'none'
  const isPR = item.kind === 'pr'
  const mine = isPR && !asked && (item.authored || item.assigned)

  const step = (rule: Rule, edge: Edge, s: Story, primary: Verb, quiet = false, situation: Situation = ''): Step => ({
    rule,
    edge,
    story: s,
    primary,
    quiet,
    situation,
  })

  // 5. The details haven't been read yet.
  if (d === null) {
    return step(
      'loading',
      'waiting',
      story('Reading details…', '', 'dot'),
      asked ? 'review' : isPR ? 'open' : 'read',
      true,
    )
  }

  const checks = checksSignal(d.checks)
  const reviews = reviewsSignal(d.reviews)
  const { passed, failed, pending } = d.checks
  const ran = passed + failed + pending
  const wake = backFromSnooze ? wakeText(item) : ''

  // worst is a pull request's worst detail, for a review asked of the user.
  const worst =
    d.mergeable === 'CONFLICTING'
      ? 'conflicts with its base'
      : checks.kind === 'failed'
        ? 'checks failing'
        : pending > 0
          ? `${counted(pending, 'check', 'checks')} still running`
          : wake

  // woken puts the wake text into a story of rules 14 to 19: it becomes the detail, or
  // follows it, and the icon becomes the clock.
  const woken = (text: string, detail: string, icon: StoryIcon): Story =>
    backFromSnooze ? story(text, detail === '' ? wake : `${detail} · ${wake}`, 'clock') : story(text, detail, icon)

  // 6. Workflow runs wait for the user's approval.
  if (asked && d.checks.awaiting > 0) {
    return step(
      'runs_held',
      'urgent',
      story(`${counted(d.checks.awaiting, 'run awaits', 'runs await')} your approval`, '', 'clock'),
      'review',
    )
  }

  // 7 to 10. The user's own pull request, or one assigned to them.
  if (mine && d.mergeable === 'CONFLICTING') {
    const text = reviews.kind === 'approved' ? 'Approved, but conflicts with its base' : 'Conflicts with its base'

    return step('conflicts', 'blocked', story(text, '', 'dot'), 'open', false, 'conflicts')
  }

  if (mine && checks.kind === 'failed') {
    // GitHub can call the checks failed with none of them counted as failing.
    const text =
      failed === 0 ? 'Checks failing' : `${formatCount(failed)} of ${counted(ran, 'check', 'checks')} failing`

    return step(
      'checks_failing',
      'blocked',
      story(text, reviews.kind === 'changes' ? 'changes requested' : '', 'dot'),
      'open',
      false,
      'checks_failing',
    )
  }

  if (mine && reviews.kind === 'changes') {
    const comments = d.comments > 0 ? counted(d.comments, 'comment', 'comments') : ''

    return step(
      'changes_requested',
      'yours',
      story('Changes requested', comments, 'dot'),
      'open',
      false,
      'changes_requested',
    )
  }

  if (mine && item.isDraft) {
    return step('my_draft', 'yours', story('Draft', 'not marked ready', 'dot'), 'open')
  }

  // 11 to 13. A review asked of the user.
  if (asked && d.pushedSinceReview) {
    const text = d.reviewRequestedAgain ? 'Review re-requested' : 'Re-review'

    return step('rereview', 'yours', story(text, worst, 'dot'), 'rereview', false, 'rereview')
  }

  if (asked && item.isDraft) {
    return step('review_draft', 'waiting', story('Not ready for review', '', 'dot'), 'open', true)
  }

  if (asked) {
    return step('review', 'yours', story('Your review', worst, 'dot'), 'review')
  }

  // 14. An issue.
  if (!isPR) {
    const status = d.projects[0]?.status ?? ''
    const yours = item.assigned || status !== ''
    const text = status !== '' ? status : item.assigned ? 'Assigned to you' : 'Opened by you'

    return step('issue', yours ? 'yours' : 'waiting', woken(text, '', 'dot'), 'read', !yours)
  }

  // 15 to 19. Not the user's move, or one ready to make.
  if (
    mine &&
    reviews.kind === 'approved' &&
    (checks.kind === 'passed' || checks.kind === 'none') &&
    d.mergeable === 'MERGEABLE' &&
    d.autoMerge === ''
  ) {
    return step('ready', 'ready', woken('Approved · ready to merge', '', 'dot'), 'merge')
  }

  if (d.autoMerge !== '') {
    return step('auto_merge', 'waiting', woken('Merges when green', mergeMethodText(d.autoMerge), 'bolt'), 'open', true)
  }

  if (item.assigned && !item.authored) {
    return step('assigned_pr', 'yours', woken('Assigned to you', '', 'dot'), 'review')
  }

  if (checks.kind === 'pending') {
    const progress = ran > 0 ? `${formatCount(passed)}/${formatCount(ran)}` : ''

    return step('checks_running', 'waiting', woken('Checks running', progress, 'dot'), 'open', true)
  }

  const waitingOn = d.reviews.reviewers - d.reviews.reviewed
  const text =
    reviews.kind === 'approved'
      ? 'Approved'
      : waitingOn > 0
        ? `Waiting on ${counted(waitingOn, 'reviewer', 'reviewers')}`
        : 'Waiting for review'

  return step('waiting', 'waiting', woken(text, '', 'dot'), 'open', true)
}

// nextStep works out what a card puts first: the tabs an item is set aside in, then what
// waits on the user now, then an agent or a routine at work, which keeps the item's own
// move as a quiet primary, then what the item itself asks for.
export function nextStep(input: StepInput): Step {
  const { tab, session, sessionFresh, autopilot, agent, mutedTarget } = input
  const none = { edge: 'none', quiet: true, situation: '' } as const

  // T1 to T3. The tabs an item is set aside in, whose primary brings it back.
  if (tab === 'snoozed') {
    return {
      ...none,
      rule: 'snoozed',
      story: { text: '', detail: '', icon: 'clock', triage: true },
      primary: 'unsnooze',
    }
  }

  if (tab === 'dismissed') {
    return {
      ...none,
      rule: 'dismissed',
      story: { text: '', detail: '', icon: 'archive', triage: true },
      primary: 'restore',
    }
  }

  if (tab === 'muted') {
    return { ...none, rule: 'muted', story: story(`Muted with ${mutedTarget}`, '', 'dot'), primary: 'unmute' }
  }

  // 1. An agent waits for the user.
  if (session !== null && needsYou(session.state)) {
    const text = session.state === 'needs_approval' ? `${agent} needs approval` : `${agent} needs input`

    return {
      rule: 'session_needs_you',
      edge: 'urgent',
      story: story(text, session.detail, 'ping'),
      primary: session.runId !== 0 ? 'watch' : 'terminal',
      quiet: false,
      situation: '',
    }
  }

  // 2. A routine waits for the user's OK.
  if (autopilot.state === 'needs') {
    return {
      rule: 'autopilot_needs_you',
      edge: 'urgent',
      story: story(`${autopilot.routine} wants your OK`, '', 'bolt'),
      primary: 'decide',
      quiet: false,
      situation: '',
    }
  }

  // 3. An agent failed under a day ago.
  if (session?.state === 'failed' && sessionFresh) {
    return {
      rule: 'session_failed',
      edge: 'blocked',
      story: story(`${agent} failed`, '', 'dot'),
      primary: 'sessions',
      quiet: false,
      situation: '',
    }
  }

  // 4. An agent or a routine is at work: the item's own move stays, quietly.
  const working = session !== null && (session.state === 'starting' || session.state === 'working')

  if (working || autopilot.state === 'working') {
    const base = itemStep(input)

    return {
      rule: 'agent_working',
      edge: 'agent',
      story: working
        ? story(`${agent} is on it`, whereWords(session), 'ping')
        : story(`${autopilot.routine} is working`, '', 'ping'),
      primary: base.primary,
      quiet: true,
      situation: base.situation,
    }
  }

  return itemStep(input)
}
