import { useId } from 'react'

import { brand } from '../../generated/brand'
import { useQuery } from '../../lib/useQuery'
import {
  agentVersions,
  bugReportURL,
  getAppInfo,
  openExternal,
  toolVersions,
  type AgentVersion,
  type Info,
} from '../../platform'
import { buttonClass } from '../controls'
import { ExternalIcon } from '../icons'
import { Logo } from '../Logo'
import { cardMatchClass, useSetting } from './match'
import { Group, Note, SectionIntro } from './rows'

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

// AboutSection shows the product, the build it runs, the coding agents' and tools'
// versions, and a way to report a bug with all of them filled in.
export function AboutSection() {
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
      <section aria-labelledby={`${id}-agents`} className="flex flex-col gap-2">
        <SectionIntro id={`${id}-agents`} title="Coding agents">
          The versions installed, as each agent&apos;s command reports them.
        </SectionIntro>
        <Versions list={agents.data} error={agents.error} what="agents" />
      </section>
      <section aria-labelledby={`${id}-tools`} className="flex flex-col gap-2">
        <SectionIntro id={`${id}-tools`} title="Tools">
          The other programs {brand.name} runs, as each reports its version: the GitHub CLI, whose sign-in it borrows.
        </SectionIntro>
        <Versions list={tools.data} error={tools.error} what="tools" />
      </section>
      {info !== null && (
        <div className="flex items-center justify-between gap-6">
          <p className="text-sm text-muted">Found a bug? Open a report on GitHub with these versions filled in.</p>
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
      <Note>Your inbox, settings, and triage state stay on this Mac. {brand.name} talks only to GitHub.</Note>
    </div>
  )
}
