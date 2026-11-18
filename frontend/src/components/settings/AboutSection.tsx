import { useId, useState } from 'react'

import { brand } from '../../generated/brand'
import type { Result } from '../../lib/call'
import type { UpdateControl } from '../../lib/useUpdate'
import { useQuery } from '../../lib/useQuery'
import {
  agentVersions,
  bugReportURL,
  getAppInfo,
  openExternal,
  toolVersions,
  type AgentVersion,
  type Info,
  type Settings,
} from '../../platform'
import { buttonClass, dangerButtonClass } from '../controls'
import { ExternalIcon, ResetIcon } from '../icons'
import { Logo } from '../Logo'
import { cardMatchClass, useSetting } from './match'
import { ResetDialog } from './ResetDialog'
import { ErrorText, Group, Note, SectionIntro } from './rows'
import { UpdatesCard } from './UpdatesCard'

// aboutRows is what the About page lists: the release version and build number, the
// commit the build came from, and the system it runs on, which a bug report needs.
function aboutRows(info: Info | null, failure: string): [string, string][] {
  if (failure !== '') {
    return [['Version', `unknown (${failure})`]]
  }

  if (info === null) {
    return [['Version', '…']]
  }

  return [
    ['Version', `${info.release} (${info.build})`],
    ['Commit', info.version],
    ['System', `${info.system}, ${info.arch}`],
  ]
}

// Installed is a program whose version the About page shows: a coding agent or a tool.
type Installed = Pick<AgentVersion, 'name' | 'installed' | 'path' | 'version' | 'error'>

// versionOf is how the About page shows a program's version: the version, with where its
// command is on hover; "not installed"; or "unknown", with why on hover.
function versionOf(a: Installed): { text: string; title: string } {
  if (!a.installed) {
    return { text: 'not installed', title: '' }
  }

  return a.version === '' ? { text: 'unknown', title: a.error } : { text: a.version, title: a.path }
}

// Rows is a list of terms and their values, each value with a tooltip when it has one.
function Rows({ rows }: { rows: readonly (readonly [string, string, string?])[] }) {
  return (
    <Group>
      {rows.map(([term, value, title]) => (
        <dl key={term} className="flex items-center justify-between gap-6 px-4 py-3 text-sm">
          <dt className="font-medium">{term}</dt>
          <dd
            title={title === '' ? undefined : title}
            className="text-right font-mono text-[0.8125rem] break-all text-muted"
          >
            {value}
          </dd>
        </dl>
      ))}
    </Group>
  )
}

// Versions lists programs' versions: while they are checked, that it is checking, and
// why it couldn't when the check failed.
function Versions({ list, error, what }: { list: readonly Installed[] | undefined; error: string; what: string }) {
  if (error !== '') {
    return (
      <p className="text-sm text-muted">
        Couldn&apos;t check the {what}: {error}
      </p>
    )
  }

  if (list === undefined) {
    return <p className="text-sm text-muted">Checking the {what}…</p>
  }

  return (
    <Rows
      rows={list.map((a) => {
        const { text, title } = versionOf(a)

        return [a.name, text, title] as const
      })}
    />
  )
}

// SetupGroup offers the first-run setup again: every step shows what is set already, so it
// doubles as a check of the setup.
function SetupGroup({ onRunSetup }: { onRunSetup: () => Promise<Result<void>> }) {
  const [error, setError] = useState('')
  const [running, setRunning] = useState(false)

  const run = async () => {
    setError('')
    setRunning(true)

    const result = await onRunSetup()

    setRunning(false)

    if (!result.ok) {
      setError(result.error)
    }
  }

  return (
    <div className="flex flex-col gap-2">
      <SectionIntro
        title="Setup"
        action={
          <button
            type="button"
            disabled={running}
            onClick={() => {
              void run()
            }}
            className={`${buttonClass} shrink-0`}
          >
            Run setup again
          </button>
        }
      >
        Walk through the first-run setup again: GitHub, your clones, your agent, notifications, and your tools. Every
        step shows what is set already, so it doubles as a check of your setup.
      </SectionIntro>
      <ErrorText error={error} />
    </div>
  )
}

