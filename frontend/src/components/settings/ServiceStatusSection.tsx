import { useId, useState } from 'react'

import { brand } from '../../generated/brand'
import { othersOnPage, stateLabel, toneOf } from '../../lib/serviceStatus'
import { formatAgo } from '../../lib/time'
import { useNowAs } from '../../lib/useNow'
import { useServiceStatus } from '../../lib/useServiceStatus'
import { useSettingsSave } from '../../lib/useSettingsSave'
import {
  openExternal,
  setRefreshInterval,
  setStatusPart,
  setStatusProvider,
  type ServiceProvider,
  type ServiceState,
  type StatusConfig,
  type StatusPart,
  type StatusPartID,
  type StatusProvider,
  type StatusProviderID,
} from '../../platform'
import { ErrorText, Switch } from '../controls'
import { ExternalIcon } from '../icons'
import { cardMatchClass, headingMatchClass, useSetting } from './match'
import { everyWords, refreshDefaults } from './RefreshingSection'
import { Group, joinIds, Note, rowDescribedBy, SectionIntro, SettingRow } from './rows'
import type { SectionProps } from './types'

// dotFill colors a provider's live state: green while what it watches works, amber or red
// while it has a problem, and quiet while it isn't checked.
function dotFill(state: ServiceState): string {
  switch (toneOf(state)) {
    case 'warning':
      return 'bg-warning'
    case 'danger':
      return 'bg-danger'
    default:
      return state === 'operational' ? 'bg-success' : 'bg-border'
  }
}

// groupsOf splits a provider's parts by the group its page lists them under, in the
// catalog's order; a provider whose page has no groups is one group named ''.
function groupsOf(parts: readonly StatusPart[]): { group: string; parts: StatusPart[] }[] {
  const out: { group: string; parts: StatusPart[] }[] = []

  for (const part of parts) {
    const last = out.find((g) => g.group === part.group)

    if (last === undefined) {
      out.push({ group: part.group, parts: [part] })
    } else {
      last.parts.push(part)
    }
  }

  return out
}

// stateLine says where a provider stands now: its state, when its page was last read, and
// why it isn't checked, when it isn't: the check is off, the provider is, or nothing of it
// is ticked.
function stateLine(
  live: ServiceProvider | undefined,
  on: boolean,
  ticked: boolean,
  checkOff: boolean,
  checked: string,
): string {
  if (live === undefined || live.state === 'off') {
    if (checkOff) {
      return 'Not checked: the check is off'
    }

    if (!on) {
      return 'Off: its page isn’t read'
    }

    return ticked ? 'Not checked yet' : 'Not checked: tick a part to watch'
  }

  if (live.state === 'unknown') {
    return 'Not checked yet'
  }

  return checked === '' ? stateLabel(live.state) : `${stateLabel(live.state)} · checked ${checked}`
}

interface ProviderCardProps {
  provider: StatusProvider
  on: boolean
  picked: readonly StatusPartID[]
  // live is where the provider stands now, or undefined until the status loads.
  live: ServiceProvider | undefined
  // checkOff greys the card: the check is off, so nothing is read.
  checkOff: boolean
  // error is why the last change to this card was refused, and errorOn the control it was
  // made with.
  error: string
  errorOn: 'switch' | 'parts' | ''
  onProvider: (on: boolean) => void
  onPart: (id: StatusPartID, on: boolean) => void
}

