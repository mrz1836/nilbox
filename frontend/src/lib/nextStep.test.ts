import { describe, expect, it } from 'vitest'

import type { AgentSession, InboxItem, ItemDetails } from '../platform'
import { agentSession, details, item, project } from '../test/fixtures'
import { autopilotState, nextStep, type Step, type StepInput } from './nextStep'

// input is what a card in the inbox tab reads, with no session, no routine, and the agent
// known, changed by overrides.
function input(overrides: Partial<StepInput> = {}): StepInput {
  return {
    item: item(),
    tab: 'inbox',
    session: null,
    sessionFresh: false,
    backFromSnooze: false,
    autopilot: { state: '', routine: '' },
    agent: 'Claude',
    mutedTarget: '',
    ...overrides,
  }
}

// pr is a pull request with its details read: passing checks, approved, mergeable.
function pr(overrides: Partial<InboxItem> = {}, d: Partial<ItemDetails> = {}): InboxItem {
  return item({ details: details(d), ...overrides })
}

// asked is a pull request that asks for the user's review.
function asked(overrides: Partial<InboxItem> = {}, d: Partial<ItemDetails> = {}): InboxItem {
  return pr({ reviewRequest: 'you', ...overrides }, d)
}

// mine is the user's own pull request.
function mine(overrides: Partial<InboxItem> = {}, d: Partial<ItemDetails> = {}): InboxItem {
  return pr({ authored: true, ...overrides }, d)
}

// issue is an issue with its details read.
function issue(overrides: Partial<InboxItem> = {}, d: Partial<ItemDetails> = {}): InboxItem {
  return item({ kind: 'issue', details: details({ labels: [], ...d }), ...overrides })
}

// session is an agent session in state, launched by hand.
function session(state: AgentSession['state'], overrides: Partial<AgentSession> = {}): AgentSession {
  return agentSession({ state, ...overrides })
}

const failing = { checks: { state: 'FAILURE', passed: 13, failed: 1, pending: 0, skipped: 0, awaiting: 0 } }
const running = { checks: { state: 'PENDING', passed: 9, failed: 0, pending: 5, skipped: 0, awaiting: 0 } }
const changes = {
  reviews: { decision: 'CHANGES_REQUESTED', reviewers: 2, reviewed: 1, approvals: 0, changesRequested: 1 },
}
const waitingOnTwo = {
  reviews: { decision: 'REVIEW_REQUIRED', reviewers: 2, reviewed: 0, approvals: 0, changesRequested: 0 },
}
const woke = { review: 'approved', commits: 2, forcePushed: false, comments: 0, ready: false } as const

interface Want {
  rule: Step['rule']
  edge: Step['edge']
  text: string
  detail?: string
  icon?: Step['story']['icon']
  primary: Step['primary']
  quiet?: boolean
  situation?: Step['situation']
  triage?: boolean
}

// step is the step a case wants, its story's detail and icon, quiet, situation, and
// triage defaulting to none, a dot, loud, the kind's own, and no.
function step(want: Want): Step {
  return {
    rule: want.rule,
    edge: want.edge,
    story: {
      text: want.text,
      detail: want.detail ?? '',
      icon: want.icon ?? 'dot',
      triage: want.triage ?? false,
    },
    primary: want.primary,
    quiet: want.quiet ?? false,
    situation: want.situation ?? '',
  }
}

