// When the Scripts tab shows, what its badge says, and what the toast that announces it
// says, from the tools the app found and how the user switched them.

import type { IntegrationID, IntegrationsConfig, IntegrationTool, IntegrationTools, ScriptRun } from '../platform'
import { counted } from './format'
import { isLive } from './scriptRuns'

// toolOf is a tool the integrations list, by its integration, or undefined until listed.
export function toolOf(tools: IntegrationTools | null, id: IntegrationID): IntegrationTool | undefined {
  return tools?.tools.find((t) => t.id === id)
}

// usable reports a tool that is on and can be used: found, and new enough.
export function usable(tool: IntegrationTool | undefined): boolean {
  return tool !== undefined && tool.on && (tool.state === 'found' || tool.state === 'update')
}

// unusable reports a tool that is on but can't be used: missing, too old, or won't run.
export function unusable(tool: IntegrationTool): boolean {
  return tool.on && !usable(tool)
}

// showsScripts reports whether the Scripts tab shows (D2): while mage-x or go-pre-commit is
// on, or the user turned Scripts on. Scripts left automatic is on only while one of the
// tools is, so it adds nothing then. Nothing shows until both the tools and the settings
// have loaded.
export function showsScripts(tools: IntegrationTools | null, config: IntegrationsConfig | null): boolean {
  if (tools === null || config === null) {
    return false
  }

  return tools.tools.some((t) => t.on) || config.switches.scripts === 'on'
}

// ScriptsBadge is what the Scripts tab's badge says: how many runs are going, whether a
// run failed and hasn't been looked at, and whether a tool that's on can't be used.
export interface ScriptsBadge {
  running: number
  failed: boolean
  blocked: boolean
}

// scriptsBadge works out the tab's badge from the runs and the tools. An update that is
// available shows nothing here: the rail's foot and Settings say so.
export function scriptsBadge(runs: readonly ScriptRun[] | null, tools: IntegrationTools | null): ScriptsBadge {
  const listed = runs ?? []

  return {
    running: listed.filter((r) => isLive(r.status)).length,
    failed: listed.some((r) => r.status === 'failed' && !r.seen),
    blocked: tools?.tools.some(unusable) === true,
  }
}

// scriptsBadgeTitle explains the badge, as the tab's tooltip.
export function scriptsBadgeTitle(badge: ScriptsBadge): string {
  const parts: string[] = []

  if (badge.running > 0) {
    parts.push(counted(badge.running, 'script running', 'scripts running'))
  }

  if (badge.failed) {
    parts.push('a run failed')
  }

  if (badge.blocked) {
    parts.push('a tool that’s on can’t be used')
  }

  const text = parts.join(', ')

  return text.charAt(0).toUpperCase() + text.slice(1)
}

// foundToolsText announces the tab when it first shows because tools were found: the
// tools on and usable, by their commands' names, and where to find them. It is '' when
// none is, as when the tab shows only because Scripts was turned on.
export function foundToolsText(tools: IntegrationTools | null): string {
  const [first, ...rest] = (tools?.tools ?? []).filter((t) => usable(t))

  if (first === undefined) {
    return ''
  }

  if (rest.length > 0) {
    const names = [first, ...rest].map((t) => t.binary)

    return `Found ${names.slice(0, -1).join(', ')} and ${names.slice(-1).join('')}. They’re in the new Scripts tab.`
  }

  return first.id === 'mage-x'
    ? `Found ${first.binary}. Release is in the new Scripts tab.`
    : `Found ${first.binary}. It’s in the new Scripts tab.`
}
