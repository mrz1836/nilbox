import { describe, expect, it, vi } from 'vitest'

import type { AutopilotTarget } from '../platform'
import { autopilotTarget } from '../test/fixtures'
import { platform } from '../test/platform'
import { watchSessionWith } from './watchSession'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

describe('watchSessionWith', () => {
  it('opens the run view of the run a session is, or says why it can’t', async () => {
    const open = vi.fn<(target: AutopilotTarget) => void>()
    const say = vi.fn<(text: string) => void>()
    const watch = watchSessionWith(open, say)

    platform.autopilotRunBySession
      .mockResolvedValueOnce(81)
      .mockResolvedValueOnce(0)
      .mockRejectedValueOnce(new Error('Autopilot’s runs couldn’t be read.'))

    watch('run')
    await vi.waitFor(() => {
      expect(open).toHaveBeenCalledExactlyOnceWith(autopilotTarget({ runId: 81 }))
    })
    expect(platform.autopilotRunBySession).toHaveBeenCalledWith('run')

    watch('gone')
    await vi.waitFor(() => {
      expect(say).toHaveBeenCalledWith('That run is no longer in Autopilot’s history.')
    })

    watch('broken')
    await vi.waitFor(() => {
      expect(say).toHaveBeenLastCalledWith('Autopilot’s runs couldn’t be read.')
    })
    expect(open).toHaveBeenCalledOnce()
  })
})