const cases: readonly (readonly [string, StepInput, Want])[] = [
  // T1 to T3: the tabs an item is set aside in.
  [
    'T1 snoozed',
    input({ tab: 'snoozed', item: asked() }),
    { rule: 'snoozed', edge: 'none', text: '', icon: 'clock', triage: true, primary: 'unsnooze', quiet: true },
  ],
  [
    'T2 dismissed',
    input({ tab: 'dismissed', item: mine({}, failing) }),
    { rule: 'dismissed', edge: 'none', text: '', icon: 'archive', triage: true, primary: 'restore', quiet: true },
  ],
  [
    'T3 muted',
    input({ tab: 'muted', item: asked(), mutedTarget: 'acme-org/legacy-tool', session: session('needs_approval') }),
    { rule: 'muted', edge: 'none', text: 'Muted with acme-org/legacy-tool', primary: 'unmute', quiet: true },
  ],

  // 1: an agent waits for the user.
  [
    '1 a session needs approval',
    input({ item: mine(), session: session('needs_approval') }),
    {
      rule: 'session_needs_you',
      edge: 'urgent',
      text: 'Claude needs approval',
      detail: 'Permission prompt',
      icon: 'ping',
      primary: 'terminal',
    },
  ],
  [
    '1 a background run needs input',
    input({ item: asked(), agent: 'Codex', session: session('needs_input', { detail: '', runId: 7 }) }),
    { rule: 'session_needs_you', edge: 'urgent', text: 'Codex needs input', icon: 'ping', primary: 'watch' },
  ],

  // 2: a routine waits for the user's OK.
  [
    '2 a routine needs you',
    input({ item: mine({}, failing), autopilot: { state: 'needs', routine: 'Heal Dependabot PRs' } }),
    {
      rule: 'autopilot_needs_you',
      edge: 'urgent',
      text: 'Heal Dependabot PRs wants your OK',
      icon: 'bolt',
      primary: 'decide',
    },
  ],

  // 3: an agent failed under a day ago.
  [
    '3 a session failed',
    input({ item: mine({}, failing), session: session('failed'), sessionFresh: true }),
    { rule: 'session_failed', edge: 'blocked', text: 'Claude failed', primary: 'sessions' },
  ],

  // 4: an agent or a routine at work keeps the item's own move, quietly.
  [
    '4 a session works',
    input({ item: mine({}, failing), session: session('working', { detail: '' }) }),
    {
      rule: 'agent_working',
      edge: 'agent',
      text: 'Claude is on it',
      detail: 'new worktree',
      icon: 'ping',
      primary: 'open',
      quiet: true,
      situation: 'checks_failing',
    },
  ],
  [
    '4 a session starts in the clone',
    input({ item: issue({ assigned: true }), session: session('starting', { worktree: '', worktreeAction: '' }) }),
    {
      rule: 'agent_working',
      edge: 'agent',
      text: 'Claude is on it',
      detail: 'in the clone',
      icon: 'ping',
      primary: 'read',
      quiet: true,
    },
  ],
  [
    '4 a routine works',
    input({ item: mine({}, changes), autopilot: { state: 'working', routine: 'Fix failing checks' } }),
    {
      rule: 'agent_working',
      edge: 'agent',
      text: 'Fix failing checks is working',
      icon: 'ping',
      primary: 'open',
      quiet: true,
      situation: 'changes_requested',
    },
  ],

  // 5: the details haven't been read.
  [
    '5 loading a review request',
    input({ item: item({ reviewRequest: 'you' }) }),
    { rule: 'loading', edge: 'waiting', text: 'Reading details…', primary: 'review', quiet: true },
  ],
  [
    '5 loading another pull request',
    input({ item: item({ authored: true }) }),
    { rule: 'loading', edge: 'waiting', text: 'Reading details…', primary: 'open', quiet: true },
  ],
  [
    '5 loading an issue',
    input({ item: item({ kind: 'issue', assigned: true }) }),
    { rule: 'loading', edge: 'waiting', text: 'Reading details…', primary: 'read', quiet: true },
  ],

  // 6: workflow runs wait for the user's approval.
  [
    '6 runs held',
    input({ item: asked({}, { checks: { state: '', passed: 0, failed: 0, pending: 0, skipped: 0, awaiting: 2 } }) }),
    { rule: 'runs_held', edge: 'urgent', text: '2 runs await your approval', icon: 'clock', primary: 'review' },
  ],
  [
    '6 one run held',
    input({ item: asked({}, { checks: { state: '', passed: 0, failed: 0, pending: 0, skipped: 0, awaiting: 1 } }) }),
    { rule: 'runs_held', edge: 'urgent', text: '1 run awaits your approval', icon: 'clock', primary: 'review' },
  ],

  // 7: the user's pull request conflicts with its base.
  [
    '7 conflicts',
    input({ item: mine({}, { mergeable: 'CONFLICTING', ...waitingOnTwo }) }),
    { rule: 'conflicts', edge: 'blocked', text: 'Conflicts with its base', primary: 'open', situation: 'conflicts' },
  ],
  [
    '7 approved, but conflicts',
    input({ item: mine({}, { mergeable: 'CONFLICTING' }) }),
    {
      rule: 'conflicts',
      edge: 'blocked',
      text: 'Approved, but conflicts with its base',
      primary: 'open',
      situation: 'conflicts',
    },
  ],

  // 8: the user's pull request fails its checks.
  [
    '8 checks failing',
    input({ item: mine({}, failing) }),
    {
      rule: 'checks_failing',
      edge: 'blocked',
      text: '1 of 14 checks failing',
      primary: 'open',
      situation: 'checks_failing',
    },
  ],
  [
    '8 checks failing, none counted',
    input({
      item: mine({}, { checks: { state: 'ERROR', passed: 0, failed: 0, pending: 0, skipped: 0, awaiting: 0 } }),
    }),
    { rule: 'checks_failing', edge: 'blocked', text: 'Checks failing', primary: 'open', situation: 'checks_failing' },
  ],
  [
    '8 one of one check failing',
    input({
      item: mine({}, { checks: { state: 'FAILURE', passed: 0, failed: 1, pending: 0, skipped: 0, awaiting: 0 } }),
    }),
    {
      rule: 'checks_failing',
      edge: 'blocked',
      text: '1 of 1 check failing',
      primary: 'open',
      situation: 'checks_failing',
    },
  ],

  // 9: changes requested on the user's pull request.
  [
    '9 changes requested',
    input({ item: mine({}, { ...changes, comments: 9 }) }),
    {
      rule: 'changes_requested',
      edge: 'yours',
      text: 'Changes requested',
      detail: '9 comments',
      primary: 'open',
      situation: 'changes_requested',
    },
  ],
  [
    '9 changes requested, one comment',
    input({ item: mine({}, { ...changes, comments: 1 }) }),
    {
      rule: 'changes_requested',
      edge: 'yours',
      text: 'Changes requested',
      detail: '1 comment',
      primary: 'open',
      situation: 'changes_requested',
    },
  ],
  [
    '9 changes requested, no comments',
    input({ item: mine({}, { ...changes, comments: 0 }) }),
    {
      rule: 'changes_requested',
      edge: 'yours',
      text: 'Changes requested',
      primary: 'open',
      situation: 'changes_requested',
    },
  ],

  // 10: the user's own draft.
  [
    '10 my draft',
    input({ item: mine({ isDraft: true }) }),
    { rule: 'my_draft', edge: 'yours', text: 'Draft', detail: 'not marked ready', primary: 'open' },
  ],

  // 11 to 13: a review asked of the user.
  [
    '11 re-review',
    input({ item: asked({}, { pushedSinceReview: true }) }),
    { rule: 'rereview', edge: 'yours', text: 'Re-review', primary: 'rereview', situation: 'rereview' },
  ],
  [
    '11 review re-requested',
    input({ item: asked({}, { pushedSinceReview: true, reviewRequestedAgain: true }) }),
    { rule: 'rereview', edge: 'yours', text: 'Review re-requested', primary: 'rereview', situation: 'rereview' },
  ],
  [
    '11 re-review back from snooze',
    input({
      item: asked({ wokeBy: { ...woke, review: '', commits: 1 } }, { pushedSinceReview: true }),
      backFromSnooze: true,
    }),
    {
      rule: 'rereview',
      edge: 'yours',
      text: 'Re-review',
      detail: 'back early: 1 new commit',
      primary: 'rereview',
      situation: 'rereview',
    },
  ],
  [
    '12 a draft asked of the user',
    input({ item: asked({ isDraft: true }) }),
    { rule: 'review_draft', edge: 'waiting', text: 'Not ready for review', primary: 'open', quiet: true },
  ],
  ['13 a review', input({ item: asked() }), { rule: 'review', edge: 'yours', text: 'Your review', primary: 'review' }],
  [
    '13 a review with checks still running',
    input({ item: asked({}, running) }),
    { rule: 'review', edge: 'yours', text: 'Your review', detail: '5 checks still running', primary: 'review' },
  ],
  [
    '13 a review with one check still running',
    input({
      item: asked({}, { checks: { state: 'PENDING', passed: 16, failed: 0, pending: 1, skipped: 0, awaiting: 0 } }),
    }),
    { rule: 'review', edge: 'yours', text: 'Your review', detail: '1 check still running', primary: 'review' },
  ],
  [
    '13 a review back early',
    input({ item: asked({ wokeBy: woke }), backFromSnooze: true }),
    {
      rule: 'review',
      edge: 'yours',
      text: 'Your review',
      detail: 'back early: approved, 2 new commits',
      primary: 'review',
    },
  ],
  [
    '13 a review back from snooze',
    input({ item: asked(), backFromSnooze: true }),
    { rule: 'review', edge: 'yours', text: 'Your review', detail: 'back from snooze', primary: 'review' },
  ],
  [
    '13 a review through a team',
    input({ item: asked({ reviewRequest: 'team' }) }),
    { rule: 'review', edge: 'yours', text: 'Your review', primary: 'review' },
  ],

  // 14: an issue.
  [
    '14 an issue assigned to the user',
    input({ item: issue({ assigned: true }) }),
    { rule: 'issue', edge: 'yours', text: 'Assigned to you', primary: 'read' },
  ],
  [
    '14 an issue with a project status',
    input({ item: issue({ authored: true }, { projects: [project()] }) }),
    { rule: 'issue', edge: 'yours', text: 'In progress', primary: 'read' },
  ],
  [
    '14 an issue in a project without a status',
    input({ item: issue({ assigned: true }, { projects: [project({ status: '' })] }) }),
    { rule: 'issue', edge: 'yours', text: 'Assigned to you', primary: 'read' },
  ],
  [
    '14 an issue back from snooze',
    input({ item: issue({ assigned: true }), backFromSnooze: true }),
    {
      rule: 'issue',
      edge: 'yours',
      text: 'Assigned to you',
      detail: 'back from snooze',
      icon: 'clock',
      primary: 'read',
    },
  ],

  // 15 to 19: not the user's move, or one ready to make.
  [
    '15 ready to merge',
    input({ item: mine() }),
    { rule: 'ready', edge: 'ready', text: 'Approved · ready to merge', primary: 'merge' },
  ],
  [
    '15 ready to merge without checks',
    input({ item: mine({}, { checks: { state: '', passed: 0, failed: 0, pending: 0, skipped: 0, awaiting: 0 } }) }),
    { rule: 'ready', edge: 'ready', text: 'Approved · ready to merge', primary: 'merge' },
  ],
  [
    '15 ready to merge, back early',
    input({ item: mine({ wokeBy: woke }), backFromSnooze: true }),
    {
      rule: 'ready',
      edge: 'ready',
      text: 'Approved · ready to merge',
      detail: 'back early: approved, 2 new commits',
      icon: 'clock',
      primary: 'merge',
    },
  ],
  [
    '16 auto-merge by squash',
    input({ item: mine({}, { ...running, autoMerge: 'SQUASH' }) }),
    {
      rule: 'auto_merge',
      edge: 'waiting',
      text: 'Merges when green',
      detail: 'squash',
      icon: 'bolt',
      primary: 'open',
      quiet: true,
    },
  ],
  [
    '16 auto-merge by merge commit',
    input({ item: mine({}, { autoMerge: 'MERGE' }) }),
    {
      rule: 'auto_merge',
      edge: 'waiting',
      text: 'Merges when green',
      detail: 'merge commit',
      icon: 'bolt',
      primary: 'open',
      quiet: true,
    },
  ],
  [
    '16 auto-merge by rebase',
    input({ item: mine({}, { autoMerge: 'REBASE' }) }),
    {
      rule: 'auto_merge',
      edge: 'waiting',
      text: 'Merges when green',
      detail: 'rebase',
      icon: 'bolt',
      primary: 'open',
      quiet: true,
    },
  ],
  [
    '16 auto-merge by a method this build does not know',
    input({ item: mine({}, { autoMerge: 'FAST_FORWARD' }) }),
    {
      rule: 'auto_merge',
      edge: 'waiting',
      text: 'Merges when green',
      detail: 'fast_forward',
      icon: 'bolt',
      primary: 'open',
      quiet: true,
    },
  ],
  [
    '16 auto-merge back from snooze',
    input({ item: mine({}, { autoMerge: 'SQUASH' }), backFromSnooze: true }),
    {
      rule: 'auto_merge',
      edge: 'waiting',
      text: 'Merges when green',
      detail: 'squash · back from snooze',
      icon: 'clock',
      primary: 'open',
      quiet: true,
    },
  ],
  [
    '17 a pull request assigned to the user',
    input({ item: pr({ assigned: true }, waitingOnTwo) }),
    { rule: 'assigned_pr', edge: 'yours', text: 'Assigned to you', primary: 'review' },
  ],
  [
    '17 assigned, back from snooze',
    input({ item: pr({ assigned: true }, waitingOnTwo), backFromSnooze: true }),
    {
      rule: 'assigned_pr',
      edge: 'yours',
      text: 'Assigned to you',
      detail: 'back from snooze',
      icon: 'clock',
      primary: 'review',
    },
  ],
  [
    '18 checks running',
    input({ item: mine({}, { ...running, ...waitingOnTwo }) }),
    { rule: 'checks_running', edge: 'waiting', text: 'Checks running', detail: '9/14', primary: 'open', quiet: true },
  ],
  [
    '18 checks running, none counted yet',
    input({
      item: mine({}, { checks: { state: 'PENDING', passed: 0, failed: 0, pending: 0, skipped: 0, awaiting: 0 } }),
    }),
    { rule: 'checks_running', edge: 'waiting', text: 'Checks running', primary: 'open', quiet: true },
  ],
  [
    '18 checks running, back early',
    input({ item: mine({ wokeBy: woke }, { ...running, ...waitingOnTwo }), backFromSnooze: true }),
    {
      rule: 'checks_running',
      edge: 'waiting',
      text: 'Checks running',
      detail: '9/14 · back early: approved, 2 new commits',
      icon: 'clock',
      primary: 'open',
      quiet: true,
    },
  ],
  [
    '19 waiting on reviewers',
    input({ item: mine({}, waitingOnTwo) }),
    { rule: 'waiting', edge: 'waiting', text: 'Waiting on 2 reviewers', primary: 'open', quiet: true },
  ],
  [
    '19 waiting on one reviewer',
    input({
      item: mine(
        {},
        { reviews: { decision: 'REVIEW_REQUIRED', reviewers: 1, reviewed: 0, approvals: 0, changesRequested: 0 } },
      ),
    }),
    { rule: 'waiting', edge: 'waiting', text: 'Waiting on 1 reviewer', primary: 'open', quiet: true },
  ],
  [
    '19 waiting for review',
    input({
      item: mine({}, { reviews: { decision: '', reviewers: 0, reviewed: 0, approvals: 0, changesRequested: 0 } }),
    }),
    { rule: 'waiting', edge: 'waiting', text: 'Waiting for review', primary: 'open', quiet: true },
  ],
  [
    '19 waiting, back from snooze',
    input({ item: mine({}, waitingOnTwo), backFromSnooze: true }),
    {
      rule: 'waiting',
      edge: 'waiting',
      text: 'Waiting on 2 reviewers',
      detail: 'back from snooze',
      icon: 'clock',
      primary: 'open',
      quiet: true,
    },
  ],

  // The precedence between neighbouring rules.
  [
    '1 over 2',
    input({ item: mine(), session: session('needs_approval'), autopilot: { state: 'needs', routine: 'Merge green' } }),
    {
      rule: 'session_needs_you',
      edge: 'urgent',
      text: 'Claude needs approval',
      detail: 'Permission prompt',
      icon: 'ping',
      primary: 'terminal',
    },
  ],
  [
    '2 over 3',
    input({
      item: mine(),
      session: session('failed'),
      sessionFresh: true,
      autopilot: { state: 'needs', routine: 'Merge green' },
    }),
    { rule: 'autopilot_needs_you', edge: 'urgent', text: 'Merge green wants your OK', icon: 'bolt', primary: 'decide' },
  ],
  [
    '3 over 4',
    input({
      item: mine(),
      session: session('failed'),
      sessionFresh: true,
      autopilot: { state: 'working', routine: 'Merge green' },
    }),
    { rule: 'session_failed', edge: 'blocked', text: 'Claude failed', primary: 'sessions' },
  ],
  [
    '4 keeps the base primary',
    input({ item: mine({ isDraft: true }), session: session('working', { worktree: '', worktreeAction: '' }) }),
    {
      rule: 'agent_working',
      edge: 'agent',
      text: 'Claude is on it',
      detail: 'in the clone',
      icon: 'ping',
      primary: 'open',
      quiet: true,
    },
  ],
  [
    '5 over 6 to 19',
    input({ item: item({ reviewRequest: 'you', isDraft: true, assigned: true }) }),
    { rule: 'loading', edge: 'waiting', text: 'Reading details…', primary: 'review', quiet: true },
  ],
  [
    '6 over 11',
    input({
      item: asked(
        {},
        { pushedSinceReview: true, checks: { state: '', passed: 3, failed: 0, pending: 0, skipped: 0, awaiting: 2 } },
      ),
    }),
    { rule: 'runs_held', edge: 'urgent', text: '2 runs await your approval', icon: 'clock', primary: 'review' },
  ],
  [
    '7 over 8',
    input({ item: mine({}, { ...failing, mergeable: 'CONFLICTING' }) }),
    {
      rule: 'conflicts',
      edge: 'blocked',
      text: 'Approved, but conflicts with its base',
      primary: 'open',
      situation: 'conflicts',
    },
  ],
  [
    '8 over 9',
    input({ item: mine({}, { ...failing, ...changes }) }),
    {
      rule: 'checks_failing',
      edge: 'blocked',
      text: '1 of 14 checks failing',
      detail: 'changes requested',
      primary: 'open',
      situation: 'checks_failing',
    },
  ],
  [
    '9 over 10',
    input({ item: mine({ isDraft: true }, { ...changes, comments: 2 }) }),
    {
      rule: 'changes_requested',
      edge: 'yours',
      text: 'Changes requested',
      detail: '2 comments',
      primary: 'open',
      situation: 'changes_requested',
    },
  ],
  [
    '11 over 12',
    input({ item: asked({ isDraft: true }, { pushedSinceReview: true }) }),
    { rule: 'rereview', edge: 'yours', text: 'Re-review', primary: 'rereview', situation: 'rereview' },
  ],
  [
    '12 over 13',
    input({ item: asked({ isDraft: true }, failing) }),
    { rule: 'review_draft', edge: 'waiting', text: 'Not ready for review', primary: 'open', quiet: true },
  ],
  [
    '15 gives way to 16 once auto-merge is on',
    input({ item: mine({}, { autoMerge: 'SQUASH' }) }),
    {
      rule: 'auto_merge',
      edge: 'waiting',
      text: 'Merges when green',
      detail: 'squash',
      icon: 'bolt',
      primary: 'open',
      quiet: true,
    },
  ],
  [
    '16 over 17',
    input({ item: pr({ assigned: true }, { ...waitingOnTwo, autoMerge: 'REBASE' }) }),
    {
      rule: 'auto_merge',
      edge: 'waiting',
      text: 'Merges when green',
      detail: 'rebase',
      icon: 'bolt',
      primary: 'open',
      quiet: true,
    },
  ],
  [
    '17 over 18',
    input({ item: pr({ assigned: true }, { ...running, ...waitingOnTwo }) }),
    { rule: 'assigned_pr', edge: 'yours', text: 'Assigned to you', primary: 'review' },
  ],

  // Realistic items.
  [
    'a review request with failing checks',
    input({ item: asked({}, failing) }),
    { rule: 'review', edge: 'yours', text: 'Your review', detail: 'checks failing', primary: 'review' },
  ],
  [
    'a review request with conflicts and failing checks',
    input({ item: asked({}, { ...failing, mergeable: 'CONFLICTING' }) }),
    { rule: 'review', edge: 'yours', text: 'Your review', detail: 'conflicts with its base', primary: 'review' },
  ],
  [
    'the user’s own draft with checks running',
    input({ item: mine({ isDraft: true }, { ...running, ...waitingOnTwo }) }),
    { rule: 'my_draft', edge: 'yours', text: 'Draft', detail: 'not marked ready', primary: 'open' },
  ],
  [
    'someone else’s pull request assigned to the user, failing its checks',
    input({ item: pr({ assigned: true, author: 'contributor-a' }, failing) }),
    {
      rule: 'checks_failing',
      edge: 'blocked',
      text: '1 of 14 checks failing',
      primary: 'open',
      situation: 'checks_failing',
    },
  ],
  [
    'someone else’s pull request assigned to the user, approved and green',
    input({ item: pr({ assigned: true, author: 'contributor-a' }) }),
    { rule: 'ready', edge: 'ready', text: 'Approved · ready to merge', primary: 'merge' },
  ],
  [
    'someone else’s pull request assigned to the user, waiting on reviewers',
    input({ item: pr({ assigned: true, author: 'contributor-a' }, waitingOnTwo) }),
    { rule: 'assigned_pr', edge: 'yours', text: 'Assigned to you', primary: 'review' },
  ],
  [
    'the user’s own pull request with changes requested and failing checks',
    input({ item: mine({}, { ...changes, ...failing }) }),
    {
      rule: 'checks_failing',
      edge: 'blocked',
      text: '1 of 14 checks failing',
      detail: 'changes requested',
      primary: 'open',
      situation: 'checks_failing',
    },
  ],
  [
    'an issue the user opened, unassigned',
    input({ item: issue({ authored: true }) }),
    { rule: 'issue', edge: 'waiting', text: 'Opened by you', primary: 'read', quiet: true },
  ],
  [
    'a pull request pushed since the user’s review, failing its checks',
    input({ item: asked({ reviewRequest: 'pushed' }, { ...failing, ...changes, pushedSinceReview: true }) }),
    {
      rule: 'rereview',
      edge: 'yours',
      text: 'Re-review',
      detail: 'checks failing',
      primary: 'rereview',
      situation: 'rereview',
    },
  ],
  [
    'a review request with a live session',
    input({ item: asked(), session: session('working') }),
    {
      rule: 'agent_working',
      edge: 'agent',
      text: 'Claude is on it',
      detail: 'new worktree',
      icon: 'ping',
      primary: 'review',
      quiet: true,
    },
  ],
  [
    'a Dependabot pull request assigned to the user',
    input({ item: pr({ assigned: true, author: 'dependabot[bot]', authorIsBot: true, trust: 'bot' }, waitingOnTwo) }),
    { rule: 'assigned_pr', edge: 'yours', text: 'Assigned to you', primary: 'review' },
  ],
  [
    'a Dependabot pull request assigned to the user, failing its checks',
    input({ item: pr({ assigned: true, author: 'dependabot[bot]', authorIsBot: true, trust: 'bot' }, failing) }),
    {
      rule: 'checks_failing',
      edge: 'blocked',
      text: '1 of 14 checks failing',
      primary: 'open',
      situation: 'checks_failing',
    },
  ],
  [
    'a Dependabot pull request assigned to the user, approved and green',
    input({ item: pr({ assigned: true, author: 'dependabot[bot]', authorIsBot: true, trust: 'bot' }) }),
    { rule: 'ready', edge: 'ready', text: 'Approved · ready to merge', primary: 'merge' },
  ],
  [
    'the user’s approved green pull request while GitHub works out whether it can merge',
    input({ item: mine({}, { mergeable: 'UNKNOWN' }) }),
    { rule: 'waiting', edge: 'waiting', text: 'Approved', primary: 'open', quiet: true },
  ],
  [
    'a failed session older than a day',
    input({ item: mine(), session: session('failed'), sessionFresh: false }),
    { rule: 'ready', edge: 'ready', text: 'Approved · ready to merge', primary: 'merge' },
  ],
  [
    'an idle session',
    input({ item: asked(), session: session('idle') }),
    { rule: 'review', edge: 'yours', text: 'Your review', primary: 'review' },
  ],
]

describe('nextStep', () => {
  it.each(cases)('%s', (_name, given, want) => {
    expect(nextStep(given)).toEqual(step(want))
  })
})

describe('autopilotState', () => {
  it('reads where a routine stands from a card’s autopilot prop', () => {
    expect(autopilotState('needs:Heal bot PRs: nightly')).toEqual({ state: 'needs', routine: 'Heal bot PRs: nightly' })
    expect(autopilotState('working:Fix checks')).toEqual({ state: 'working', routine: 'Fix checks' })
    expect(autopilotState('')).toEqual({ state: '', routine: '' })
    expect(autopilotState('done:Fix checks')).toEqual({ state: '', routine: '' })
  })
})
