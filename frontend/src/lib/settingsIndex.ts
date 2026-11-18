// What "Find a setting" searches: each settings section's settings by the labels their
// rows show, with the words someone might look for them by. The settings page's rows
// carry the same labels (data-setting), which is how a match is highlighted, and a drift
// test renders every section to keep the two in step.
//
// Nothing here imports a settings component: the window's toolbar counts the matches
// before the settings page has ever loaded, and the page's code stays out of the script
// the window runs before its first paint.

import { brand } from '../generated/brand'
import { notificationKinds } from './notifications'
import { fold, haystack, matchesTerms, parseQuery, type Term } from './query'

// SettingsSection is one section of the settings page.
export type SettingsSection =
  | 'account'
  | 'appearance'
  | 'notifications'
  | 'github'
  | 'refreshing'
  | 'status'
  | 'snoozing'
  | 'cards'
  | 'reviewing'
  | 'autopilot'
  | 'launching'
  | 'worktrees'
  | 'profiles'
  | 'repositories'
  | 'trust'
  | 'files'
  | 'integrations'
  | 'labs'
  | 'backup'
  | 'about'

// SectionGroup is a run of the sidebar's sections under one heading, or under none when
// heading is ''.
export interface SectionGroup {
  heading: string
  sections: readonly SettingsSection[]
}

// sectionGroups is the sidebar, in the order the app is used: the settings of the whole
// app, then the inbox's, then Autopilot's, which acts on the inbox's items, then the
// agents' in the order they are set up (where the clones live, the profiles a launch runs
// with, the launch itself and what it trusts, and the worktrees it leaves behind), then
// the tools, then what is still being built (Labs), and last the setup as a whole, kept in
// a backup, and About. Autopilot's group shows only while Autopilot (beta) is on (Hidden).
export const sectionGroups: readonly SectionGroup[] = [
  { heading: '', sections: ['account', 'appearance', 'notifications', 'github', 'refreshing', 'status'] },
  { heading: 'Inbox', sections: ['snoozing', 'cards', 'reviewing'] },
  { heading: 'Autopilot', sections: ['autopilot'] },
  { heading: 'Agents', sections: ['repositories', 'profiles', 'launching', 'trust', 'files', 'worktrees'] },
  { heading: 'Tools', sections: ['integrations'] },
  { heading: '', sections: ['labs', 'backup', 'about'] },
]

// Hidden says which settings that come and go the window leaves out now: Autopilot's
// section, and its banner switches in Notifications, while Autopilot (beta) is off.
export interface Hidden {
  autopilot: boolean
}

// hiddenOf is what the settings hide: Autopilot's settings while it is off, and while the
// settings load, since a fresh install has it off.
export function hiddenOf(settings: { autopilot: { enabled: boolean } } | null): Hidden {
  return { autopilot: settings?.autopilot.enabled !== true }
}

// nothingHidden hides no setting.
const nothingHidden: Hidden = { autopilot: false }

// shownSection reports whether the settings page shows section, as hidden says.
export function shownSection(section: SettingsSection, hidden: Hidden = nothingHidden): boolean {
  return !(hidden.autopilot && section === 'autopilot')
}

// sectionOrder is the sidebar's order, its headings aside.
export const sectionOrder: readonly SettingsSection[] = sectionGroups.flatMap((group) => group.sections)

