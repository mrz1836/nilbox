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
  | 'snoozing'
  | 'reviewing'
  | 'launching'
  | 'worktrees'
  | 'profiles'
  | 'repositories'
  | 'trust'
  | 'files'
  | 'about'

// sectionOrder is the sidebar's order.
export const sectionOrder: readonly SettingsSection[] = [
  'account',
  'appearance',
  'notifications',
  'snoozing',
  'reviewing',
  'launching',
  'worktrees',
  'profiles',
  'repositories',
  'trust',
  'files',
  'about',
]

// sectionInfo is each section's heading and summary.
export const sectionInfo: Readonly<Record<SettingsSection, { label: string; description: string }>> = {
  account: {
    label: 'Account',
    description: 'The GitHub account your inbox comes from, and what it lets the inbox show.',
  },
  appearance: { label: 'Appearance', description: 'The theme, the text size, and the size the window opens at.' },
  notifications: {
    label: 'Notifications',
    description: 'The menu bar icon, and the banners that tell you when something new needs you.',
  },
  snoozing: {
    label: 'Snoozing',
    description: 'How long a snooze lasts, and what new activity brings an item back early.',
  },
  reviewing: {
    label: 'Reviewing',
    description: 'How the review sheet merges a pull request, and where its replies are kept.',
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
    description: 'Where your local clones live, and the launch defaults for orgs and repositories.',
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
}

// agentNames are the coding agents' names, as their rows' labels hold them, and as people
// call them.
const agentNames = ['Claude', 'Claude Code', 'Codex']

// agents are the coding agents' names as a template's examples, each with what people
// call it besides.
const agents: Readonly<Record<string, readonly string[]>> = { Claude: ['Claude Code'], Codex: [] }

// banner is a notification kind's switch, under its label.
function banner(session: boolean): SettingEntry[] {
  return notificationKinds
    .filter((k) => (k.session === true) === session)
    .map((k) => ({ label: k.label, keywords: [k.title, 'banner', 'notify'] }))
}

// settingsIndex lists each section's settings in the order the section shows them.
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
    { label: 'Banners', keywords: ['notifications', 'alerts'] },
    ...banner(false),
    { label: 'Test', keywords: ['test notification', 'send a test', 'sample banner', 'permission'] },
    { label: 'Agent sessions', keywords: ['session banners', ...agentNames] },
    ...banner(true),
  ],
  snoozing: [
    { label: 'Snooze for', keywords: ['snooze length', 'days', 'default snooze', 'duration'] },
    { label: 'Wake early', keywords: ['wake rules', 'back early', 'unsnooze'] },
    { label: 'New commits', keywords: ['wake', 'push', 'force-push'] },
    { label: 'Reviews', keywords: ['wake', 'approval', 'changes requested'] },
    { label: 'New comments', keywords: ['wake', 'bots'] },
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
    { label: 'Start in a worktree', keywords: ['worktree default', 'git worktree'] },
    { label: 'Follow agent sessions', keywords: ['session status', 'transcript', 'working', 'waiting'] },
    { label: 'Set Codex’s effort at launch', keywords: ['reasoning effort', 'config override'] },
    { label: 'Start Codex in Fast mode', keywords: ['fast', 'speed'] },
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
    { label: 'Clone root', keywords: ['clones', 'projects folder', 'local repositories', 'path', 'directory'] },
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
  about: [
    {
      label: 'Version',
      keywords: [brand.name, 'build', 'commit', 'release', 'system', 'update', 'report a bug', 'feedback'],
    },
    { label: 'Coding agents', keywords: ['agent versions', 'installed', ...agentNames] },
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

// matchSettings finds the settings a query names, or null when the query filters
// nothing. Settings take words only: a key:value is looked for as written.
export function matchSettings(query: string): SettingsMatch | null {
  const { terms } = parseQuery(query, { keys: [] })

  if (terms.length === 0) {
    return null
  }

  const found = indexed.filter(({ hay }) => matchesTerms(terms, hay))
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
