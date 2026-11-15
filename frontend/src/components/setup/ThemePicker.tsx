import { useId, useState } from 'react'

import type { Result } from '../../lib/call'
import type { Theme } from '../../lib/theme'
import { DisplayIcon, MoonIcon, SunIcon } from '../icons'

// tiles are the themes the picker offers, each with its icon and the turn-in it plays
// when picked (see index.css), as Settings → Appearance offers them.
const tiles: readonly { value: Theme; label: string; hint: string; Icon: typeof SunIcon; turnIn: string }[] = [
  {
    value: 'system',
    label: 'System',
    hint: 'Matches your Mac',
    Icon: DisplayIcon,
    turnIn: 'motion-safe:animate-display-in',
  },
  { value: 'light', label: 'Light', hint: 'Always light', Icon: SunIcon, turnIn: 'motion-safe:animate-sun-in' },
  { value: 'dark', label: 'Dark', hint: 'Always dark', Icon: MoonIcon, turnIn: 'motion-safe:animate-moon-in' },
]

export interface ThemePickerProps {
  theme: Theme
  onThemeChange: (theme: Theme) => Promise<Result<void>>
}

// ThemePicker chooses the theme from three tiles, a first choice that risks nothing: the
// window cross-fades to it at once, and it is stored. Radio buttons underneath, so arrow
// keys move between the tiles; a theme that can't be stored says why.
export function ThemePicker({ theme, onThemeChange }: ThemePickerProps) {
  const name = useId()
  const [error, setError] = useState('')
  // picked is the theme picked last: only its icon turns in.
  const [picked, setPicked] = useState<Theme | null>(null)

  const choose = async (next: Theme) => {
    setError('')
    setPicked(next)

    const result = await onThemeChange(next)

    if (!result.ok) {
      setError(result.error)
    }
  }

  return (
    <div className="flex flex-col gap-2">
      <div role="radiogroup" aria-label="Theme" className="grid grid-cols-3 gap-3">
        {tiles.map(({ value, label, hint, Icon, turnIn }) => (
          <label
            key={value}
            className="group flex cursor-pointer flex-col items-center gap-1.5 rounded-lg border border-border bg-surface px-3 py-3.5 text-center transition-colors hover:border-accent/50 has-checked:border-accent has-checked:bg-accent/10 has-focus-visible:outline-2 has-focus-visible:outline-accent"
          >
            <input
              type="radio"
              name={name}
              value={value}
              checked={theme === value}
              onChange={() => {
                void choose(value)
              }}
              className="sr-only"
            />
            <Icon className={`size-5 text-muted group-has-checked:text-accent ${picked === value ? turnIn : ''}`} />
            <span className="text-sm font-medium">{label}</span>
            <span className="text-xs text-muted">{hint}</span>
          </label>
        ))}
      </div>
      {error !== '' && (
        <p role="alert" className="text-xs text-danger">
          Couldn’t store the theme: {error}
        </p>
      )}
    </div>
  )
}
