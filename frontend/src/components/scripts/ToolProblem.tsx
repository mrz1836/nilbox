import type { IntegrationTool } from '../../platform'
import { buttonClass } from '../controls'
import { AlertIcon } from '../icons'

export interface ToolProblemProps {
  // tool is the integration as the integrations list it, or undefined until listed.
  tool: IntegrationTool | undefined
  // binary and name are the tool's command and its integration's name, for before it is
  // listed: magex and mage-x, or go-pre-commit twice.
  binary: string
  name: string
  // page is the page that needs the tool, as in "which Release needs".
  page: string
  onOpenSettings: () => void
}

// ToolProblem stands in for a page's list while its tool can't be used, saying why, with
// the way to fix it in Settings: install it, update it, or look at it there.
export function ToolProblem({ tool, binary, name, page, onOpenSettings }: ToolProblemProps) {
  let text = `${binary} can’t be used yet.`
  let fix = 'Open Settings'

  switch (tool?.state) {
    case 'missing':
      text = `${binary} isn’t on this Mac.`
      fix = `Install ${name} in Settings`
      break
    case 'tooOld':
      text = `${binary} ${tool.copy.version.raw} is older than ${tool.minVersion}, which ${page} needs.`
      fix = `Update ${name} in Settings`
      break
    case 'wontRun':
      text = `${tool.copy.displayPath} is there but won’t run${tool.copy.error === '' ? '.' : `: ${tool.copy.error}`}`
      break
  }

  return (
    <div className="flex items-start gap-3 rounded-lg border border-warning px-4 py-3">
      <AlertIcon className="mt-0.5 size-4 shrink-0 text-warning" />
      <p className="min-w-0 flex-1 text-sm text-text">{text}</p>
      <button type="button" onClick={onOpenSettings} className={buttonClass}>
        {fix}
      </button>
    </div>
  )
}