// ProviderCard is one status page: its name, a dot of where it stands now, its switch,
// its parts to tick, grouped as its page groups them, how many incidents on the page touch
// nothing ticked, the parts ticked that the page no longer lists, and the page itself.
function ProviderCard({ provider, on, picked, live, checkOff, error, errorOn, onProvider, onPart }: ProviderCardProps) {
  const id = useId()
  const [marks, match] = useSetting(`${provider.name} status page`)
  const checkedAt = live?.checkedAt ?? ''
  const checked = useNowAs((now) => (checkedAt === '' ? '' : formatAgo(now, checkedAt)))
  const state = live?.state ?? 'off'
  const others = live === undefined || live.state === 'off' ? '' : othersOnPage(live.others)
  const unlisted = (live?.unlisted ?? []).map((pid) => provider.parts.find((p) => p.id === pid)?.name ?? pid)
  const switchError = errorOn === 'switch' ? error : ''
  const partsError = errorOn === 'parts' ? error : ''

  return (
    <section
      {...marks}
      aria-labelledby={`${id}-name`}
      className={`rounded-lg border border-border bg-surface transition-opacity ${checkOff ? 'opacity-60' : ''} ${cardMatchClass}`}
    >
      <div className="flex items-start justify-between gap-6 px-4 pt-3.5">
        <div className="min-w-0">
          <h3 id={`${id}-name`} className={`flex items-center gap-2 text-sm font-semibold ${headingMatchClass(match)}`}>
            <span aria-hidden="true" className={`size-2 shrink-0 rounded-full ${dotFill(state)}`} />
            <label htmlFor={`${id}-switch`}>{provider.name}</label>
          </h3>
          <p id={`${id}-state`} className="mt-0.5 text-xs text-muted">
            {stateLine(live, on, picked.length > 0, checkOff, checked)}
          </p>
        </div>
        <Switch
          id={`${id}-switch`}
          checked={on}
          disabled={checkOff}
          describedBy={joinIds(`${id}-state`, switchError !== '' && `${id}-switch-error`)}
          onChange={onProvider}
        />
      </div>
      <div className="flex flex-col gap-2.5 px-4 pt-2.5 pb-3">
        <ErrorText id={`${id}-switch-error`} error={switchError} />
        {groupsOf(provider.parts).map(({ group, parts }) => (
          <div
            key={group}
            role="group"
            aria-label={group === '' ? `${provider.name} parts` : `${provider.name} ${group}`}
            aria-describedby={joinIds(partsError !== '' && `${id}-parts-error`)}
            className="flex flex-col gap-1.5"
          >
            {group !== '' && (
              <p aria-hidden="true" className="text-caption font-medium tracking-wide text-muted uppercase">
                {group}
              </p>
            )}
            <div className="flex flex-wrap gap-x-6 gap-y-1.5">
              {parts.map((part) => (
                <label key={part.id} className="flex cursor-pointer items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    checked={picked.includes(part.id)}
                    disabled={checkOff || !on}
                    onChange={(e) => {
                      onPart(part.id, e.target.checked)
                    }}
                    className="checkbox"
                  />
                  {part.name}
                </label>
              ))}
            </div>
          </div>
        ))}
        <ErrorText id={`${id}-parts-error`} error={partsError} />
        {live !== undefined && live.error !== '' && <p className="text-xs text-warning">{live.error}</p>}
        {unlisted.length > 0 && (
          <p className="text-xs text-muted">
            The page no longer lists {unlisted.join(', ')}, so {unlisted.length === 1 ? 'it isn’t' : 'they aren’t'}{' '}
            watched until {brand.name} is updated.
          </p>
        )}
        <p className="flex flex-wrap items-center gap-x-1.5 text-xs text-muted">
          {others !== '' && (
            <>
              <span>{others}</span>
              <span aria-hidden="true">·</span>
            </>
          )}
          <button
            type="button"
            onClick={() => {
              openExternal(provider.pageUrl)
            }}
            className="inline-flex items-center gap-1 hover:text-text hover:underline"
          >
            Status page
            <ExternalIcon className="size-3" />
          </button>
        </p>
      </div>
    </section>
  )
}

// watchOf is the choice for provider id, or one that watches nothing.
function watchOf(config: StatusConfig, id: StatusProviderID): { on: boolean; parts: StatusPartID[] } {
  return config.watch.find((w) => w.provider === id) ?? { on: false, parts: [] }
}

