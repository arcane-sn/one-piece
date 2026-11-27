const STATUS = { todo: 'To Do', doing: 'In Progress', done: 'Done' }
const EV = { passed: 'Run · passed', failed: 'Run · failed', blocked: 'Run · blocked', stopped: 'Run · stopped', running: 'Running…', stale: 'Run · outdated' }
const when = (s) => new Date(s).toLocaleString([], { dateStyle: 'short', timeStyle: 'short' })

// Proof that a QA test case was actually executed in the browser: state, time and steps passed.
function EvidenceChip({ e }) {
  if (!e) return <span className="item-status ev-none" title="No browser run yet">Not run</span>
  const bad = e.state !== 'passed' && e.state !== 'running'
  return (
    <span className={'item-status ' + (bad ? 'stale' : '')} title={`${when(e.at)} · ${e.stepsPassed}/${e.steps} steps passed · ${e.runs} run${e.runs === 1 ? '' : 's'}`}>
      {EV[e.state] ?? e.state}{e.steps > 0 ? ` ${e.stepsPassed}/${e.steps}` : ''}
    </span>
  )
}

export function EvidenceSummary({ list }) {
  const live = list.filter((i) => !i.superseded)
  const ran = live.filter((i) => i.evidence).length
  const ok = live.filter((i) => i.evidence?.state === 'passed').length
  const stale = live.filter((i) => i.evidence?.state === 'stale').length
  return (
    <p className={'hint ev-summary' + (ran < live.length ? ' warn' : '')}>
      Executed in the browser: {ran}/{live.length} · passed {ok}{stale > 0 ? ` · outdated ${stale}` : ''}{ran < live.length ? ` · ${live.length - ran} not run yet` : ''}
    </p>
  )
}

// One Agent Task or QA Test Case in a list: title, state tags (outdated, failed, blocked, browser run, changes) and status.
export default function ItemRow({ item: i, card, onOpen, struck = false }) {
  const last = i.log[i.log.length - 1]
  return (
    <li>
      <button type="button" onClick={() => onOpen(i)}>
        <span className="item-title">{i.ident && <span className="ident">{i.ident}</span>}{i.title}</span>
        <span className="item-tags">
          {i.version > 0 && i.version < card.contentVersion && (
            <span className="item-status stale" title="The ticket changed after Claude generated this. Regenerate to refresh.">Outdated</span>
          )}
          {i.kind === 'qa' && i.failure && i.status === 'todo' && <span className="item-status stale" title={i.failure}>Failed</span>}
          {i.status === 'todo' && !i.failure && last && /^(Failed|Blocked):/.test(last) && <span className="item-status stale" title={last}>Blocked</span>}
          {!struck && i.kind === 'qa' && <EvidenceChip e={i.evidence} />}
          {!struck && i.kind === 'agent' && i.changes && <span className="item-status ev-none" title="What this task committed">{i.changes.files} file{i.changes.files === 1 ? '' : 's'} +{i.changes.additions} −{i.changes.deletions}</span>}
          <span className={'item-status ' + i.status}>{i.running && <span className="spinner" aria-hidden="true" />}{STATUS[i.status]}</span>
        </span>
      </button>
    </li>
  )
}
