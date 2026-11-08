// What the window says about the GitHub projects an issue is in, and how the user lets the
// app read them: the GitHub CLI's token needs the read:project scope, which gh does not
// grant by default.

import type { InboxSnapshot, Project, ProjectField, Settings, SubIssues } from '../platform'

// grantProjectsCommand gives the GitHub CLI's token read-only access to projects. gh adds
// the scope to the ones the token has, and the app's next refresh picks the token up.
export const grantProjectsCommand = 'gh auth refresh -s read:project'

// projectText says where an issue stands in a project: "Roadmap: In progress", or the
// project's title alone when the issue has no status there.
export function projectText(project: Project): string {
  return project.status === '' ? project.title : `${project.title}: ${project.status}`
}

// featuredRules pick the fields a card shows beside a project's status, by name, in the
// order they show: how urgent the issue is, then how big. Every other field is in the
// project's tooltip.
const featuredRules: readonly RegExp[] = [/\b(priority|prio)\b/i, /\b(size|effort)\b/i]

// featuredFields returns the fields a card shows beside the project's status: the first
// field named like each of featuredRules.
export function featuredFields(project: Project): ProjectField[] {
  return featuredRules.flatMap((rule) => project.fields.find((field) => rule.test(field.name)) ?? [])
}

// projectTooltip says all a card knows about an issue's place in a project: where it
// stands, each field's value, and that a click opens it.
export function projectTooltip(project: Project): string {
  return [
    projectText(project),
    ...project.fields.map((f) => `${f.name}: ${f.value}`),
    'Open the project on GitHub',
  ].join('\n')
}

// subIssuesText says how far along an issue's sub-issues are: "6 of 8 sub-issues done (75%)".
export function subIssuesText({ total, completed }: SubIssues): string {
  const noun = total === 1 ? 'sub-issue' : 'sub-issues'

  return `${String(completed)} of ${String(total)} ${noun} done (${String(Math.round((completed / total) * 100))}%)`
}

// hasIssues reports whether any inbox list holds an issue.
export function hasIssues(snapshot: InboxSnapshot): boolean {
  return [snapshot.reviewRequested, snapshot.authored, snapshot.assigned].some((list) =>
    list.items.some((item) => item.kind === 'issue'),
  )
}

// offersProjects reports whether the inbox offers to show issue projects: the last
// refresh found the token can't read them, the inbox holds an issue they would show on,
// and the user hasn't put the offer away. It waits for both the inbox and the settings.
export function offersProjects(snapshot: InboxSnapshot | null, settings: Settings | null): boolean {
  return (
    snapshot !== null &&
    settings !== null &&
    snapshot.projectAccess === 'needs_scope' &&
    !settings.projectsTipDismissed &&
    hasIssues(snapshot)
  )
}
