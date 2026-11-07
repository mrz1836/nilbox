import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { brand } from '../generated/brand'
import type { DataReset, Info } from '../platform'
import { appInfo } from '../test/fixtures'
import { DataResetNotice } from './DataResetNotice'

const platform = vi.hoisted(() => ({
  getAppInfo: vi.fn(),
}))

vi.mock('../platform', () => platform)

// info is the app info the shell reports, with the data reset given, or none at all.
function info(dataReset?: DataReset | null): Info {
  return dataReset === undefined ? appInfo() : appInfo({ dataReset })
}

const reset: DataReset = { path: '~/data/app.db.broken-20260102T030405Z', reason: 'corrupt' }

const message = `${brand.name} couldn’t read its data, so it set it aside at ${reset.path} and started fresh.`

beforeEach(() => {
  vi.clearAllMocks()
})

describe('DataResetNotice', () => {
  it('says the data was set aside, where, and that the app started fresh', async () => {
    platform.getAppInfo.mockResolvedValue(info(reset))
    render(<DataResetNotice />)

    const status = screen.getByRole('status')
    expect(await screen.findByText(reset.path)).toBeInTheDocument()
    expect(status).toHaveTextContent(message)
  })

  it('goes away when dismissed', async () => {
    platform.getAppInfo.mockResolvedValue(info(reset))
    render(<DataResetNotice />)

    await userEvent.click(await screen.findByRole('button', { name: 'Dismiss notice' }))

    expect(screen.getByRole('status')).toBeEmptyDOMElement()
    expect(platform.getAppInfo).toHaveBeenCalledTimes(1)
  })

  it.each([
    ['left out', undefined],
    ['null', null],
  ])('shows nothing when the reset is %s', async (_, dataReset) => {
    platform.getAppInfo.mockResolvedValue(info(dataReset))
    render(<DataResetNotice />)

    await vi.waitFor(() => {
      expect(platform.getAppInfo).toHaveBeenCalled()
    })
    expect(screen.getByRole('status')).toBeEmptyDOMElement()
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })

  it('logs app info that could not be read, and shows nothing', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.getAppInfo.mockRejectedValue(new Error('backend gone'))
    render(<DataResetNotice />)

    await vi.waitFor(() => {
      expect(error).toHaveBeenCalledWith('could not read the app info', expect.any(Error))
    })
    expect(screen.getByRole('status')).toBeEmptyDOMElement()
  })

  it('ignores app info that arrives after it is gone', async () => {
    let resolve: (got: Info) => void = () => undefined
    platform.getAppInfo.mockReturnValue(
      new Promise<Info>((r) => {
        resolve = r
      }),
    )

    const { unmount } = render(<DataResetNotice />)
    unmount()
    resolve(info(reset))

    await Promise.resolve()
    expect(screen.queryByText(reset.path)).not.toBeInTheDocument()
  })
})
