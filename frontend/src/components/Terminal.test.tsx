import { render, screen } from '@testing-library/react'
import type { CSSProperties, ReactElement, ReactNode } from 'react'
import { describe, expect, it } from 'vitest'

import { brand, palettes } from '../generated/brand'
import { darkTokens } from '../lib/theme'
import { domShape } from '../test/dom'
import { themeColors } from '../test/theme'
import { lineStyle, Terminal } from './Terminal'

// The terminals below are each one as its page drew it before it moved onto Terminal,
// copied as written but for the text size and the shadow, which now take their names from
// the type scale and the elevation tokens (the same values), to check that Terminal draws
// the same window for the props that page passes.

// lineStyleToday is the line stagger both pages define for themselves.
function lineStyleToday(at: number): CSSProperties {
  return { '--i': String(at) } as CSSProperties
}

// The inbox at nil's party.
function InboxNilToday({ children }: { children: ReactNode }) {
  const command = `${brand.slug} status`

  return (
    <figure
      aria-label="Terminal"
      className="nil-rise mt-9 w-full max-w-xl overflow-hidden rounded-lg border border-border bg-surface text-left font-mono text-body leading-6 text-text shadow-terminal"
      style={{ ...darkTokens, animationDelay: '320ms' }}
    >
      <div className="flex items-center gap-1.5 border-b border-border bg-raised px-3 py-2">
        <span aria-hidden="true" className="size-2.5 rounded-full bg-danger" />
        <span aria-hidden="true" className="size-2.5 rounded-full bg-warning" />
        <span aria-hidden="true" className="size-2.5 rounded-full bg-success" />
        <span className="flex-1 pr-10 text-center text-xs text-muted">~/inbox</span>
      </div>
      <div className="px-4 py-3">
        <p>
          <span className="text-success">$</span>{' '}
          <span className="nil-type" style={{ '--n': String(command.length) } as CSSProperties}>
            {command}
          </span>
        </p>
        {children}
      </div>
    </figure>
  )
}

// The setup's Finish log.
function FinishStepToday({ children }: { children: ReactNode }) {
  const command = `${brand.slug} setup`

  return (
    <figure
      aria-label="Setup log"
      className="nil-rise mt-8 w-full overflow-hidden rounded-lg border border-border bg-surface text-left font-mono text-body leading-6 text-text shadow-terminal"
      style={{ ...darkTokens, animationDelay: '200ms' }}
    >
      <div className="flex items-center gap-1.5 border-b border-border bg-raised px-3 py-2">
        <span aria-hidden="true" className="size-2.5 rounded-full bg-danger" />
        <span aria-hidden="true" className="size-2.5 rounded-full bg-warning" />
        <span aria-hidden="true" className="size-2.5 rounded-full bg-success" />
        <span className="flex-1 pr-10 text-center text-xs text-muted">~/{brand.slug}</span>
      </div>
      <div className="px-4 py-3">
        <p>
          <span className="text-success">$</span>{' '}
          <span className="nil-type" style={{ '--n': String(command.length) } as CSSProperties}>
            {command}
          </span>
        </p>
        {children}
      </div>
    </figure>
  )
}

// lines are a few lines as the pages print them, staggered by stagger.
function lines(stagger: (at: number) => CSSProperties) {
  return (
    <>
      <p className="nil-line pl-4 text-muted" style={stagger(0)}>
        {'// '}next one back tomorrow
      </p>
      <p className="nil-line mt-3" style={stagger(1)}>
        <span className="text-success">exit status 0</span> <span aria-hidden="true" className="nil-cursor" />
      </p>
    </>
  )
}

// shapeOf is the DOM a terminal draws.
function shapeOf(ui: ReactElement): string {
  const { container, unmount } = render(ui)
  const shape = domShape(container)

  unmount()

  return shape
}

describe('Terminal', () => {
  it.each<{ page: string; today: ReactElement; shared: ReactElement }>([
    {
      page: 'the inbox at nil',
      today: <InboxNilToday>{lines(lineStyleToday)}</InboxNilToday>,
      shared: (
        <Terminal
          label="Terminal"
          title="~/inbox"
          command={`${brand.slug} status`}
          className="mt-9 max-w-xl"
          delay={320}
        >
          {lines(lineStyle)}
        </Terminal>
      ),
    },
    {
      page: 'the setup’s Finish step',
      today: <FinishStepToday>{lines(lineStyleToday)}</FinishStepToday>,
      shared: (
        <Terminal
          label="Setup log"
          title={`~/${brand.slug}`}
          command={`${brand.slug} setup`}
          className="mt-8"
          delay={200}
        >
          {lines(lineStyle)}
        </Terminal>
      ),
    },
  ])('draws the terminal of $page as it is drawn today', ({ today, shared }) => {
    expect(shapeOf(shared)).toBe(shapeOf(today))
  })

  it('is a figure named by its label, typing its command, then its lines', () => {
    render(
      <Terminal label="Setup log" title="~/example-repo" command="example status" delay={200}>
        <p>ok</p>
      </Terminal>,
    )

    const figure = screen.getByRole('figure', { name: 'Setup log' })

    expect(figure).toHaveTextContent(/^~\/example-repo\$ example statusok$/)
    expect(screen.getByText('example status')).toHaveStyle({ '--n': '14' })
    expect(figure).toHaveStyle({ animationDelay: '200ms' })
  })

  it('places itself as told, and nowhere in particular by default', () => {
    const shape = 'nil-rise w-full overflow-hidden rounded-lg border border-border bg-surface text-left font-mono'
    const figure = (className?: string) => {
      const { unmount } = render(
        <Terminal label="Terminal" title="~" command="ls" delay={0} {...(className === undefined ? {} : { className })}>
          {null}
        </Terminal>,
      )
      const classes = screen.getByRole('figure').className

      unmount()

      return classes
    }

    expect(figure()).toMatch(new RegExp(`^${shape} `))
    expect(figure('mt-8')).toMatch(new RegExp(`^mt-8 ${shape} `))
  })

  it('sets every color the theme defines to the dark palette, and nothing else', () => {
    render(
      <Terminal label="Terminal" title="~/inbox" command="ls" delay={0}>
        {null}
      </Terminal>,
    )

    const style = screen.getByRole('figure').style
    const overrides = Array.from(style).filter((name) => name.startsWith('--'))

    expect(overrides).toEqual(themeColors())

    for (const [token, value] of Object.entries(palettes.dark)) {
      expect(style.getPropertyValue(`--color-${token}`)).toBe(value)
    }
  })
})

describe('lineStyle', () => {
  it('sets a line’s place for its staggered entrance', () => {
    expect(lineStyle(0)).toEqual({ '--i': '0' })
    expect(lineStyle(12)).toEqual({ '--i': '12' })
  })
})
