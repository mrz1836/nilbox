import { vi } from 'vitest'

import type { IntegrationsState } from '../lib/useIntegrations'
import type { Query } from '../lib/useQuery'
import type { IntegrationTools } from '../platform'

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
