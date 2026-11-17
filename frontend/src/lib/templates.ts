// The template gallery's pure half: what quick setup asks for each of a template's blanks,
// the routine it makes from what the user filled in, and the words beside it. A routine
// made from a template is an ordinary routine, the user's to edit.

import type {
  AutopilotBlank,
  AutopilotDraft,
  AutopilotMode,
  AutopilotProblem,
  AutopilotRoutine,
  AutopilotSpec,
  AutopilotTemplate,
} from '../platform'
import { modeLabels } from './autopilot'

// blankValue is what a blank of a template holds as it ships: a search's where, or a
// condition's values.
export function blankValue(spec: AutopilotSpec, blank: AutopilotBlank): string[] {
  if (blank.kind === 'where') {
    return [...(spec.watch.sources[blank.source]?.search?.where ?? [])]
  }

  return [...(spec.conditions.of[blank.condition]?.values ?? [])]
}

// blankValues are a template's blanks as it ships, one list a blank, in its order.
export function blankValues(t: AutopilotTemplate): string[][] {
  return t.blanks.map((b) => blankValue(t.spec, b))
}

// filledSpec is a template's spec with each blank holding what the user filled in; a blank
// left out keeps what the template ships.
export function filledSpec(t: AutopilotTemplate, values: readonly (readonly string[])[]): AutopilotSpec {
  const spec = structuredClone(t.spec)

  t.blanks.forEach((b, i) => {
    const value = values[i]

    if (value === undefined) {
      return
    }

    if (b.kind === 'where') {
      const search = spec.watch.sources[b.source]?.search

      if (search != null) {
        search.where = [...value]
      }
    } else {
      const condition = spec.conditions.of[b.condition]

      if (condition !== undefined) {
        condition.values = [...value]
      }
    }
  })

  return spec
}

// freeName is name, or "name 2", "name 3", and so on, when a routine has it, ignoring case.
export function freeName(routines: readonly Pick<AutopilotRoutine, 'name'>[], name: string): string {
  const taken = new Set(routines.map((r) => r.name.toLowerCase()))

  for (let n = 1; ; n++) {
    const candidate = n === 1 ? name : `${name} ${String(n)}`

    if (!taken.has(candidate.toLowerCase())) {
      return candidate
    }
  }
}

// templateDraft is the new routine a template makes: named name, in mode (the one new
// routines start in, unless the user chose another), its blanks filled in.
export function templateDraft(
  t: AutopilotTemplate,
  values: readonly (readonly string[])[],
  name: string,
  mode: AutopilotMode,
): AutopilotDraft {
  return { id: 0, name, description: '', mode, templateKey: t.key, spec: filledSpec(t, values) }
}

// suggestedFirst splits the gallery into the templates that fit what this Mac already
// knows, in their order, and the rest.
export function suggestedFirst(templates: readonly AutopilotTemplate[]): {
  suggested: AutopilotTemplate[]
  rest: AutopilotTemplate[]
} {
  return {
    suggested: templates.filter((t) => t.suggested !== ''),
    rest: templates.filter((t) => t.suggested === ''),
  }
}

// modeHint says what a template suggests beside the mode its routine starts in, as a hint
// that never changes it: "This template suggests Autopilot." when they differ, else ''.
export function modeHint(t: AutopilotTemplate, mode: AutopilotMode): string {
  if (t.suggests === mode) {
    return ''
  }

  return `This template suggests ${modeLabels[t.suggests]}. It starts in ${modeLabels[mode]}, as new routines do: change it here if you like.`
}

// missingStarters are the starter prompts a template runs that the library no longer
// holds, which quick setup offers to add again.
export function missingStarters(t: AutopilotTemplate): AutopilotTemplate['prompts'] {
  return t.prompts.filter((p) => !p.inLibrary)
}

// quickProblems are the problems quick setup shows of a template's draft: every problem
// Describe found but what the template's needs already say (where its agent runs) and a
// starter it offers to add again.
export function quickProblems(t: AutopilotTemplate, problems: readonly AutopilotProblem[]): AutopilotProblem[] {
  const offered = missingStarters(t).length > 0
  const said = (p: AutopilotProblem) =>
    p.part === 'steps' &&
    ((t.needs.length > 0 && p.field.endsWith('.agent.where')) || (offered && p.field.endsWith('.prompt')))

  return problems.filter((p) => !said(p))
}

// blankProblems are the problems at one blank's part of the spec.
export function blankProblems(blank: AutopilotBlank, problems: readonly AutopilotProblem[]): AutopilotProblem[] {
  const at =
    blank.kind === 'where'
      ? { part: 'watch', field: `sources[${String(blank.source)}].search.where` }
      : { part: 'conditions', field: `of[${String(blank.condition)}]` }

  return problems.filter((p) => p.part === at.part && p.field.startsWith(at.field))
}
