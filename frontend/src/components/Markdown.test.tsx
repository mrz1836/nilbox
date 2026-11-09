import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { platform } from '../test/platform'
import { Markdown } from './Markdown'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

describe('Markdown', () => {
  it('renders the markdown in a container sized in rem', () => {
    const { container } = render(<Markdown text={'# Title\n\nSome **bold** text.'} />)

    const box = container.firstElementChild
    expect(box).toHaveClass('markdown', 'space-y-2', 'text-[0.8125rem]', 'text-text')
    expect(screen.getByRole('heading', { level: 1, name: 'Title' })).toBeInTheDocument()
    expect(box?.querySelector('strong')).toHaveTextContent('bold')
  })

  it('adds the classes it is given', () => {
    const { container } = render(<Markdown text="text" className="mt-4 text-muted" />)

    expect(container.firstElementChild).toHaveClass('markdown', 'mt-4', 'text-muted')
  })

  it('opens a link in the browser', async () => {
    render(<Markdown text="See [the pull request](https://github.com/acme-org/example-repo/pull/7)." />)

    await userEvent.click(screen.getByRole('button', { name: 'the pull request' }))

    expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith('https://github.com/acme-org/example-repo/pull/7')
  })

  it('loads no image, and opens one only when asked', async () => {
    const { container } = render(
      <Markdown
        text={'![Screenshot](https://example.com/shot.png)\n\n<img src="https://example.com/raw.png" alt="Raw">'}
      />,
    )

    expect(container.querySelector('img')).toBeNull()
    expect(platform.openExternal).not.toHaveBeenCalled()

    await userEvent.click(screen.getByRole('button', { name: 'Raw' }))

    expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith('https://example.com/raw.png')
  })

  it('shows a javascript: link as text that opens nothing', async () => {
    render(<Markdown text="[click me](javascript:alert(1))" />)

    expect(screen.queryByRole('button')).toBeNull()

    await userEvent.click(screen.getByText('click me'))

    expect(platform.openExternal).not.toHaveBeenCalled()
  })
})
