import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it } from 'vitest'

import { TabPanel, Tabs } from './Tabs'

type Pane = 'one' | 'two' | 'three'

// Harness wires the tabs to their panel the way the views do.
function Harness() {
  const [pane, setPane] = useState<Pane>('one')

  return (
    <>
      <Tabs
        baseId="t"
        label="Panes"
        value={pane}
        onChange={setPane}
        tabs={[
          { value: 'one', label: 'One', count: '3', strong: true },
          { value: 'two', label: 'Two' },
          { value: 'three', label: 'Three', count: '' },
        ]}
      />
      <TabPanel baseId="t" value={pane}>
        Showing {pane}
      </TabPanel>
    </>
  )
}

describe('Tabs', () => {
  it('can hide a tab’s words where the row is narrow, keeping its name and a tooltip', () => {
    render(
      <Tabs
        baseId="t"
        label="Panes"
        value="one"
        onChange={() => undefined}
        tabs={[
          { value: 'one', label: 'One' },
          { value: 'two', label: 'Two', count: '4', labelClassName: '@max-[60rem]:sr-only', title: 'Two' },
        ]}
      />,
    )

    const two = screen.getByRole('tab', { name: 'Two 4' })
    expect(two).toHaveAttribute('title', 'Two')
    expect(screen.getByText('Two')).toHaveClass('@max-[60rem]:sr-only')
    expect(screen.getByRole('tab', { name: 'One' })).not.toHaveAttribute('title')
  })

  it('names each tab with its count and labels the panel with the selected tab', async () => {
    render(<Harness />)

    expect(screen.getByRole('tablist', { name: 'Panes' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: 'One 3' })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('tab', { name: 'Three' })).toHaveAttribute('tabIndex', '-1')
    expect(screen.getByRole('tabpanel')).toHaveAccessibleName('One 3')

    await userEvent.click(screen.getByRole('tab', { name: 'Two' }))

    expect(screen.getByRole('tabpanel')).toHaveAccessibleName('Two')
    expect(screen.getByRole('tabpanel')).toHaveTextContent('Showing two')
    expect(screen.getByRole('tab', { name: 'Two' })).toHaveAttribute('aria-controls', screen.getByRole('tabpanel').id)
  })

  it('moves and selects with the arrow keys, Home, and End, wrapping at the ends', async () => {
    render(<Harness />)
    const [one, two, three] = screen.getAllByRole('tab')

    await userEvent.click(screen.getByRole('tab', { name: 'One 3' }))
    await userEvent.keyboard('{ArrowLeft}')
    expect(three).toHaveFocus()
    expect(three).toHaveAttribute('aria-selected', 'true')

    await userEvent.keyboard('{ArrowRight}')
    expect(one).toHaveFocus()

    await userEvent.keyboard('{End}')
    expect(three).toHaveFocus()

    await userEvent.keyboard('{Home}')
    expect(one).toHaveFocus()

    await userEvent.keyboard('{ArrowRight}')
    expect(two).toHaveFocus()
    expect(screen.getByRole('tabpanel')).toHaveTextContent('Showing two')

    // Other keys are left alone.
    await userEvent.keyboard('x')
    expect(two).toHaveFocus()
  })
})
