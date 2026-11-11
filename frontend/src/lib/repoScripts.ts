// What Repo scripts shows of a clone's scripts, and how it words them: which sources show,
// and why none does; magex's targets split into its common ones, the repository's own,
// and the rest, the search over them, the Arguments field's words, a task's inputs as the
// form starts them, and the question a repository command asks first.

import type { RepoScripts, RunsIn, ScriptEntry, ScriptInput, ScriptQuestion, ScriptSection } from '../platform'

// questionFiles name where each source's commands come from, as a question names it: a
// custom magex target is the repository's magefiles' code.
const questionFiles: Readonly<Record<ScriptQuestion['source'], string>> = {
  yours: 'your scripts',
  t3: 't3.json',
  tasks: '.vscode/tasks.json',
  magex: 'magefiles',
}

// trustTitle asks whether to run a command from a repository's files: Run “Test” from
// acme-org/sdk’s t3.json?
export function trustTitle(question: ScriptQuestion, repo: string): string {
  return `Run “${question.name}” from ${repo}’s ${questionFiles[question.source]}?`
}

// changedCommand is the command trusted before, when a changed command's question should
// show it above the new one: not when it reads the same, as for a magex target whose
// magefiles changed, which its note says instead.
export function changedCommand(question: ScriptQuestion): string {
  if (question.trust !== 'changed' || question.trusted === '' || question.trusted === question.command) {
    return ''
  }

  return question.trusted
}

// shownSections are the repository's sources the page shows, in its order: each one the
// settings turn on that has scripts, couldn't be read, or, for magex, says why it can't
// list them. The user's own scripts show apart, always (yoursSection).
export function shownSections(page: RepoScripts): ScriptSection[] {
  return page.sections.filter(
    (s) =>
      s.source !== 'yours' &&
      !s.off &&
      (s.entries.length > 0 || s.error !== '' || (s.source === 'magex' && s.note !== '')),
  )
}

// yoursSection is the page's section of the user's own scripts, or null when it has none,
// as while Scripts is off.
export function yoursSection(page: RepoScripts): ScriptSection | null {
  return page.sections.find((s) => s.source === 'yours') ?? null
}

// EmptyReason says why a clone shows no scripts, a sentence each: a file that lists none,
// the files it doesn't have, and the sources turned off. off reports a source turned off
// in Settings → Integrations, where the page links.
export interface EmptyReason {
  lines: string[]
  off: boolean
}

// emptyReason words why page shows no source (shownSections is empty): its t3.json or
// .vscode/tasks.json lists nothing, the clone has neither, or the sources are turned off.
export function emptyReason(page: RepoScripts): EmptyReason {
  const files = page.sections.filter((s) => s.source === 't3' || s.source === 'tasks')
  const on = files.filter((s) => !s.off)
  const found = on.filter((s) => s.found).map((s) => questionFiles[s.source])
  const missing = on.filter((s) => !s.found).map((s) => questionFiles[s.source])
  const off = [
    ...(files.length > 0 && on.length === 0 ? ['Commands from t3.json and VS Code tasks'] : []),
    ...(page.sections.some((s) => s.source === 'magex' && s.off) ? ['magex targets'] : []),
  ]
  const lines: string[] = []

  if (found.length > 0) {
    lines.push(`${page.name}’s ${found.join(' and ')} ${found.length === 1 ? 'lists' : 'list'} no commands.`)
  }

  if (missing.length > 0) {
    lines.push(`${page.name} has no ${missing.join(' or ')}.`)
  }

  if (off.length > 0) {
    lines.push(`${off.join(' and ')} are turned off in Settings → Integrations.`)
  }

  if (lines.length === 0) {
    lines.push(`${page.name} has no scripts to show.`)
  }

  return { lines, off: off.length > 0 }
}

// MagexTargets are magex's section split up: its common targets, for the chips, the
// repository's own targets, and every built-in.
export interface MagexTargets {
  common: ScriptEntry[]
  custom: ScriptEntry[]
  builtins: ScriptEntry[]
}

// magexTargets splits magex's section up. A common target the section doesn't list is
// left out.
export function magexTargets(section: ScriptSection): MagexTargets {
  const byRef = new Map(section.entries.map((e) => [e.ref, e]))

  return {
    common: section.common.flatMap((ref) => byRef.get(ref) ?? []),
    custom: section.entries.filter((e) => e.custom),
    builtins: section.entries.filter((e) => !e.custom),
  }
}

// matchTargets keeps the targets whose name or description holds search, in any case.
export function matchTargets(entries: readonly ScriptEntry[], search: string): ScriptEntry[] {
  const wanted = search.trim().toLowerCase()

  if (wanted === '') {
    return [...entries]
  }

  return entries.filter((e) => e.name.toLowerCase().includes(wanted) || e.detail.toLowerCase().includes(wanted))
}

// argsOf reads the Arguments field: its key=value pairs, separated by spaces.
export function argsOf(text: string): string[] {
  return text.split(/\s+/).filter((word) => word !== '')
}

// firstChoice is the choice a pickString's form starts on, by its place in the list: the
// first whose value is the default, or else the first. Choices are told apart by place,
// since two may fill in the same value.
export function firstChoice(input: ScriptInput): number {
  return Math.max(
    0,
    input.options.findIndex((o) => o.value === input.default),
  )
}

// otherPlace is where a run goes when it goes to the other place from runsIn.
export function otherPlace(runsIn: RunsIn): RunsIn {
  return runsIn === 'app' ? 'terminal' : 'app'
}
