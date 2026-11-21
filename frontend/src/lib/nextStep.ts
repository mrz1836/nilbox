// What an inbox card puts first: whose move it is (the edge), the one sentence that says
// why (the story), the human's next move (the primary verb), and which of the saved launch
// defaults' prompts the agent button beside it runs (the situation). The first rule that
// matches wins. It reads only what the card already has, so it costs no request, and it
// reads no clock: the caller works out every input that depends on the time.

import type { AgentSession, InboxItem, Situation } from '../platform'
import { counted, formatCount } from './format'
import { awaitsAuthor } from './nudge'
import { needsYou, whereWords } from './sessions'
import { checksSignal, reviewsSignal } from './signals'
import { wakeSummary, type TriageTab } from './triage'

// Edge is whose move a card is, as the color of its left edge: something waits on the
// user now (urgent), it is stuck (blocked), it is the user's (yours), it is ready to ship
// (ready), an agent is on it (agent), it waits on others (waiting), or none, in the tabs
// an item is set aside in.
export type Edge = 'urgent' | 'blocked' | 'yours' | 'ready' | 'agent' | 'waiting' | 'none'

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
  | 'nudge'

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
  | 'awaiting_author'
  | 'rereview'
  | 'approved_by_you'
  | 'conflicts'
  | 'checks_failing'
  | 'changes_requested'
  | 'my_draft'
  | 'review_draft'
  | 'review'
  | 'issue'
  | 'issue_fix'
  | 'ready'
  | 'auto_merge'
  | 'assigned_pr'
  | 'checks_running'
  | 'waiting'

// Story is the card's one sentence: its lead, in the edge's color, and a muted detail after
// " · ", '' for none. In the Snoozed and Dismissed tabs it is triage: the card shows when
// the item comes back, or when it was dismissed, instead, then state, muted: the lead the
// item's own story would have outside those tabs, so the card still says where it stands.
// state is '' everywhere else.
export interface Story {
  text: string
  detail: string
  icon: StoryIcon
  triage: boolean
  state: string
}

// Step is what a card puts first. quiet makes the primary a quiet button, outlined in the
// border color rather than the edge's. situation picks the launch defaults' prompt the
// agent button beside it runs: '' for the item kind's own, or a pull request's situation.
export interface Step {
  rule: Rule
  edge: Edge
  story: Story
  primary: Verb
  quiet: boolean
  situation: Situation
}

// CardAutopilot is where an Autopilot routine stands on a card's item: it needs the
// user, it is working, or neither.
export interface CardAutopilot {
  state: '' | 'needs' | 'working'
  routine: string
}

// noAutopilot is a card no routine waits on or works on.
const noAutopilot: CardAutopilot = { state: '', routine: '' }

// autopilotState reads a card's autopilot prop, as the lists word it (rowAutopilot):
// "needs:<routine>", "working:<routine>", or '' for neither.
export function autopilotState(row: string): CardAutopilot {
  const at = row.indexOf(':')
  const state = row.slice(0, at)

  return state === 'needs' || state === 'working' ? { state, routine: row.slice(at + 1) } : noAutopilot
}

// NudgeState is what a card knows of a nudge of a pull request's author, at the time the
// caller read: whether one is due (nudgeDue), and the words its story says of the wait
// (nudgeWords), '' for none.
export interface NudgeState {
  due: boolean
  words: string
}

// noNudge is a card that offers no nudge and says nothing of one.
const noNudge: NudgeState = { due: false, words: '' }

// StepInput is everything nextStep reads. session, sessionFresh, backFromSnooze, and nudge
// depend on the time, which the caller reads: session is the card's session (cardSession),
// sessionFresh reports a failed one that failed under a day ago, backFromSnooze an item
// back from a snooze within the last day, and nudge whether a nudge of a pull request's
// quiet author is due, with what the story says of the wait. agent names the session's
// agent, or else the agent a launch resolves to, '' while unknown; mutedTarget is the
// repository or org that mutes the item, '' outside the Muted tab.
export interface StepInput {
  item: InboxItem
  tab: TriageTab
  session: AgentSession | null
  sessionFresh: boolean
  backFromSnooze: boolean
  nudge?: NudgeState
  autopilot: CardAutopilot
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
  return { text, detail, icon, triage: false, state: '' }
}

// wakeText says the item is back from a snooze: what ended it early, if anything did.
function wakeText(item: InboxItem): string {
  const woke = wakeSummary(item.wokeBy)

  return woke === '' ? 'back from snooze' : `back early: ${woke}`
}

