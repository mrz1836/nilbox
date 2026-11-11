// Hooks as the Hooks page and the run panel word them: the groups, a row's state and its
// button, what an action asks before it runs, the checks, and how an action or a checks
// run came out.

import type {
  HookAction,
  HookActionOutcome,
  HookActionResult,
  HookChangedFile,
  HookCheckState,
  HookChecksResult,
  HookClone,
  HookGroup,
  HookLastRun,
  HookRepo,
  ScriptRun,
} from '../platform'
import { counted, formatCount } from './format'
import { formatDuration, isLive } from './scriptRuns'

// Tone is how a line reads: done, a note, a warning, what went wrong, or set aside.
export type Tone = 'ok' | 'info' | 'warn' | 'bad' | 'quiet'

// Worded is a line in words, with its tone.
export interface Worded {
  tone: Tone
  text: string
}

// hookGroups are the page's groups of clones with a go-pre-commit config, in order: what
// each is called, and whether it starts open. The clones without one follow, collapsed.
export const hookGroups: readonly { id: HookGroup; title: string; open: boolean }[] = [
  { id: 'attention', title: 'Needs attention', open: true },
  { id: 'ready', title: 'Ready', open: true },
  { id: 'off', title: 'Turned off in the repository', open: false },
]

// attentionCount counts the repositories that need attention, which the rail shows beside
// Hooks.
export function attentionCount(repos: readonly HookRepo[] | undefined): number {
  return repos?.filter((r) => r.group === 'attention').length ?? 0
}

// holds reports whether text, lowercased, holds want, which is lowercased already.
function holds(text: string, want: string): boolean {
  return text.toLowerCase().includes(want)
}

// matchHooks keeps the repositories whose name, "owner/name", or folder holds the search,
// trimmed and in any case, in their order.
export function matchHooks(repos: readonly HookRepo[], search: string): HookRepo[] {
  const want = search.trim().toLowerCase()

  return repos.filter((r) => holds(r.name, want) || holds(r.repo, want) || holds(r.dir, want))
}

// matchClones keeps the clones without a config whose name or folder holds the search, as
// matchHooks does.
export function matchClones(clones: readonly HookClone[], search: string): HookClone[] {
  const want = search.trim().toLowerCase()

  return clones.filter((c) => holds(c.name, want) || holds(c.dir, want))
}

// installable reports a row Install and update all covers: one that needs go-pre-commit's
// hook installed, updated, or written again, in a hooks folder of its own. It never
// replaces another tool's hook, nor touches a shared folder.
export function installable(repo: HookRepo): boolean {
  return (
    repo.group === 'attention' &&
    !repo.shared &&
    (repo.action === 'install' || repo.action === 'update' || repo.action === 'reinstall')
  )
}

// sharedWord says how many repositories use a shared hooks folder, as a row says it: " (6
// repos)", or nothing when only this one is known to.
function sharedWord(repo: Pick<HookRepo, 'sharedWith'>): string {
  return repo.sharedWith > 1 ? ` (${formatCount(repo.sharedWith)} repos)` : ''
}

// ownerOf names whose hook another tool's is: "husky’s", or "another tool’s".
export function ownerOf(otherTool: string): string {
  return otherTool === '' ? 'another tool’s' : `${otherTool}’s`
}

// hookState says what a row's pre-commit hook is, in a few words: a shared hooks folder,
// when its button would change one, and otherwise the hook itself.
export function hookState(repo: HookRepo): Worded {
  if (repo.shared && actionButton(repo) !== null) {
    return { tone: 'warn', text: `Shared hooks folder ${repo.hooksDir}${sharedWord(repo)}` }
  }

  switch (repo.state) {
    case 'missing':
      return { tone: 'bad', text: 'No hook yet' }
    case 'outdated':
      return { tone: 'warn', text: 'Outdated hook' }
    case 'notExecutable':
      return { tone: 'warn', text: 'The hook isn’t executable' }
    case 'other':
      return {
        tone: 'warn',
        text:
          repo.otherTool === '' ? 'Another tool’s hook is there' : `Another tool’s hook is there (${repo.otherTool})`,
      }
    case 'ready':
      return { tone: 'ok', text: 'Installed' }
    case 'unknown':
      return { tone: 'info', text: 'Installed · go-pre-commit couldn’t say whether it’s current' }
    case 'unreadable':
      return { tone: 'bad', text: 'Couldn’t read the hooks' }
  }
}

// ours reports a row whose pre-commit hook is go-pre-commit's own, which Reinstall writes
// again.
export function ours(repo: HookRepo): boolean {
  return (
    repo.state === 'ready' || repo.state === 'outdated' || repo.state === 'notExecutable' || repo.state === 'unknown'
  )
}

