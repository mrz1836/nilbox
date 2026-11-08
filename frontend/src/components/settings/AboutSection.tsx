import { brand } from '../../generated/brand'
import { useQuery } from '../../lib/useQuery'
import { getAppInfo, openExternal, type Info } from '../../platform'
import { buttonClass } from '../controls'
import { ExternalIcon } from '../icons'
import { Logo } from '../Logo'
import { Group, Note } from './rows'

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

// AboutSection shows the product, the build it runs, and a way to report a bug with that
// build's details filled in.
export function AboutSection() {
  const about = useQuery(() => getAppInfo(), [])
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
      <Group>
        {aboutRows(info, failure).map(([term, value]) => (
          <dl key={term} className="flex items-center justify-between gap-6 px-4 py-3 text-sm">
            <dt className="font-medium">{term}</dt>
            <dd className="text-right font-mono text-[13px] break-all text-muted">{value}</dd>
          </dl>
        ))}
      </Group>
      {info !== null && (
        <div className="flex items-center justify-between gap-6">
          <p className="text-sm text-muted">Found a bug? Open a report on GitHub with these details filled in.</p>
          <button
            type="button"
            onClick={() => {
              openExternal(info.bugReportUrl)
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