// sectionInfo is each section's heading and summary.
export const sectionInfo: Readonly<Record<SettingsSection, { label: string; description: string }>> = {
  account: {
    label: 'Account',
    description: 'The GitHub account your inbox comes from, and what it lets the inbox show.',
  },
  appearance: { label: 'Appearance', description: 'The theme, the text size, and the size the window opens at.' },
  notifications: {
    label: 'Notifications',
    description: 'The menu bar icon, how long toasts stay, and the banners that tell you when something new needs you.',
  },
  github: {
    label: 'GitHub notifications',
    description:
      'Whether the app reads your notifications on GitHub, and clears them there once the work behind them is done.',
  },
  refreshing: {
    label: 'Refreshing',
    description:
      'How often the app reads the inbox, the running checks, the Release page’s running checks, the worktrees, and the GitHub notifications again on its own.',
  },
  status: {
    label: 'Service status',
    description:
      'The status pages of the services the app depends on, how often they are checked, and which parts of each you watch.',
  },
  snoozing: {
    label: 'Snoozing',
    description: 'How long a snooze lasts, and what new activity brings an item back early.',
  },
  cards: {
    label: 'Cards',
    description: 'What the inbox’s cards leave out, such as the labels their own size already shows.',
  },
  reviewing: {
    label: 'Reviewing',
    description: 'How the review sheet merges a pull request, and where its replies are kept.',
  },
  autopilot: {
    label: 'Autopilot',
    description:
      'Pause Autopilot, the mode new routines start in, the limits every routine keeps to, and how long its history is kept.',
  },
  launching: {
    label: 'Launching',
    description: 'How a launch starts a coding agent: the app defaults every launch starts from.',
  },
  worktrees: {
    label: 'Worktrees',
    description:
      'How the worktrees sessions run in are cleaned up, which branches go with them, and the branches that never go.',
  },
  profiles: {
    label: 'Profiles',
    description: 'The agent configurations a session can start with, and the folder each one runs from.',
  },
  repositories: {
    label: 'Repositories',
    description:
      'Where your local clones live, the orgs and repositories you pinned or muted, and the launch defaults for orgs and repositories.',
  },
  trust: {
    label: 'Trust',
    description: 'Who launches trust with the profile’s permissions, and which bots count.',
  },
  files: {
    label: 'Sensitive files',
    description:
      'The changed files to point out on pull requests from outside the repository, before you review them or open a session on them.',
  },
  integrations: {
    label: 'Integrations',
    description: `Tools ${brand.name} runs for you: mage-x releases, go-pre-commit hooks, and scripts.`,
  },
  labs: {
    label: 'Labs',
    description: `Parts of ${brand.name} still being built, to try before they are done. Each is off until you turn it on.`,
  },
  backup: {
    label: 'Back up and restore',
    description:
      'Your whole setup in one file: settings, repositories, prompts and replies, launch defaults, your scripts, and your Autopilot routines, to keep or to bring to another Mac.',
  },
  about: { label: 'About', description: '' },
}

// Template stands for rows whose labels the backend words, one per coding agent, as in
// "Codex profile": pattern matches those labels, and examples are the names such a label
// can hold, each with other words for it, so a search for any of them finds the setting
// and marks the rows of the name it found.
export interface Template {
  pattern: RegExp
  examples: Readonly<Record<string, readonly string[]>>
}

// SettingEntry is one setting a section shows, by its row's label.
export interface SettingEntry {
  label: string
  // keywords are other words it is looked for by.
  keywords: readonly string[]
  template?: Template
  // autopilot marks one of Autopilot's settings outside its own section, which shows, as
  // that section does, only while Autopilot (beta) is on.
  autopilot?: boolean
}

// agentNames are the coding agents' names, as their rows' labels hold them, and as people
// call them.
const agentNames = ['Claude', 'Claude Code', 'Codex']

// agents are the coding agents' names as a template's examples, each with what people
// call it besides.
const agents: Readonly<Record<string, readonly string[]>> = { Claude: ['Claude Code'], Codex: [] }

// BannerGroup is one of the groups Settings → Notifications lists the banner switches in.
type BannerGroup = 'inbox' | 'autopilot' | 'session'

// banner is each switch of a group of notification kinds, under its label; Autopilot's are
// found by Autopilot's name and its routines too, and only while Autopilot is on.
function banner(group: BannerGroup): SettingEntry[] {
  return notificationKinds
    .filter((k) => (k.session === true ? 'session' : k.autopilot === true ? 'autopilot' : 'inbox') === group)
    .map((k) =>
      group === 'autopilot'
        ? { label: k.label, keywords: [k.title, 'banner', 'notify', 'Autopilot', 'routine'], autopilot: true }
        : { label: k.label, keywords: [k.title, 'banner', 'notify'] },
    )
}