// itemStep is the step rules 5 to 19 give: what the item itself asks for, whatever an
// agent or a routine is doing with it.
function itemStep({ item, backFromSnooze, nudge = noNudge }: StepInput): Step {
  const d = item.details
  const asked = item.reviewRequest !== 'none'
  const isPR = item.kind === 'pr'
  const mine = isPR && !asked && (item.authored || item.assigned)
  // theirs is a pull request someone else opened, which the user may have reviewed.
  const theirs = isPR && !item.authored

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
  // approvedByYou is someone else's pull request the user's latest verdict approved, with
  // no review request of the user's open: a new request hands it back to the review rules.
  const approvedByYou = theirs && !asked && d.viewerVerdict === 'approved'
  // ready is a pull request ready to merge: no draft, approved, its checks passed (or it
  // has none), no conflicts, and no auto-merge waiting to do it.
  const ready =
    !item.isDraft &&
    reviews.kind === 'approved' &&
    (checks.kind === 'passed' || checks.kind === 'none') &&
    d.mergeable === 'MERGEABLE' &&
    d.autoMerge === ''

  // worst is a pull request's worst detail, for a review asked of the user or one that
  // waits on its author.
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

  // 6. Workflow runs wait for the user's approval, on a review asked of the user or a pull
  // request they approved.
  if ((asked || approvedByYou) && d.checks.awaiting > 0) {
    return step(
      'runs_held',
      'urgent',
      story(
        `${counted(d.checks.awaiting, 'run awaits', 'runs await')} your approval`,
        approvedByYou ? 'you approved' : '',
        'clock',
      ),
      asked ? 'review' : 'open',
    )
  }

  // After 6. The user asked the author of someone else's pull request for changes, and
  // the author hasn't pushed since nor asked for the user's review again: nothing is the
  // user's to do until they do, but nudge them once they have been quiet long enough. The
  // story says how long, or when the user nudged them.
  if (awaitsAuthor(item)) {
    const said =
      nudge.words === ''
        ? 'you requested changes'
        : item.nudgedAt !== ''
          ? nudge.words
          : `you requested changes, ${nudge.words}`
    const detail = worst === '' ? said : `${said}, ${worst}`

    return step(
      'awaiting_author',
      'waiting',
      story('Waiting on the author', detail, 'dot'),
      nudge.due ? 'nudge' : 'open',
      !nudge.due,
    )
  }

  // 11, ahead of 7 to 10. The author pushed since the user asked for changes, on a review
  // asked of the user or a pull request assigned to them: the user's move is to look again,
  // not to fix it.
  if ((asked || theirs) && d.pushedSinceReview) {
    const text = d.reviewRequestedAgain ? 'Review re-requested' : 'Re-review'

    return step('rereview', 'yours', story(text, worst, 'dot'), 'rereview', false, 'rereview')
  }

  // Ahead of 7 to 10. The user approved someone else's pull request, which isn't ready to
  // merge yet: there is nothing left to review. With auto-merge on, it says so, as rule 16
  // does. Once it is ready, rule 15 offers the merge.
  if (approvedByYou && !ready) {
    const s =
      d.autoMerge !== ''
        ? woken('Merges when green', mergeMethodText(d.autoMerge), 'bolt')
        : story('Approved by you', worst, 'dot')

    return step('approved_by_you', 'waiting', s, 'open', true)
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

  // 12 and 13. A review asked of the user.
  if (asked && item.isDraft) {
    return step('review_draft', 'waiting', story('Not ready for review', '', 'dot'), 'open', true)
  }

  if (asked) {
    return step('review', 'yours', story('Your review', worst, 'dot'), 'review')
  }

  // 14. An issue. A trusted pull request on its way to closing it carries the work, so the
  // issue waits on it, quietly, a merged one first; the card links to it (LinkChip).
  // Otherwise its first project's status leads, or that it is assigned to the user.
  if (!isPR) {
    const fix = d.closingPulls.find((p) => p.trusted && p.merged) ?? d.closingPulls.find((p) => p.trusted)

    if (fix !== undefined) {
      const text = fix.merged ? 'Fix merged' : 'Fix in progress'

      return step('issue_fix', 'waiting', woken(text, fix.isDraft ? 'draft' : '', 'dot'), 'read', true)
    }

    const status = d.projects[0]?.status ?? ''
    const yours = item.assigned || status !== ''
    const text = status !== '' ? status : item.assigned ? 'Assigned to you' : 'Opened by you'

    return step('issue', yours ? 'yours' : 'waiting', woken(text, '', 'dot'), 'read', !yours)
  }

  // 15 to 19. Not the user's move, or one ready to make.
  if (mine && ready) {
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

// liveStep is the step rules 1 to 19 give, wherever the item is: what waits on the user
// now, then an agent or a routine at work, then what the item itself asks for.
function liveStep(input: StepInput): Step {
  const { session, sessionFresh, autopilot, agent } = input

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

// nextStep works out what a card puts first: the tabs an item is set aside in, then what
// waits on the user now, then an agent or a routine at work, which keeps the item's own
// move as a quiet primary, then what the item itself asks for.
export function nextStep(input: StepInput): Step {
  const { tab, mutedTarget } = input
  const none = { edge: 'none', quiet: true, situation: '' } as const

  // T1 and T2. The Snoozed and Dismissed tabs, whose primary brings the item back: the
  // card says when it comes back, or when it was dismissed, then where it stands, the
  // lead its own story (rules 1 to 19) would have in its list.
  if (tab === 'snoozed' || tab === 'dismissed') {
    const snoozed = tab === 'snoozed'

    return {
      ...none,
      rule: tab,
      story: {
        text: '',
        detail: '',
        icon: snoozed ? 'clock' : 'archive',
        triage: true,
        state: liveStep(input).story.text,
      },
      primary: snoozed ? 'unsnooze' : 'restore',
    }
  }

  // T3. The Muted tab, whose primary unmutes the item's repository or org.
  if (tab === 'muted') {
    return { ...none, rule: 'muted', story: story(`Muted with ${mutedTarget}`, '', 'dot'), primary: 'unmute' }
  }

  return liveStep(input)
}
