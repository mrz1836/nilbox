import { useEffect, useId, useRef, type KeyboardEvent, type ReactNode } from 'react'

import { usePopover } from '../lib/usePopover'

export interface MenuItem {
  label: string
  icon?: ReactNode
  onSelect: () => void
  disabled?: boolean
  // title explains the item, such as why it is disabled.
  title?: string
  danger?: boolean
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
}

const itemClass =
  'flex w-full items-center gap-2.5 rounded-md px-2.5 py-1.5 text-left text-sm hover:bg-raised focus:bg-raised focus-visible:outline-none disabled:cursor-default disabled:opacity-50 disabled:hover:bg-transparent'

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
// button, and a click outside closes it.
export function MenuButton({ label, title, items, children, className = '', align = 'right' }: MenuButtonProps) {
  const id = useId()
  const { open, toggle, close, triggerRef, panelRef } = usePopover<HTMLButtonElement, HTMLDivElement>()
  const menuRef = useRef<HTMLDivElement>(null)

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
          className={`absolute top-full z-40 mt-1.5 min-w-52 rounded-lg border border-border bg-surface p-1.5 shadow-xl shadow-black/20 motion-safe:animate-pop-in ${align === 'right' ? 'right-0 origin-top-right' : 'left-0 origin-top-left'}`}
        >
          <div ref={menuRef} id={id} role="menu" aria-label={label} tabIndex={-1} onKeyDown={onKeyDown}>
            {items.map((item) => (
              <button
                key={item.label}
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
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
