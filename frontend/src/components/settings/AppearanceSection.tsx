import { useId, useState } from 'react'

import type { Result } from '../../lib/call'
import type { Theme } from '../../lib/theme'
import { useSettingsSave } from '../../lib/useSettingsSave'
import { presetFor, sizeKey, sizeLabel, windowPresets } from '../../lib/windowSize'
import { setWindowSize, type Settings } from '../../platform'
import { Segmented } from '../controls'
import { DisplayIcon, MoonIcon, SunIcon } from '../icons'
import { ChoiceRow, Group, joinIds, Note } from './rows'
import type { SectionProps } from './types'

// themeChoices are the theme options, each with its icon and the turn-in it plays when
// picked (see index.css).
const themeChoices: readonly {
  value: Theme
  label: string
  title: string
  Icon: typeof SunIcon
  turnIn: string
}[] = [
  {
    value: 'system',
    label: 'System',
    title: 'Light or dark as your Mac is, switching when it does',
    Icon: DisplayIcon,
    turnIn: 'motion-safe:animate-display-in',
  },
  { value: 'light', label: 'Light', title: 'Always light', Icon: SunIcon, turnIn: 'motion-safe:animate-sun-in' },
  { value: 'dark', label: 'Dark', title: 'Always dark', Icon: MoonIcon, turnIn: 'motion-safe:animate-moon-in' },
]

interface ThemeRowProps {
  // theme is the chosen theme, and onThemeChange switches to another and stores it,
  // resolving with why it could not be stored, if it could not.
  theme: Theme
  onThemeChange: (theme: Theme) => Promise<Result<void>>
}

// ThemeRow chooses the theme. A choice shows at once, with a short cross-fade, and is
// stored; the picked option's icon turns in.
function ThemeRow({ theme, onThemeChange }: ThemeRowProps) {
  const errorId = useId()
  const [error, setError] = useState('')
  // picked is the theme picked last on this page: only its icon turns in, so nothing
  // moves when the page opens.
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
    <ChoiceRow
      label="Theme"
      description="System matches your Mac, light or dark, and switches when it does."
      error={error}
      errorId={errorId}
    >
      <Segmented
        label="Theme"
        value={theme}
        options={themeChoices.map(({ value, label, title, Icon, turnIn }) => ({
          value,
          label,
          title,
          icon: <Icon className={`size-3.5 ${picked === value ? turnIn : ''}`} />,
        }))}
        describedBy={joinIds(error !== '' && errorId)}
        onChange={(next) => {
          void choose(next)
        }}
      />
    </ChoiceRow>
  )
}

// WindowSizeRow holds the size the window opens at. Choosing a size stores it and resizes
// the open window to it at once.
function WindowSizeRow({ settings: stored, onSaved }: SectionProps) {
  const errorId = useId()
  const { settings, save, error } = useSettingsSave(stored, onSaved)
  const defaultKey = sizeKey(settings.defaultWindowSize)

  const choose = (key: string) => {
    const preset = presetFor(key)
    if (preset === undefined) {
      return
    }

    void save({ windowSize: preset.size }, () => setWindowSize(preset.size))
  }

  return (
    <ChoiceRow
      label="Window size"
      description={`Opens at ${sizeLabel(settings.windowSize)}. Choosing a size resizes the window now, too.`}
      error={error}
      errorId={errorId}
    >
      <Segmented
        label="Window size"
        value={sizeKey(settings.windowSize)}
        options={windowPresets.map(({ label, size }) => ({
          value: sizeKey(size),
          label,
          title: sizeKey(size) === defaultKey ? `${sizeLabel(size)} (default)` : sizeLabel(size),
        }))}
        describedBy={joinIds(error !== '' && errorId)}
        onChange={choose}
      />
    </ChoiceRow>
  )
}

interface AppearanceSectionProps extends ThemeRowProps {
  // settings is null until it has loaded; the window size waits for it.
  settings: Settings | null
  onSaved: (settings: Settings) => void
}

// AppearanceSection holds how the app looks: the theme, which needs no settings to show,
// and the size the window opens at.
export function AppearanceSection({ settings, onSaved, theme, onThemeChange }: AppearanceSectionProps) {
  return (
    <div className="flex flex-col gap-3">
      <Group>
        <ThemeRow theme={theme} onThemeChange={onThemeChange} />
        {settings === null ? (
          <p className="px-4 py-3.5 text-sm text-muted">Loading the window size…</p>
        ) : (
          <WindowSizeRow settings={settings} onSaved={onSaved} />
        )}
      </Group>
      <Note>
        Sizes are in points. A size larger than your screen opens as large as the screen allows, and you can still
        resize the window by dragging its edges.
      </Note>
    </div>
  )
}