// ServiceStatusSection holds the service status check: how often the status pages are
// checked (off, or five minutes to an hour), and one card per status page Go's catalog
// lists, each with its switch and its parts to tick, a dot of where it stands now, and
// what the page says beside what is ticked. Each change is saved as soon as it is made;
// ticking a part needs no request, since the last readings are judged again.
export function ServiceStatusSection({ settings: stored, onSaved }: SectionProps) {
  const id = useId()
  const { settings, save, error } = useSettingsSave(stored, onSaved)
  // changed is what was changed last, beside which its refusal shows.
  const [changed, setChanged] = useState<{ on: StatusProviderID | 'every'; control: 'switch' | 'parts' | '' }>({
    on: 'every',
    control: '',
  })
  const service = useServiceStatus(stored.refresh.serviceStatus, stored.serviceStatus)
  const every = settings.refresh.serviceStatus
  const checkOff = every <= 0
  const everyError = changed.on === 'every' ? error : ''

  const chooseEvery = (seconds: number) => {
    setChanged({ on: 'every', control: '' })
    void save({ refresh: { ...settings.refresh, serviceStatus: seconds } }, () =>
      setRefreshInterval('serviceStatus', seconds),
    )
  }

  const changeWatch = (
    provider: StatusProviderID,
    change: (on: boolean, parts: StatusPartID[]) => [boolean, StatusPartID[]],
  ) => {
    const watch = settings.serviceStatus.watch.map((w) => {
      if (w.provider !== provider) {
        return w
      }

      const [on, parts] = change(w.on, w.parts)

      return { ...w, on, parts }
    })

    return { serviceStatus: { watch } }
  }

  const switchProvider = (provider: StatusProviderID, on: boolean) => {
    setChanged({ on: provider, control: 'switch' })
    void save(
      changeWatch(provider, (_, parts) => [on, parts]),
      () => setStatusProvider(provider, on),
    )
  }

  const tickPart = (provider: StatusProviderID, part: StatusPartID, on: boolean) => {
    setChanged({ on: provider, control: 'parts' })
    void save(
      changeWatch(provider, (wasOn, parts) => [
        wasOn,
        on ? [...parts.filter((p) => p !== part), part] : parts.filter((p) => p !== part),
      ]),
      () => setStatusPart(part, on),
    )
  }

  return (
    <div className="flex flex-col gap-5">
      <Group>
        <SettingRow
          label="Check every"
          controlId={`${id}-every`}
          description="One small request to each status page you watch, outside any API, so it costs none of your GitHub budget. A page that hasn’t changed answers with nothing new."
          error={everyError}
        >
          <select
            id={`${id}-every`}
            value={every}
            aria-describedby={rowDescribedBy(`${id}-every`, everyError)}
            onChange={(e) => {
              chooseEvery(Number(e.target.value))
            }}
            className="rounded-md border border-border bg-surface px-2 py-1 text-sm"
          >
            {settings.refreshChoices.serviceStatus.map((seconds) => (
              <option key={seconds} value={seconds}>
                {everyWords(seconds)}
                {seconds === refreshDefaults.serviceStatus ? ' (default)' : ''}
              </option>
            ))}
          </select>
        </SettingRow>
      </Group>
      <div className="flex flex-col gap-3">
        <SectionIntro title="What to watch">
          Only the parts you tick count: an incident elsewhere on a page shows no warning. While a part you watch has a
          problem, a warning shows in the toolbar.
        </SectionIntro>
        {settings.statusProviders.map((provider) => {
          const watch = watchOf(settings.serviceStatus, provider.id)

          return (
            <ProviderCard
              key={provider.id}
              provider={provider}
              on={watch.on}
              picked={watch.parts}
              live={service.status?.providers.find((p) => p.id === provider.id)}
              checkOff={checkOff}
              error={changed.on === provider.id ? error : ''}
              errorOn={changed.on === provider.id ? changed.control : ''}
              onProvider={(on) => {
                switchProvider(provider.id, on)
              }}
              onPart={(part, on) => {
                tickPart(provider.id, part, on)
              }}
            />
          )
        })}
      </div>
      <Note>
        A change to what you watch applies at once, with no request: the last readings are judged again. A page turned
        on is checked straight away.
      </Note>
    </div>
  )
}