// settingsIndex lists each section's settings in the order the section shows them, with
// Autopilot (beta) on: off, Autopilot's section and the entries marked autopilot don't show.
export const settingsIndex: Readonly<Record<SettingsSection, readonly SettingEntry[]>> = {
  account: [
    {
      label: 'GitHub account',
      keywords: ['sign in', 'signed in', 'login', 'gh auth switch', 'GitHub CLI', 'token', 'switch account'],
    },
    { label: 'Projects on issues', keywords: ['GitHub projects', 'project status', 'read:project', 'scope'] },
  ],
  appearance: [
    { label: 'Theme', keywords: ['dark mode', 'light mode', 'system', 'colors'] },
    { label: 'Text size', keywords: ['font', 'font size', 'zoom', 'larger text', 'scale', 'accessibility'] },
    { label: 'Window size', keywords: ['window', 'resize', 'dimensions', 'opens at'] },
  ],
  notifications: [
    { label: 'Show in the menu bar', keywords: ['menu bar icon', 'tray', 'status bar', 'badge', 'keep running'] },
    {
      label: 'Toasts stay for',
      keywords: ['toast', 'message', 'undo', 'how long', 'duration', 'seconds', 'timeout', 'dismiss', 'auto close'],
    },
    { label: 'Banners', keywords: ['notifications', 'alerts'] },
    ...banner('inbox'),
    { label: 'Test', keywords: ['test notification', 'send a test', 'sample banner', 'permission'] },
    { label: 'Autopilot', keywords: ['Autopilot banners', 'routines', 'notices'], autopilot: true },
    ...banner('autopilot'),
    { label: 'Agent sessions', keywords: ['session banners', ...agentNames] },
    ...banner('session'),
  ],
  // The switches of reading and clearing, the moments that clear, what else counts, and
  // what clearing does. Two of the moments name the product.
  github: [
    {
      label: 'Read my GitHub notifications',
      keywords: ['GitHub inbox', 'notifications', 'threads', 'unread', 'mirror', 'turn off', 'refused', 'token'],
    },
    {
      label: 'Clear them automatically',
      keywords: ['automatic clearing', 'auto clear', 'clean up', 'cleanup', 'housekeeping', 'inbox zero'],
    },
    { label: 'When to clear', keywords: ['triggers', 'finished', 'clearing'] },
    { label: 'A pull request is merged', keywords: ['merge', 'trigger'] },
    { label: 'A pull request is closed without merging', keywords: ['closed', 'trigger'] },
    { label: 'An issue is closed', keywords: ['closed', 'trigger'] },
    {
      label: 'You approve or request changes on a pull request',
      keywords: ['approve', 'request changes', 'review', 'agent', 'trigger'],
    },
    { label: `You dismiss an item in ${brand.name}`, keywords: ['dismissed', 'archive', 'trigger'] },
    {
      label: 'You unassign yourself or remove yourself as reviewer',
      keywords: ['unassigned', 'review request', 'trigger'],
    },
    { label: 'Include repositories you only watch', keywords: ['watching', 'subscribed', 'watched repositories'] },
    {
      label: 'Count bot activity after it closes as seen',
      keywords: ['bots', 'dependabot', 'github-actions', 'bot comments'],
    },
    { label: 'What clearing does', keywords: ['mark done', 'mark read', 'done', 'read'] },
  ],
  snoozing: [
    { label: 'Snooze for', keywords: ['snooze length', 'days', 'default snooze', 'duration'] },
    { label: 'Wake early', keywords: ['wake rules', 'back early', 'unsnooze'] },
    { label: 'New commits', keywords: ['wake', 'push', 'force-push'] },
    { label: 'Reviews', keywords: ['wake', 'approval', 'changes requested'] },
    { label: 'New comments', keywords: ['wake', 'bots'] },
    { label: 'Marked ready for review', keywords: ['wake', 'draft', 'ready'] },
  ],
  cards: [
    {
      label: 'Hidden labels',
      keywords: ['labels', 'size/*', 'size labels', 'hide', 'chips', 'card labels', 'patterns', 'restore the defaults'],
    },
  ],
  reviewing: [
    {
      label: 'Merge method',
      keywords: ['squash', 'merge commit', 'rebase', 'approve and merge', 'merge button', 'review sheet'],
    },
    {
      label: 'Review replies',
      keywords: ['reply', 'LGTM', 'approval', 'request changes', 'comment', 'templates', 'review text'],
    },
  ],
  // The switch and the default mode, the limits, what holds an agent back, the history,
  // and the engine's heartbeat.
  autopilot: [
    { label: 'Pause Autopilot', keywords: ['pause', 'stop', 'resume', 'hold', 'routines', 'nothing new starts'] },
    {
      label: 'New routines start in',
      keywords: ['default mode', 'dry run', 'ask first', 'autopilot mode', 'on its own', 'new routine'],
    },
    { label: 'Limits', keywords: ['caps', 'limit', 'quota', 'routines', 'a day', 'midnight'] },
    { label: 'Agents at once', keywords: ['coding agents', 'parallel', 'concurrent', 'slots', ...agentNames] },
    { label: 'Agent runs a day', keywords: ['daily runs', 'coding agents', 'cap', 'limit'] },
    { label: 'GitHub actions a day', keywords: ['rebase', 'update branch', 'labels', 'writes', 'cap', 'limit'] },
    { label: 'Daily cap', keywords: ['budget', 'spend', 'cost', 'dollars', 'usd', 'tokens', 'money', 'limit'] },
    { label: 'While it runs', keywords: ['agents', 'sleep', 'outage', 'catch up'] },
    {
      label: 'Keep the Mac awake while agents work',
      keywords: ['sleep', 'awake', 'caffeinate', 'power', 'lid', 'energy'],
    },
    {
      label: 'Hold agent starts while their provider reports an outage',
      keywords: ['outage', 'incident', 'status page', 'service status', 'down', ...agentNames, 'OpenAI'],
    },
    {
      label: 'Ask before catching up after',
      keywords: ['catch up', 'catch-up', 'long gap', 'closed', 'asleep', 'away', 'weekend', 'hours'],
    },
    { label: 'History', keywords: ['retention', 'activity', 'ledger', 'kept', 'days'] },
    { label: 'History kept', keywords: ['retention', 'activity', 'ledger', 'days', 'prune'] },
    { label: 'Run logs kept', keywords: ['logs', 'transcripts', 'retention', 'days', 'prune'] },
    { label: 'Clear history', keywords: ['clear', 'delete history', 'forget', 'activity', 'ledger', 'wipe'] },
    { label: 'Stop all', keywords: ['stop', 'agents', 'kill', 'background', 'runs', 'halt'] },
    {
      label: 'This Mac',
      keywords: [
        'readiness',
        'ready',
        'not ready',
        'check again',
        'sign in',
        'installed',
        'PATH',
        'profiles',
        'terminal',
        'what is missing',
        ...agentNames,
      ],
    },
    {
      label: 'Last checked',
      keywords: ['engine', 'heartbeat', 'cycle', 'last run', 'nothing happens', 'another copy', 'status'],
    },
  ],
  launching: [
    { label: 'Terminal', keywords: ['iTerm2', 'terminal app', 'open sessions in'] },
    { label: 'Default agent', keywords: ['coding agent', ...agentNames] },
    {
      label: 'Agent profile',
      keywords: ['default profile', 'config'],
      template: { pattern: /^.+ profile$/, examples: agents },
    },
    { label: 'Prompt for pull requests', keywords: ['default prompt', 'PR prompt', 'review prompt'] },
    { label: 'Prompt for issues', keywords: ['default prompt', 'issue prompt'] },
    {
      label: 'Prompt when checks fail',
      keywords: ['situation prompt', 'failing checks', 'CI', 'Fix checks', 'default prompt'],
    },
    {
      label: 'Prompt when it has conflicts',
      keywords: ['situation prompt', 'merge conflicts', 'Resolve conflicts', 'default prompt'],
    },
    {
      label: 'Prompt when changes are requested',
      keywords: ['situation prompt', 'review feedback', 'Address review', 'default prompt'],
    },
    {
      label: 'Prompt when you’re asked to review again',
      keywords: ['situation prompt', 'Re-review', 'pushed since your review', 're-requested', 'default prompt'],
    },
    { label: 'Start in a worktree', keywords: ['worktree default', 'git worktree'] },
    { label: 'Follow agent sessions', keywords: ['session status', 'transcript', 'working', 'waiting'] },
    { label: 'Set Codex’s effort at launch', keywords: ['reasoning effort', 'config override'] },
    { label: 'Start Codex in Fast mode', keywords: ['fast', 'speed'] },
    {
      label: 'Use an API key for Claude Code when one is set',
      keywords: ['API key', 'ANTHROPIC_API_KEY', 'billing', 'sign in', 'subscription', 'Claude'],
    },
    {
      label: 'Use an API key for Codex when one is set',
      keywords: ['API key', 'OPENAI_API_KEY', 'billing', 'sign in', 'subscription', 'OpenAI'],
    },
  ],
  refreshing: [
    { label: 'How often', keywords: ['interval', 'refresh', 'poll', 'API budget', 'rate limit'] },
    { label: 'Inbox', keywords: ['refresh every', 'five minutes', 'interval', 'poll'] },
    { label: 'Running checks', keywords: ['CI', 'checks', 'pending', 'yellow', 'follow', 'interval'] },
    { label: 'Release checks', keywords: ['Release page', 'CI', 'checks', 'default branch', 'follow', 'interval'] },
    { label: 'Worktrees', keywords: ['sweep', 'cleanup', 'interval'] },
    { label: 'GitHub notifications', keywords: ['notifications', 'GitHub inbox', 'poll interval', 'interval'] },
  ],
  // How often, then one card per status page, by the provider's name, the backend's
  // catalog: each card's parts are found by the words for its provider.
  status: [
    {
      label: 'Check every',
      keywords: ['interval', 'status page', 'incident', 'outage', 'downtime', 'off', 'poll'],
    },
    { label: 'What to watch', keywords: ['parts', 'components', 'tick', 'relevant', 'warning', 'toolbar'] },
    {
      label: 'Status page',
      keywords: ['provider', 'incident', 'outage', 'components', 'parts', 'turn off'],
      template: {
        pattern: /^.+ status page$/,
        examples: {
          GitHub: ['githubstatus.com', 'Git Operations', 'API Requests', 'Pull Requests', 'Actions', 'Copilot'],
          Claude: ['Anthropic', 'status.claude.com', 'Claude API', 'Claude Code', 'claude.ai'],
          OpenAI: ['Codex', 'Codex CLI', 'ChatGPT', 'Responses', 'status.openai.com'],
        },
      },
    },
  ],
  worktrees: [
    { label: 'Clean up after merging', keywords: ['automatic cleanup', 'auto cleanup', 'remove worktrees', 'sweep'] },
    { label: 'Branches', keywords: ['delete branches', 'branch rules'] },
    { label: 'Delete the worktree’s own branch', keywords: ['own branch'] },
    { label: 'Delete the pull request’s branch', keywords: ['PR branch', 'head branch'] },
    { label: 'Tick merged branches', keywords: ['merged branches'] },
    { label: 'Protected branches', keywords: ['protect', 'main', 'master', 'never deleted', 'release'] },
  ],
  profiles: [
    {
      label: 'Agent profiles',
      keywords: ['config folder', 'sign in', 'login', 'installed'],
      template: {
        pattern: /^.+ profiles$/,
        // Each agent's config variable names its folder, so it finds that agent's profiles.
        examples: { Claude: ['Claude Code', 'CLAUDE_CONFIG_DIR'], Codex: ['CODEX_HOME'] },
      },
    },
  ],
  repositories: [
    {
      label: 'Clone root',
      keywords: ['clones', 'projects folder', 'local repositories', 'path', 'directory', 'new clones'],
    },
    {
      label: 'Other repository locations',
      keywords: ['locations', 'more folders', 'elsewhere', 'scattered', 'external drive', 'single clone'],
    },
    {
      label: 'Pinned and muted',
      keywords: ['pin repository', 'pin org', 'mute', 'mute org', 'hide repository', 'silence', 'ignore', 'muted tab'],
    },
    { label: 'Launch defaults', keywords: ['org defaults', 'repository defaults', 'organization', 'overrides'] },
  ],
  trust: [
    {
      label: 'Trusted bots and apps',
      keywords: ['bots', 'dependabot', 'renovate', 'github-actions', 'trust a bot'],
    },
    {
      label: 'How trust works',
      keywords: ['outside contributor', 'plan mode', 'read-only', 'collaborator', 'fork', 'permissions'],
    },
  ],
  files: [
    { label: 'Check pull requests from', keywords: ['outside contributors', 'collaborators', 'scope', 'forks'] },
    {
      label: 'What counts as sensitive',
      // The categories are the backend's; their names are here, so a search finds them.
      keywords: [
        'categories',
        'agent configuration',
        'hooks',
        'MCP',
        'workflows',
        'CI',
        'build tooling',
        'review and merge rules',
        'dependencies',
      ],
    },
    { label: 'Your patterns', keywords: ['custom patterns', 'gitignore', 'glob', 'infrastructure', 'deploy'] },
    { label: 'Try a path', keywords: ['test a path', 'check a path'] },
    { label: 'How it works', keywords: ['sensitive check', 'changed files', 'paths'] },
  ],
  // Each tool's card, by the tool's name, with what it does and what its card shows; the
  // Scripts card; then where the runs go.
  integrations: [
    {
      label: 'mage-x',
      keywords: ['magex', 'mage', 'release', 'tools', 'version', 'update', 'install', 'path', 'copy', 'shell'],
    },
    {
      label: 'go-pre-commit',
      keywords: ['pre-commit', 'git hooks', 'checks', 'tools', 'version', 'update', 'install', 'path', 'copy', 'shell'],
    },
    {
      label: 'Scripts',
      keywords: [
        'repo scripts',
        't3.json',
        'VS Code tasks',
        'tasks.json',
        'magex targets',
        'commands',
        'automatic',
        'run panel',
        'show runs',
        'pull',
      ],
    },
    {
      label: 'Runs in',
      keywords: ['runs', 'scripts', 'terminal', 'iTerm2', 'where runs go', 'passphrase', 'ssh', brand.name],
    },
    {
      label: 'Offer a release after you merge a pull request',
      keywords: ['release', 'merge', 'after merge', 'toast', 'magex', 'tag', 'version'],
    },
  ],
  labs: [
    {
      label: 'Autopilot (beta)',
      keywords: ['Autopilot', 'beta', 'experimental', 'preview', 'early access', 'routines', 'turn on', 'automation'],
    },
  ],
  backup: [
    {
      label: 'Back up your setup',
      keywords: ['backup', 'export', 'save', 'settings file', 'another Mac', 'new Mac', 'migrate', 'move', 'transfer'],
    },
    {
      label: 'Restore from a backup',
      keywords: ['import', 'restore', 'merge', 'replace', 'bring back', 'undo', 'before import', 'another Mac'],
    },
    {
      label: 'What a backup never holds',
      keywords: ['GitHub sign-in', 'token', 'caches', 'history', 'triage', 'script trust', 'privacy', 'left out'],
    },
  ],
  about: [
    {
      label: 'Version',
      keywords: [brand.name, 'build', 'commit', 'release', 'system', 'update', 'report a bug', 'feedback'],
    },
    {
      label: 'Updates',
      keywords: [
        'update',
        'upgrade',
        'new version',
        'latest version',
        'check for updates',
        'restart to update',
        'what’s new',
        'release notes',
        'download',
        'install',
        'rehearsal build',
        'move to applications',
        'applications folder',
      ],
    },
    {
      label: 'When a new version is out',
      keywords: [
        'automatic updates',
        'auto update',
        'update mode',
        'download it and tell me',
        'just tell me',
        'don’t check',
        'notify',
        'upgrade',
      ],
    },
    { label: 'Coding agents', keywords: ['agent versions', 'installed', ...agentNames] },
    { label: 'Tools', keywords: ['GitHub CLI', 'gh', 'Git', 'versions', 'installed'] },
    {
      label: 'Setup',
      keywords: [
        'first-run setup',
        'onboarding',
        'wizard',
        'run setup again',
        'welcome',
        'check my setup',
        'get started',
      ],
    },
    {
      label: 'Reset',
      keywords: ['reset', 'start over', 'fresh start', 'wipe', 'clear data', 'factory reset', 'erase', brand.name],
    },
  ],
}