// uninstallable reports a row with a hook of go-pre-commit's to uninstall: its pre-commit
// hook, or another type that carries go-pre-commit's.
export function uninstallable(repo: HookRepo): boolean {
  return ours(repo) || repo.alsoInstalled.length > 0
}

// actionWords name each action as a button does.
export const actionWords: Readonly<Record<HookAction, string>> = {
  install: 'Install',
  update: 'Update',
  reinstall: 'Reinstall',
  replace: 'Replace',
  uninstall: 'Uninstall',
}

// ActionButton is a row's button: its action, its words, and whether it asks first.
export interface ActionButton {
  action: HookAction
  label: string
  asks: boolean
}

// actionButton is the button of a row that needs attention, or null for none: Install,
// Update, or Reinstall, which run at once, and Replace…, or any of them in a shared hooks
// folder, which ask first. A row turned off in its repository has none.
export function actionButton(repo: HookRepo): ActionButton | null {
  if (repo.group !== 'attention' || repo.action === '') {
    return null
  }

  const asks = repo.action === 'replace' || repo.shared

  return { action: repo.action, label: `${actionWords[repo.action]}${asks ? '…' : ''}`, asks }
}

// inClone writes a hooks folder relative to its clone, as .git/hooks, or as it is when it's
// elsewhere.
function inClone(hooksDir: string, dir: string): string {
  return hooksDir.startsWith(`${dir}/`) ? hooksDir.slice(dir.length + 1) : hooksDir
}

// replaceQuestion asks before go-pre-commit sets another tool's hook aside.
export function replaceQuestion(repo: Pick<HookRepo, 'name' | 'dir' | 'hooksDir' | 'otherTool'>): string {
  const folder = inClone(repo.hooksDir, repo.dir)

  return `Replace ${ownerOf(repo.otherTool)} pre-commit hook in ${repo.name}? go-pre-commit moves it to ${folder}/pre-commit.backup.<number>, and puts it back if you uninstall.`
}

// sharedQuestion asks before an action changes a hooks folder other repositories share:
// how many of the user's own share it, and that the change is theirs too.
export function sharedQuestion(repo: Pick<HookRepo, 'hooksDir' | 'sharedWith'>, action: HookAction): string {
  const users =
    repo.sharedWith > 1
      ? `${formatCount(repo.sharedWith)} of your repositories, and by any other repository that uses it`
      : 'other repositories'
  const change =
    action === 'uninstall'
      ? 'Uninstalling here uninstalls go-pre-commit’s hooks'
      : 'Installing here installs go-pre-commit’s hook'

  return `${repo.hooksDir} is shared by ${users}. ${change} for all of them.`
}

// uninstallQuestion says what Uninstall removes, and what it puts back.
export function uninstallQuestion(repo: HookRepo): string {
  const types = [...(ours(repo) ? ['pre-commit'] : []), ...repo.alsoInstalled]
  const hooks = types.length === 1 ? 'hook' : 'hooks'
  const back = repo.backups.at(-1)
  const text = `go-pre-commit removes its ${types.join(' and ')} ${hooks} from ${repo.name}.`

  return back === undefined ? text : `${text} It puts back the hook it set aside, ${back}.`
}

// Question is what a row asks before an action: its title, what it says, the button that
// goes ahead, and whether that button warns.
export interface Question {
  title: string
  text: string
  confirm: string
  danger: boolean
}

// question is what a row asks before action: Replace… and Uninstall… always, saying what
// happens to the other tool's hook, and any action in a shared hooks folder, saying which
// repositories it changes too.
export function question(repo: HookRepo, action: HookAction): Question {
  const shared = repo.shared ? ` ${sharedQuestion(repo, action)}` : ''

  switch (action) {
    case 'replace':
      return {
        title: `Replace ${ownerOf(repo.otherTool)} hook?`,
        text: `${replaceQuestion(repo)}${shared}`,
        confirm: `Replace ${ownerOf(repo.otherTool)} hook`,
        danger: true,
      }
    case 'uninstall':
      return {
        title: `Uninstall the hook from ${repo.name}?`,
        text: `${uninstallQuestion(repo)}${shared}`,
        confirm: 'Uninstall the hook',
        danger: true,
      }
    case 'install':
    case 'update':
    case 'reinstall':
      return {
        title: `${actionWords[action]} the hook in a shared folder?`,
        text: sharedQuestion(repo, action),
        confirm: `${actionWords[action]} for all of them`,
        danger: false,
      }
  }
}

