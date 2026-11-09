import type { HTMLAttributes, KeyboardEvent, ReactNode, Ref } from 'react'

export interface TabItem<T extends string> {
  value: T
  label: string
  // count is how much the tab holds, shown in a badge beside the label and read with it;
  // empty or missing shows none.
  count?: string
  // icon sits before the label; it is decorative, since the label names the tab.
  icon?: ReactNode
  // strong sets the label in the weight of a heading, for the tab that names the whole set.
  strong?: boolean
}

export interface TabsProps<T extends string> {
  // baseId ties the tabs to their TabPanel, which takes the same baseId.
  baseId: string
  // label names the set of tabs for assistive tech.
  label: string
  tabs: readonly TabItem<T>[]
  value: T
  onChange: (value: T) => void
  // className lays out the row of tabs, and tabClassName adds to each tab, such as padding.
  className?: string
  tabClassName?: string
}

// tabId is the id of one tab.
function tabId(baseId: string, value: string): string {
  return `${baseId}-tab-${value}`
}

// tabPanelId is the id of the panel a row of tabs controls, for another control of the
// same panel to point aria-controls at.
export function tabPanelId(baseId: string): string {
  return `${baseId}-panel`
}

// moveTo is the index a navigation key moves to among n tabs from at, wrapping at both
// ends, or undefined for any other key.
function moveTo(key: string, at: number, n: number): number | undefined {
  switch (key) {
    case 'ArrowRight':
      return (at + 1) % n
    case 'ArrowLeft':
      return (at - 1 + n) % n
    case 'Home':
      return 0
    case 'End':
      return n - 1
    default:
      return undefined
  }
}

// Tabs is a row of tabs over one TabPanel, following the ARIA tabs pattern: only the
// selected tab is in the Tab order, and the arrow keys, Home, and End move between the
// tabs, selecting the one they reach.
export function Tabs<T extends string>({
  baseId,
  label,
  tabs,
  value,
  onChange,
  className = '',
  tabClassName = '',
}: TabsProps<T>) {
  const onKeyDown = (e: KeyboardEvent<HTMLButtonElement>) => {
    const at = tabs.findIndex((tab) => tab.value === value)
    const next = tabs[moveTo(e.key, at, tabs.length) ?? -1]

    if (next === undefined) {
      return
    }

    e.preventDefault()
    onChange(next.value)
    document.getElementById(tabId(baseId, next.value))?.focus()
  }

  return (
    <div role="tablist" aria-label={label} className={className}>
      {tabs.map((tab) => {
        const selected = tab.value === value
        const count = tab.count ?? ''

        return (
          <button
            key={tab.value}
            type="button"
            role="tab"
            id={tabId(baseId, tab.value)}
            aria-label={count === '' ? tab.label : `${tab.label} ${count}`}
            aria-selected={selected}
            aria-controls={tabPanelId(baseId)}
            tabIndex={selected ? 0 : -1}
            onClick={() => {
              onChange(tab.value)
            }}
            onKeyDown={onKeyDown}
            className={`-mb-px flex items-center gap-1.5 border-b-2 border-transparent px-2.5 pb-2 text-sm text-muted transition-colors hover:text-text aria-selected:border-accent aria-selected:text-text ${tabClassName}`}
          >
            {tab.icon}
            <span className={tab.strong === true ? 'font-semibold' : 'font-medium'}>{tab.label}</span>
            {count !== '' && (
              <span
                className={`rounded-full px-1.5 text-[0.6875rem] leading-[1.125rem] font-medium tabular-nums ${selected ? 'bg-accent/15 text-text' : 'bg-raised text-muted'}`}
              >
                {count}
              </span>
            )}
          </button>
        )
      })}
    </div>
  )
}

export interface TabPanelProps extends HTMLAttributes<HTMLDivElement> {
  // baseId is the Tabs' baseId, and value the selected tab, which names the panel.
  baseId: string
  value: string
  ref?: Ref<HTMLDivElement>
}

// TabPanel is the panel a row of Tabs controls, showing the selected tab's content.
export function TabPanel({ baseId, value, ref, children, ...rest }: TabPanelProps) {
  return (
    <div ref={ref} role="tabpanel" id={tabPanelId(baseId)} aria-labelledby={tabId(baseId, value)} {...rest}>
      {children}
    </div>
  )
}
