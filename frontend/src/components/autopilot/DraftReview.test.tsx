import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { AutopilotDraft } from '../../platform'
import { routineSpec } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { DraftTestPanel, useDraftReview } from './DraftReview'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// Reviewed is a draft under review: what Describe read of it, and its Test.
function Reviewed({ draft }: { draft: AutopilotDraft }) {
  const review = useDraftReview(draft, 0)

  return (
    <>
      <p>{review.fresh ? (review.reading?.sentence ?? '') : 'Reading it…'}</p>
      <DraftTestPanel test={review.test} ready={review.fresh} kinds={['pr']} mode="ask" draft={draft} />
    </>
  )
}

describe('useDraftReview', () => {
  it('reads a draft with no ID as a new routine’s, and says why a test of it failed', async () => {
    const user = userEvent.setup()

    render(<Reviewed draft={{ name: 'Pin failing PRs', spec: routineSpec() }} />)

    expect(await screen.findByText(/When checks pass on pull requests in Review requested/)).toBeInTheDocument()

    const test = within(screen.getByRole('region', { name: 'Test' }))

    platform.testRoutine.mockRejectedValueOnce(new Error('GitHub didn’t answer the search.'))
    await user.click(test.getByRole('button', { name: /Test/ }))
    expect(await test.findByRole('alert')).toHaveTextContent('GitHub didn’t answer the search.')
    expect(test.queryByRole('list', { name: 'Items it watches' })).not.toBeInTheDocument()

    await user.click(test.getByRole('button', { name: /Test/ }))
    expect(await test.findByRole('list', { name: 'Items it watches' })).toBeInTheDocument()
    expect(platform.describeRoutine).toHaveBeenCalledWith({ name: 'Pin failing PRs', spec: routineSpec() })
  })
})