// lastChecksWords say how a row's newest checks run went, in a few words, or '' for none.
export function lastChecksWords(last: HookLastRun): string {
  if (last.id === 0) {
    return ''
  }

  if (isLive(last.status)) {
    return 'checks running'
  }

  switch (last.status) {
    case 'succeeded':
      return 'checks passed'
    case 'failed':
      return 'checks failed'
    default:
      return `checks ${last.status}`
  }
}

// ciNote says which go-pre-commit the repository's CI pins when it isn't the one in use:
// information, not a warning. It is '' when they match or either isn't known.
export function ciNote(ciVersion: string, version: string): string {
  if (ciVersion === '' || version === '' || ciVersion === version) {
    return ''
  }

  return `This repository’s CI uses go-pre-commit v${ciVersion} (GO_PRE_COMMIT_VERSION); you have ${version}.`
}

// busyRun is the run going in a clone, which a busy row's Watch opens, or null when none
// is listed, as for a worktree's change.
export function busyRun(runs: readonly ScriptRun[] | null, dir: string): number | null {
  return runs?.find((r) => isLive(r.status) && r.dir === dir)?.id ?? null
}

// liveDirs are the folders runs go in now, sorted, as one key: it changes only when a run
// starts or ends in a clone, which is when the page reads which clones are busy again.
export function liveDirs(runs: readonly ScriptRun[] | null): string {
  const dirs = new Set((runs ?? []).filter((r) => isLive(r.status)).map((r) => r.dir))

  return [...dirs].sort().join('\n')
}

// outcomeTones mark how each action came out.
export const outcomeTones: Readonly<Record<HookActionOutcome, Tone>> = {
  installed: 'ok',
  updated: 'ok',
  alreadyCurrent: 'ok',
  replaced: 'ok',
  uninstalled: 'ok',
  nothingToUninstall: 'ok',
  nothingInstalled: 'warn',
  unverified: 'warn',
  failed: 'bad',
  skipped: 'quiet',
}

// blocked reports an action that installed nothing because another tool's hook was in the
// way, which Replace… fixes.
export function blocked(result: Pick<HookActionResult, 'outcome'>): boolean {
  return result.outcome === 'nothingInstalled'
}

// checkTones mark each check's state, and checkSuffix says the states the mark alone
// wouldn't: a check the repository turns off, one that didn't run, and one skipped.
export const checkTones: Readonly<Record<HookCheckState, Tone>> = {
  passed: 'ok',
  failed: 'bad',
  skipped: 'warn',
  off: 'quiet',
  notRun: 'quiet',
}

export const checkSuffix: Readonly<Record<HookCheckState, string>> = {
  passed: '',
  failed: '',
  skipped: ' (skipped)',
  off: ' (off)',
  notRun: ' (not run)',
}

// checksHeading heads a checks run's result: which checks, in which clone, on which files.
export function checksHeading(result: Pick<HookChecksResult, 'scope' | 'check' | 'name'>): string {
  switch (result.scope) {
    case 'staged':
      return `Checks in ${result.name} · staged files`
    case 'one':
      return `${result.check} in ${result.name} · all files`
    case 'all':
      return `Checks in ${result.name} · all files`
  }
}

// checksTone is how a checks run came out, by its counts: a check failed, go-pre-commit
// erred, a check was skipped, or every one that ran passed.
export function checksTone(result: Pick<HookChecksResult, 'failed' | 'skipped' | 'errors' | 'passed'>): Tone {
  if (result.failed > 0 || result.errors.length > 0) {
    return 'bad'
  }

  if (result.skipped > 0) {
    return 'warn'
  }

  return result.passed > 0 ? 'ok' : 'info'
}

// checksTook says how long the checks took, as go-pre-commit said: "350 ms", "38 s", "1
// min 12 s", or '' when it didn't say.
export function checksTook(ms: number): string {
  if (ms <= 0) {
    return ''
  }

  return ms < 1000 ? `${String(ms)} ms` : formatDuration(ms)
}

// changedHeading says how many files the checks changed.
export function changedHeading(files: readonly HookChangedFile[]): string {
  return `Changed ${counted(files.length, 'file', 'files')}:`
}

// changedNote says how the checks changed a file, in a few words, or '' when there is
// nothing to say beyond that they did: fixed, and staged too, by which checks, and that
// it had uncommitted changes before, which the fix may be mixed into.
export function changedNote(file: HookChangedFile): string {
  const notes: string[] = []
  const fixed = file.staged ? 'fixed and staged' : 'fixed'

  if (file.by.length > 0) {
    notes.push(`${fixed} by ${file.by.join(' and ')}`)
  } else if (file.staged) {
    notes.push('staged')
  }

  if (file.wasDirty) {
    notes.push('already had uncommitted changes')
  }

  return notes.join('; ')
}