// Indexed is an entry with the text a search matches: its section's heading, its label,
// its keywords, and a template's examples.
interface Indexed {
  section: SettingsSection
  entry: SettingEntry
  hay: string
}

const indexed: readonly Indexed[] = sectionOrder.flatMap((section) =>
  settingsIndex[section].map((entry) => ({
    section,
    entry,
    hay: haystack([
      sectionInfo[section].label,
      entry.label,
      ...entry.keywords,
      ...Object.entries(entry.template?.examples ?? {}).flat(2),
    ]),
  })),
)

// examplesIn lists a template's words for the names a row's label holds.
function examplesIn(template: Template, label: string): string[] {
  const folded = fold(label)

  return Object.entries(template.examples)
    .filter(([name]) => folded.includes(fold(name)))
    .flatMap(([name, words]) => [name, ...words])
}

// SettingsMatch is what a search found.
export interface SettingsMatch {
  // sections counts the matching settings of each section that has any, in the
  // sidebar's order.
  sections: ReadonlyMap<SettingsSection, number>
  // matches counts the matching settings in all.
  matches: number
  // isMatch reports whether the row a section shows with label is one of the matches.
  isMatch: (section: SettingsSection, label: string) => boolean
}

// rowMatches reports whether a row of section, with label, matches terms: a setting
// whose entry matched, or a template's row whose own label matches with the template's
// keywords and the words for the name it holds, so "Codex" finds the Codex profile and
// not Claude's.
function rowMatches(terms: readonly Term[], found: readonly Indexed[], section: SettingsSection, label: string) {
  return found.some(({ section: s, entry }) => {
    if (s !== section) {
      return false
    }

    if (entry.template === undefined) {
      return entry.label === label
    }

    return (
      entry.template.pattern.test(label) &&
      matchesTerms(
        terms,
        haystack([sectionInfo[section].label, label, ...entry.keywords, ...examplesIn(entry.template, label)]),
      )
    )
  })
}

// shownEntry reports whether the settings page shows an entry of section, as hidden says.
function shownEntry({ section, entry }: Indexed, hidden: Hidden): boolean {
  return shownSection(section, hidden) && !(hidden.autopilot && entry.autopilot === true)
}

// matchSettings finds the settings a query names, among those hidden leaves shown (all of
// them by default), or null when the query filters nothing. Settings take words only: a
// key:value is looked for as written.
export function matchSettings(query: string, hidden: Hidden = nothingHidden): SettingsMatch | null {
  const { terms } = parseQuery(query, { keys: [] })

  if (terms.length === 0) {
    return null
  }

  const found = indexed.filter((i) => shownEntry(i, hidden) && matchesTerms(terms, i.hay))
  const sections = new Map<SettingsSection, number>()

  for (const { section } of found) {
    sections.set(section, (sections.get(section) ?? 0) + 1)
  }

  return {
    sections,
    matches: found.length,
    isMatch: (section, label) => rowMatches(terms, found, section, label),
  }
}
