import {
  Fragment,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type ReactNode,
} from 'react'

import { usePopover } from '../lib/usePopover'

export interface MenuItem {
  label: string
  icon?: ReactNode
  // hint is quieter text at the item's end, such as when a choice takes effect.
  hint?: ReactNode
  onSelect: () => void
  disabled?: boolean
  // title explains the item, such as why it is disabled.
  title?: string
  danger?: boolean
  // divided draws a line above the item, setting it apart from those before it.
  divided?: boolean
}

export interface MenuButtonProps {
  // label names the button for assistive tech; children draw it.
  label: string
  title?: string
  items: readonly MenuItem[]
  children: ReactNode
  className?: string
  // align puts the menu's right or left edge under the button's.
  align?: 'left' | 'right'
  // heading is a caption above the items.
  heading?: string
}

const itemClass =
  'flex w-full items-center gap-2.5 rounded-md px-2.5 py-1.5 text-left text-sm whitespace-nowrap hover:bg-raised focus:bg-raised focus-visible:outline-none disabled:cursor-default disabled:opacity-50 disabled:hover:bg-transparent'

// placements put the menu below or above the button, lined up with its right or left
// edge, growing out of that corner.
const placements = {
  below: { right: 'top-full mt-1.5 right-0 origin-top-right', left: 'top-full mt-1.5 left-0 origin-top-left' },
  above: {
    right: 'bottom-full mb-1.5 right-0 origin-bottom-right',
    left: 'bottom-full mb-1.5 left-0 origin-bottom-left',
  },
} as const

// menuItems lists a menu's enabled items in order.
function menuItems(menu: HTMLElement | null): HTMLElement[] {
  return Array.from(menu?.querySelectorAll<HTMLElement>('[role="menuitem"]:not(:disabled)') ?? [])
}

// nextItem is the item a navigation key moves to, wrapping at both ends, or undefined
// for any other key.
function nextItem(items: HTMLElement[], key: string): HTMLElement | undefined {
  const at = items.findIndex((item) => item === document.activeElement)

  switch (key) {
    case 'ArrowDown':
      return items[(at + 1) % items.length]
    case 'ArrowUp':
      return items[(at - 1 + items.length) % items.length]
    case 'Home':
      return items[0]
    case 'End':
      return items.at(-1)
    default:
      return undefined
  }
}

// MenuButton is a button that opens a menu of actions. It behaves like a native menu:
// arrows, Home, and End move between items, Escape closes it and returns focus to the
// button, and a click outside closes it. It opens upward when there is not enough room
// below the button.
export function MenuButton({
  label,
  title,
  items,
  children,
  className = '',
  align = 'right',
  heading,
}: MenuButtonProps) {
  const id = useId()
  const { open, toggle, close, triggerRef, panelRef } = usePopover<HTMLButtonElement, HTMLDivElement>()
  const menuRef = useRef<HTMLDivElement>(null)
  const [above, setAbove] = useState(false)

  useLayoutEffect(() => {
    const trigger = triggerRef.current
    const panel = panelRef.current

    if (!open || trigger === null || panel === null) {
      return
    }

    const box = trigger.getBoundingClientRect()
    const spaceBelow = window.innerHeight - box.bottom

    setAbove(panel.offsetHeight + 12 > spaceBelow && box.top > spaceBelow)
  }, [open, triggerRef, panelRef])

  useEffect(() => {
    if (open) {
      menuItems(menuRef.current)[0]?.focus()
    }
  }, [open])

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const target = nextItem(menuItems(menuRef.current), e.key)

    if (target !== undefined) {
      e.preventDefault()
      target.focus()
    }
  }

  return (
    <div className="relative">
      <button
        ref={triggerRef}
        type="button"
        aria-label={label}
        title={title ?? label}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        onClick={toggle}
        className={className}
      >
        {children}
      </button>
      {open && (
        <div
          ref={panelRef}
          className={`absolute z-40 min-w-52 rounded-lg border border-border bg-surface p-1.5 shadow-xl shadow-black/20 motion-safe:animate-pop-in ${placements[above ? 'above' : 'below'][align]}`}
        >
          {heading !== undefined && (
            <p aria-hidden="true" className="px-2.5 pt-1 pb-1.5 text-xs font-medium text-muted">
              {heading}
            </p>
          )}
          <div ref={menuRef} id={id} role="menu" aria-label={label} tabIndex={-1} onKeyDown={onKeyDown}>
            {items.map((item) => (
              <Fragment key={item.label}>
                {item.divided === true && <hr className="mx-1 my-1.5 border-border" />}
                <button
                  type="button"
                  role="menuitem"
                  tabIndex={-1}
                  disabled={item.disabled}
                  title={item.title}
                  onClick={() => {
                    close(true)
                    item.onSelect()
                  }}
                  className={`${itemClass} ${item.danger === true ? 'text-danger' : 'text-text'}`}
                >
                  {item.icon}
                  {item.label}
                  {item.hint !== undefined && (
                    <>
                      <span className="sr-only">, </span>
                      <span className="ml-auto flex items-center gap-1.5 pl-6 text-xs text-muted">{item.hint}</span>
                    </>
                  )}
                </button>
              </Fragment>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
