// The choices the launch menu makes for one launch, and how they turn into a launch
// request and into launch defaults.

import type { LaunchDefaults, LaunchDefaultsInput, LaunchRequest, Resolution } from '../platform'

// Choices are the launch menu's own choices. Each is unset (0, '', or null) until the
// user picks something other than what the item's settings resolve to.
export interface Choices {
  promptId: number
  profile: string
  model: string | null
  effort: string | null
  planMode: boolean | null
  worktree: boolean | null
  // override lifts the outside-contributor restriction; it is never saved as a default.
  override: boolean
}

export const noChoices: Choices = {
  promptId: 0,
  profile: '',
  model: null,
  effort: null,
  planMode: null,
  worktree: null,
  override: false,
}

// hasChoices reports whether any choice differs from the resolved settings.
export function hasChoices(c: Choices): boolean {
  return (
    c.promptId !== 0 ||
    c.profile !== '' ||
    c.model !== null ||
    c.effort !== null ||
    c.planMode !== null ||
    c.worktree !== null ||
    c.override
  )
}

// savableChoices reports whether any choice can be saved as a default: all of them but
// the outside-contributor override.
export function savableChoices(c: Choices): boolean {
  return hasChoices({ ...c, override: false })
}

// launchRequest turns the choices into a launch request for an item. The override only
// applies to an untrusted item.
export function launchRequest(itemId: number, c: Choices, untrusted: boolean): LaunchRequest {
  return {
    itemId,
    profile: c.profile,
    promptId: c.promptId,
    model: c.model,
    effort: c.effort,
    planMode: c.planMode,
    worktree: c.worktree,
    fullAccessOverride: untrusted && c.override,
  }
}

// defaultsInput returns stored launch defaults as an editable input.
export function defaultsInput(d: LaunchDefaults): LaunchDefaultsInput {
  return {
    owner: d.owner,
    name: d.name,
    agent: d.agent,
    profile: d.profile,
    prPromptId: d.prPromptId,
    issuePromptId: d.issuePromptId,
    model: d.model,
    effort: d.effort,
    planMode: d.planMode,
    worktree: d.worktree,
    extraFlags: d.extraFlags,
  }
}

// emptyDefaults is launch defaults for owner (and repository name, or '' for the owner)
// that set nothing.
export function emptyDefaults(owner: string, name: string): LaunchDefaultsInput {
  return {
    owner,
    name,
    agent: '',
    profile: '',
    prPromptId: null,
    issuePromptId: null,
    model: '',
    effort: '',
    planMode: null,
    worktree: null,
    extraFlags: '',
  }
}

// findDefaults returns the stored defaults for owner and name (or '' for the owner's),
// matching names ignoring case.
export function findDefaults(all: readonly LaunchDefaults[], owner: string, name: string): LaunchDefaults | undefined {
  return all.find((d) => d.owner.toLowerCase() === owner.toLowerCase() && d.name.toLowerCase() === name.toLowerCase())
}

// withChoices returns launch defaults with the menu's choices for an item of kind
// written over them; settings the menu did not change stay as they were.
export function withChoices(base: LaunchDefaultsInput, c: Choices, kind: string): LaunchDefaultsInput {
  const next = { ...base }

  if (c.profile !== '') {
    next.profile = c.profile
  }

  if (c.promptId !== 0) {
    if (kind === 'pr') {
      next.prPromptId = c.promptId
    } else {
      next.issuePromptId = c.promptId
    }
  }

  if (c.model !== null) {
    next.model = c.model
  }

  if (c.effort !== null) {
    next.effort = c.effort
  }

  if (c.planMode !== null) {
    next.planMode = c.planMode
  }

  if (c.worktree !== null) {
    next.worktree = c.worktree
  }

  return next
}

// Shadowed lists the choices a saved default would not change for this item: the ones
// the chosen prompt sets itself, which win over every default, and, when saving for the
// org, the ones the repository sets itself, which win over the org's.
export interface Shadowed {
  byPrompt: string[]
  byRepo: string[]
}

// shadowedChoices works out which of the choices a default saved for scope would not
// change here, from the resolution under the chosen prompt and profile, whose inherited
// sources say what each setting falls back to without this launch's choice.
export function shadowedChoices(c: Choices, r: Resolution, scope: 'repo' | 'org'): Shadowed {
  const out: Shadowed = { byPrompt: [], byRepo: [] }

  const check = (chosen: boolean, label: string, source: string) => {
    if (!chosen) {
      return
    }

    if (source === 'prompt') {
      out.byPrompt.push(label)
    } else if (scope === 'org' && source === 'repo') {
      out.byRepo.push(label)
    }
  }

  check(c.profile !== '', 'profile', r.profile.inheritedSource)
  check(c.promptId !== 0, 'prompt', r.prompt.inheritedSource)
  check(c.model !== null, 'model', r.model.inheritedSource)
  check(c.effort !== null, 'effort', r.effort.inheritedSource)
  check(c.planMode !== null, 'plan mode', r.planMode.inheritedSource)
  check(c.worktree !== null, 'worktree setting', r.worktree.inheritedSource)

  return out
}

// joinList joins names for a sentence: "a", "a and b", or "a, b, and c".
export function joinList(names: readonly string[]): string {
  if (names.length <= 2) {
    return names.join(' and ')
  }

  return `${names.slice(0, -1).join(', ')}, and ${names.at(-1) ?? ''}`
}

// SaveStatus is the launch menu's report on its last save.
export interface SaveStatus {
  tone: 'success' | 'warning' | 'error'
  text: string
}

// saveStatus reports a saved default, explaining any choice it would not change here.
export function saveStatus(target: string, promptName: string, repo: string, s: Shadowed): SaveStatus {
  const notes: string[] = []

  if (s.byPrompt.length > 0) {
    const those = s.byPrompt.length === 1 ? 'it applies' : 'they apply'

    notes.push(`“${promptName}” sets its own ${joinList(s.byPrompt)}, so ${those} only with other prompts.`)
  }

  if (s.byRepo.length > 0) {
    const wins = s.byRepo.length === 1 ? 'wins' : 'win'

    notes.push(`${repo} has its own ${joinList(s.byRepo)}, which still ${wins} there.`)
  }

  const saved = `Saved as the default for ${target}.`

  return notes.length === 0
    ? { tone: 'success', text: saved }
    : { tone: 'warning', text: `${saved} ${notes.join(' ')}` }
}
