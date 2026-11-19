import type {
  AgentInfo,
  AgentSession,
  InboxItem,
  ItemResolution,
  LaunchDefaults,
  Profile,
  Prompt,
  Settings,
} from '../platform'

// The card guide's sample: one pull request, fixed, that the guide (components/card/
// CardGuide) shows on the real card, so every part of a card has something to say. It is
// nobody's data and asks nothing of GitHub: a review request from an outside contributor
// whose files touch Workflows, its checks passed, pushed since the user's review, mergeable,
// size L, with one comment, the agent's session on it ended a minute ago, and activity the
// user hasn't seen. Its times are counted back from now, so its ages always read the same.

const minute = 60_000

// ago is the time minutes before now, as the backend writes a time.
function ago(now: Date, minutes: number): string {
  return new Date(now.getTime() - minutes * minute).toISOString()
}

// url is the sample's address on GitHub, which nothing opens: the guide's card takes no
// clicks.
const url = 'https://github.com/acme-org/example-repo/pull/150'

// head is the sample's head commit.
const head = '5d1c0b7e9a2f4c6d8e0a1b3c5d7e9f1a2b4c6d8e'

// guideAgent is the agent the sample's session ran in, as the guide's card names it.
const guideAgent: AgentInfo = {
  id: 'claude',
  name: 'Claude',
  command: 'claude',
  configEnv: '',
  configDir: '',
  markers: [],
  effortLevels: [],
  models: [],
  permissionModes: [],
  planLabel: 'Plan mode',
  sessionNames: false,
  sessionIds: false,
  fastMode: false,
  configOverrides: false,
  authFile: '',
  authEnv: [],
  modelsCache: '',
  versionArgs: [],
  signInArgs: [],
  keyEnv: [],
  loginArgs: [],
  statusPart: '',
  background: {
    background: false,
    budget: false,
    cost: false,
    usage: false,
    usageLive: false,
    structured: false,
    resume: false,
    readsTrust: false,
    minVersion: '',
    presets: [],
  },
  installed: true,
  path: '',
}

// guideItem is the sample pull request, as it stands at now: #150 of acme-org/example-repo,
// last moved 18 minutes before.
export function guideItem(now: Date): InboxItem {
  return {
    id: 0,
    kind: 'pr',
    number: 150,
    title: 'feat(client): accept a caller-supplied connection gater',
    url,
    repo: 'acme-org/example-repo',
    author: 'contributor-a',
    updatedAt: ago(now, 18),
    unread: true,
    trust: 'outside',
    trusted: false,
    authorIsBot: false,
    authorAvatarUrl: '',
    isCrossRepository: true,
    authorAssociation: 'CONTRIBUTOR',
    branch: 'connection-gater',
    isDraft: false,
    details: {
      labels: [
        { name: 'feature', color: '#2da44e' },
        { name: 'fork-pr', color: '#6e40c9' },
      ],
      projects: [],
      subIssues: { total: 0, completed: 0 },
      issue: { type: '', typeColor: '', fields: [], milestone: null, parent: null, blockedBy: 0, blocking: 0 },
      closingPulls: [],
      comments: 1,
      additions: 455,
      deletions: 8,
      changedFiles: 5,
      commits: 2,
      mergeable: 'MERGEABLE',
      checks: { state: 'SUCCESS', passed: 22, failed: 0, pending: 0, skipped: 0, awaiting: 0, awaitingOnGitHub: 0 },
      reviews: { decision: 'CHANGES_REQUESTED', reviewers: 1, reviewed: 1, approvals: 0, changesRequested: 1 },
      autoMerge: '',
      fetchedAt: ago(now, 2),
      pushedSinceReview: true,
      reviewRequestedAgain: false,
      viewerVerdict: 'changes',
    },
    files: {
      status: 'flagged',
      head,
      checked: 5,
      total: 5,
      complete: true,
      groups: [
        {
          category: 'workflows',
          label: 'Workflows and CI',
          short: 'Workflows',
          description: 'Run on CI with the repository’s token and secrets.',
          level: 'danger',
          files: [
            {
              path: '.github/workflows/ci.yml',
              previousPath: '',
              change: 'modified',
              pattern: '.github/workflows/',
              url: `${url}/files`,
            },
          ],
        },
      ],
      checkedAt: ago(now, 2),
      error: '',
    },
    dismissedAt: '',
    snoozedUntil: '',
    wokeBy: null,
    pinnedAt: '',
    setAsideBy: 0,
    pinnedBy: 0,
    repoRule: '',
    authored: false,
    assigned: false,
    reviewRequest: 'you',
    reviewTeams: [],
  }
}

