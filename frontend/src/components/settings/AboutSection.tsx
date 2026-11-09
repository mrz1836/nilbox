import { useId } from 'react'

import { brand } from '../../generated/brand'
import { useQuery } from '../../lib/useQuery'
import { agentVersions, bugReportURL, getAppInfo, openExternal, type AgentVersion, type Info } from '../../platform'
import { buttonClass } from '../controls'
import { ExternalIcon } from '../icons'
import { Logo } from '../Logo'
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

// versionOf is how the About page shows a coding agent's version: the version, with
// where its command is on hover; "not installed"; or "unknown", with why on hover.
function versionOf(a: AgentVersion): { text: string; title: string } {
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
            className="text-right font-mono text-[13px] break-all text-muted"
          >
            {value}
          </dd>
        </dl>
      ))}
    </Group>
  )
}

// AboutSection shows the product, the build it runs, the coding agents' versions, and a
// way to report a bug with all of them filled in.
export function AboutSection() {
  const id = useId()
  const about = useQuery(() => getAppInfo(), [])
  const agents = useQuery(() => agentVersions(), [])
  const info = about.data ?? null
  const failure = about.error

  return (
    <div className="flex flex-col gap-5">
      <div>
        <p className="flex">
          <Logo className="text-2xl" />
        </p>
        <p className="mt-2.5 text-sm text-muted">{brand.tagline}</p>
      </div>
      <Rows rows={aboutRows(info, failure)} />
      <section aria-labelledby={`${id}-agents`} className="flex flex-col gap-2">
        <SectionIntro id={`${id}-agents`} title="Coding agents">
          The versions installed, as each agent&apos;s command reports them.
        </SectionIntro>
        {agents.error !== '' ? (
          <p className="text-sm text-muted">Couldn&apos;t check the agents: {agents.error}</p>
        ) : agents.data === undefined ? (
          <p className="text-sm text-muted">Checking the agents…</p>
        ) : (
          <Rows
            rows={agents.data.map((a) => {
              const { text, title } = versionOf(a)

              return [a.name, text, title] as const
            })}
          />
        )}
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
