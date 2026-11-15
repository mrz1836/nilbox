import { vi } from 'vitest'

import { bannerOf, offerOf, type RestartStep } from '../lib/update'
import type { IntegrationsState } from '../lib/useIntegrations'
import type { MoveControl } from '../lib/useMove'
import type { Query } from '../lib/useQuery'
import type { UpdateControl } from '../lib/useUpdate'
import type { IntegrationTools, UpdateStatus } from '../platform'

// queryOf is a read as useQuery hands it to a component: data answered, or undefined while
// it loads, with refetch and set as mocks a test reads.
export function queryOf<T>(data: T | undefined, overrides: Partial<Query<T>> = {}): Query<T> {
  return {
    data,
    error: '',
    pending: false,
    waiting: false,
    refetch: vi.fn(() => Promise.resolve()),
    set: vi.fn(),
    ...overrides,
  }
}

// integrationsOf is the tools as useIntegrations hands them to a component: tools listed,
// or null while they load, with its moves as mocks a test reads.
export function integrationsOf(
  tools: IntegrationTools | null,
  overrides: Partial<IntegrationsState> = {},
): IntegrationsState {
  return {
    tools,
    error: '',
    checking: false,
    checkError: '',
    checkAgain: vi.fn(() => Promise.resolve()),
    reload: vi.fn(() => Promise.resolve()),
    replace: vi.fn(),
    ...overrides,
  }
}

// moveOf is the move into Applications as useMoveToApplications hands it to a component,
// for a status, or null while it loads: its dialog closed, with its moves as mocks a test
// reads. Whether it is offered is the update's offer's (offerOf).
export function moveOf(status: UpdateStatus | null, overrides: Partial<MoveControl> = {}): MoveControl {
  return {
    location: status?.location ?? null,
    dialog: null,
    move: vi.fn(),
    confirm: vi.fn(),
    dismiss: vi.fn(),
    reveal: vi.fn(),
    ...overrides,
  }
}

// updateOf is the app's update as useUpdateControl hands it to a component: a status, or
// null while it loads, the restart flow at a step, the banner they show, what they offer,
// and the move into Applications, with its moves as mocks a test reads.
export function updateOf(
  status: UpdateStatus | null,
  flow: RestartStep = { step: 'idle' },
  overrides: Partial<UpdateControl> = {},
): UpdateControl {
  return {
    status,
    flow,
    banner: bannerOf(status, flow, ''),
    offer: offerOf(status, flow),
    move: moveOf(status),
    restart: vi.fn(),
    later: vi.fn(),
    dismiss: vi.fn(),
    reveal: vi.fn(() => Promise.resolve(true)),
    reload: vi.fn(() => Promise.resolve()),
    ...overrides,
  }
}