// guideSession is the agent's session on the sample, as it stands at now: a review that
// ended a minute before, so the card offers what it found.
export function guideSession(now: Date): AgentSession {
  return {
    id: 'card-guide',
    agentSessionId: '',
    resumedFrom: '',
    agent: 'claude',
    profile: 'default',
    prompt: 'Re-review',
    terminal: 'terminal',
    itemId: 0,
    repo: 'acme-org/example-repo',
    kind: 'pr',
    number: 150,
    title: 'feat(client): accept a caller-supplied connection gater',
    url,
    dir: '',
    worktree: '',
    worktreeAction: '',
    permission: '',
    notice: '',
    state: 'ended',
    detail: '',
    stateAt: ago(now, 1),
    lastMessage: '',
    launchedAt: ago(now, 9),
    endedAt: ago(now, 1),
    canFocus: false,
    canResume: false,
    runId: 0,
    githubEvent: '',
    githubEventCommit: '',
    githubEventAt: '',
  }
}

// rereview is the prompt the sample's situation, a re-review, names.
const rereview: ItemResolution['prompt'] = {
  id: 0,
  nickname: 'Re-review',
  name: 'Re-review a pull request',
  source: 'app',
  inheritedId: 0,
  inheritedSource: 'app',
}

// guideResolution is the sample's launch settings: the agent, read-only for an outside
// contributor, re-reviewing what changed.
export function guideResolution(): ItemResolution {
  return {
    itemId: 0,
    kind: 'pr',
    owner: 'acme-org',
    repo: 'acme-org/example-repo',
    trusted: false,
    agent: guideAgent,
    agentSource: 'app',
    agentInherited: 'claude',
    agentInheritedSource: 'app',
    profile: { value: 'default', source: 'app', inherited: 'default', inheritedSource: 'app' },
    prompt: rereview,
    model: { value: '', source: 'agent', inherited: '', inheritedSource: 'agent' },
    effort: { value: '', source: 'agent', inherited: '', inheritedSource: 'agent' },
    planMode: { value: true, source: 'app', inherited: true, inheritedSource: 'app' },
    worktree: { value: true, source: 'app', inherited: true, inheritedSource: 'app' },
    effortOff: false,
    fast: { value: false, source: 'agent', inherited: false, inheritedSource: 'agent' },
    extraArgs: [],
    warnings: [],
    situations: [{ situation: 'rereview', prompt: rereview }],
    problem: '',
  }
}

// GuideLaunchOptions are the launch options the sample's card offers (ItemRow's
// LaunchOptions): the agent alone, and nothing to save or open, since the guide's card
// takes no clicks.
export interface GuideLaunchOptions {
  agents: readonly AgentInfo[]
  profiles: readonly Profile[]
  prompts: readonly Prompt[]
  launchDefaults: readonly LaunchDefaults[]
  onDefaultsSaved: () => Promise<void>
  onManagePrompts: () => void
  onSettingsSaved: (settings: Settings) => void
}

// guideLaunchOptions are the launch options the sample's card offers.
export function guideLaunchOptions(): GuideLaunchOptions {
  return {
    agents: [guideAgent],
    profiles: [],
    prompts: [],
    launchDefaults: [],
    onDefaultsSaved: () => Promise.resolve(),
    onManagePrompts: () => undefined,
    onSettingsSaved: () => undefined,
  }
}
