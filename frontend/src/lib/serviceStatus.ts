import type { IncidentStage, ServiceProvider, ServiceState, ServiceStatus } from '../platform'

// The window's words and colors for the service status check, the one source every
// surface shows them from: the toolbar's warning, the account menu, Settings → Service
// status, and, in Go, the banners (internal/svcstatus/alerts.go words a state the same).

// Tone is how loudly the window shows a state: a warning for degraded performance or
// maintenance, danger for an outage.
export type Tone = 'warning' | 'danger'

// toneOf is how loudly to show state, or null for one the toolbar doesn't show.
export function toneOf(state: ServiceState): Tone | null {
  switch (state) {
    case 'minor':
    case 'maintenance':
      return 'warning'
    case 'major':
    case 'critical':
      return 'danger'
    default:
      return null
  }
}

// toneText and toneFill color a tone's text and its dot.
export const toneText: Readonly<Record<Tone, string>> = { warning: 'text-warning', danger: 'text-danger' }
export const toneFill: Readonly<Record<Tone, string>> = { warning: 'bg-warning', danger: 'bg-danger' }

// stateWords are each state's words, the same Go's banners use.
const stateWords: Readonly<Record<ServiceState, string>> = {
  off: 'Not checked',
  unknown: 'Not checked yet',
  operational: 'Operational',
  maintenance: 'Maintenance',
  minor: 'Degraded',
  major: 'Partial outage',
  critical: 'Major outage',
}

// stateLabel words a state: "Partial outage".
export function stateLabel(state: ServiceState): string {
  return stateWords[state]
}

// stageWords are where an incident stands, or '' for a stage the page didn't name.
const stageWords: Readonly<Record<IncidentStage, string>> = {
  investigating: 'Investigating',
  identified: 'Identified',
  monitoring: 'Monitoring a fix',
  in_progress: 'Under way',
  verifying: 'Verifying',
  other: '',
}

// stageLabel words where an incident stands: "Monitoring a fix", or '' for none.
export function stageLabel(stage: IncidentStage): string {
  return stageWords[stage]
}

// severity orders the states, worst last.
const severity: Readonly<Record<ServiceState, number>> = {
  off: 0,
  unknown: 0,
  operational: 1,
  maintenance: 2,
  minor: 3,
  major: 4,
  critical: 5,
}

// troubled lists the providers in trouble, which the toolbar shows, the worst first and
// otherwise in the catalog's order. With no reading, the check off, or everything watched
// operational, it lists none.
export function troubled(status: ServiceStatus | null): ServiceProvider[] {
  if (status === null) {
    return []
  }

  return status.providers
    .filter((p) => toneOf(p.state) !== null)
    .toSorted((a, b) => severity[b.state] - severity[a.state])
}

// watched lists the providers the check watches, in the catalog's order: on, with a part
// ticked, while the check is on.
export function watched(status: ServiceStatus | null): ServiceProvider[] {
  return status === null ? [] : status.providers.filter((p) => p.state !== 'off')
}

// worst is the worst state among providers, or 'operational' for none.
export function worst(providers: readonly ServiceProvider[]): ServiceState {
  return providers.reduce<ServiceState>((acc, p) => (severity[p.state] > severity[acc] ? p.state : acc), 'operational')
}

// statusLabel names the toolbar's warning: "Service status: Partial outage on Claude",
// each provider in trouble named, the worst first.
export function statusLabel(providers: readonly ServiceProvider[]): string {
  const said = providers.map((p) => `${stateWords[p.state]} on ${p.name}`).join(', ')

  return `Service status: ${said}`
}

// affectsWords names the parts a provider's trouble touches: "Affects: Claude API, Claude
// Code", or '' when only an incident that names no part counts.
export function affectsWords(provider: ServiceProvider): string {
  return provider.affected.length === 0 ? '' : `Affects: ${provider.affected.map((p) => p.name).join(', ')}`
}

// othersWords says how many incidents on a provider's page touch nothing the user
// watches: "1 other incident doesn’t touch what you watch", or '' for none.
export function othersWords(others: number): string {
  if (others <= 0) {
    return ''
  }

  return others === 1
    ? '1 other incident doesn’t touch what you watch'
    : `${String(others)} other incidents don’t touch what you watch`
}

// othersOnPage says, for Settings, how many incidents on a provider's page touch nothing
// the user watches: "1 other incident on the page now", or '' for none.
export function othersOnPage(others: number): string {
  if (others <= 0) {
    return ''
  }

  return others === 1 ? '1 other incident on the page now' : `${String(others)} other incidents on the page now`
}