// ResetGroup offers to start the app over on fresh data, asking first.
function ResetGroup() {
  const [asking, setAsking] = useState(false)

  return (
    <div className="flex flex-col gap-2">
      <SectionIntro
        title="Reset"
        action={
          <button
            type="button"
            onClick={() => {
              setAsking(true)
            }}
            className={`${dangerButtonClass} shrink-0`}
          >
            <ResetIcon className="size-3.5" />
            Reset {brand.name}…
          </button>
        }
      >
        Start over on fresh data, keeping your prompts, launch defaults, your own scripts, and your Autopilot routines.{' '}
        {brand.name} saves a backup of your setup first.
      </SectionIntro>
      {asking && (
        <ResetDialog
          onCancel={() => {
            setAsking(false)
          }}
        />
      )}
    </div>
  )
}

export interface AboutSectionProps {
  // onRunSetup opens the first-run setup again; without it About offers no way to.
  onRunSetup?: (() => Promise<Result<void>>) | undefined
  // update is the app's update, which the Updates card under the version shows, with the
  // settings its choice of what to do is stored in; without it the card doesn't show.
  update?: UpdateControl | undefined
  settings?: Settings | null
  onSaved?: (settings: Settings) => void
}

// AboutSection shows the product, the build it runs and its updates, the coding agents'
// and tools' versions, and a way to report a bug with all of them filled in; then the
// first-run setup, to run again, and a reset of the app, to start over.
export function AboutSection({ onRunSetup, update, settings = null, onSaved = () => undefined }: AboutSectionProps) {
  const id = useId()
  const about = useQuery(() => getAppInfo(), [])
  const agents = useQuery(() => agentVersions(), [])
  const tools = useQuery(() => toolVersions(), [])
  const info = about.data ?? null
  const failure = about.error
  const [marks] = useSetting('Version')

  return (
    <div className="flex flex-col gap-5">
      <div>
        <p className="flex">
          <Logo className="text-2xl" />
        </p>
        <p className="mt-2.5 text-sm text-muted">{brand.tagline}</p>
      </div>
      <div {...marks} className={`rounded-lg ${cardMatchClass}`}>
        <Rows rows={aboutRows(info, failure)} />
      </div>
      {update !== undefined && <UpdatesCard update={update} settings={settings} onSaved={onSaved} />}
      <section aria-labelledby={`${id}-agents`} className="flex flex-col gap-2">
        <SectionIntro id={`${id}-agents`} title="Coding agents">
          The versions installed, as each agent&apos;s command reports them.
        </SectionIntro>
        <Versions list={agents.data} error={agents.error} what="agents" />
      </section>
      <section aria-labelledby={`${id}-tools`} className="flex flex-col gap-2">
        <SectionIntro id={`${id}-tools`} title="Tools">
          The other programs {brand.name} runs, as each reports its version: the GitHub CLI, whose sign-in it borrows,
          and Git, which makes each launch&apos;s worktree and tells which clone is which repository.
        </SectionIntro>
        <Versions list={tools.data} error={tools.error} what="tools" />
      </section>
      {info !== null && (
        <div className="flex items-center justify-between gap-6">
          <p className="text-sm text-muted">
            Found a bug? Open a report on GitHub with these versions, your settings, and your integrations filled in,
            folded away under the versions. Nothing personal goes in it: no account, repository, path, or name of your
            own. You read it on GitHub before you send it.
          </p>
          <button
            type="button"
            onClick={() => {
              // The link is made now, so it carries the agents' versions as they are.
              bugReportURL().then(openExternal, (err: unknown) => {
                console.error('could not make the bug report link', err)
              })
            }}
            className={`${buttonClass} shrink-0`}
          >
            Report a bug
            <ExternalIcon className="size-3.5 text-muted" />
          </button>
        </div>
      )}
      {onRunSetup !== undefined && <SetupGroup onRunSetup={onRunSetup} />}
      <ResetGroup />
      <Note>Your inbox, settings, and triage state stay on this Mac. {brand.name} talks only to GitHub.</Note>
    </div>
  )
}
