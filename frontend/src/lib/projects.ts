// What the window says about the GitHub projects an issue is in, and how the user lets the
// app read them: the GitHub CLI's token needs the read:project scope, which gh does not
// grant by default.

import type { InboxSnapshot, Project, Settings } from '../platform'

// grantProjectsCommand gives the GitHub CLI's token read-only access to projects. gh adds
// the scope to the ones the token has, and the app's next refresh picks the token up.
export const grantProjectsCommand = 'gh auth refresh -s read:project'

// projectText says where an issue stands in a project: "Roadmap: In progress", or the
// project's title alone when the issue has no status there.
export function projectText(project: Project): string {
  return project.status === '' ? project.title : `${project.title}: ${project.status}`
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
