import { useEffect, useState } from 'react'
import { api, baseName } from '../api'

const LABEL = { running: 'Running', passed: 'Passed', failed: 'Failed', blocked: 'Blocked', stopped: 'Stopped' }
const ev = (run, attempt, file) => `/api/qa-evidence/${run}/${attempt}/${file}`

// Browser QA evidence of a QA Test Case: the latest runs with step screenshots, repos and SHAs, trace and video.
export default function QARunPanel({ itemId, refreshKey }) {
  const [runs, setRuns] = useState([])
  const [open, setOpen] = useState(null)
  useEffect(() => {
    api(`/api/items/${itemId}/qa-runs`).then((r) => { setRuns(r ?? []); setOpen((o) => o ?? r?.[0]?.id ?? null) }).catch(() => {})
  }, [itemId, refreshKey])
  if (runs.length === 0) return null
  return (
    <div className="field qa-runs">
      <span>Browser QA</span>
      {runs.map((r) => {
        const last = r.attempts || 1
        const steps = r.steps.filter((s) => s.attempt === last)
        return (
          <div className="qa-run" key={r.id}>
            <button type="button" className="link-btn" onClick={() => setOpen(open === r.id ? null : r.id)} aria-expanded={open === r.id}>
              <span className={`item-status ${r.status === 'passed' ? '' : 'stale'}`}>{LABEL[r.status] ?? r.status}</span>{' '}
              {new Date(r.startedAt).toLocaleString()} · {r.attempts} attempt{r.attempts === 1 ? '' : 's'}
            </button>
            {open === r.id && (
              <div className="qa-run-body">
                {r.blockReason && <p><strong>Blocked:</strong> {r.blockReason}</p>}
                {r.verdictIssue && <p><strong>Issue:</strong> {r.verdictIssue}</p>}
                {r.summary && <p>{r.summary}</p>}
                {r.blockedHosts?.length > 0 && <p className="hint">Blocked hosts: {r.blockedHosts.join(', ')}</p>}
                {r.repos?.length > 0 && (
                  <ul className="qa-repos">
                    {r.repos.map((p) => <li key={p.repo} title={p.repo}>{baseName(p.repo)} · {p.branch} · <code>{p.sha.slice(0, 8)}</code>{p.dirty ? ' · uncommitted changes' : ''}</li>)}
                  </ul>
                )}
                {steps.length > 0 && (
                  <ol className="qa-steps">
                    {steps.map((s) => (
                      <li key={s.position} className={s.status}>
                        <a href={ev(r.id, s.attempt, s.screenshot)} target="_blank" rel="noreferrer">
                          <img loading="lazy" alt={`Step ${s.position}: ${s.title}`} src={ev(r.id, s.attempt, s.screenshot)} />
                        </a>
                        <span>{s.position}. {s.title} <em>{s.status}</em></span>
                        {s.error && <code className="qa-err">{s.error}</code>}
                      </li>
                    ))}
                  </ol>
                )}
                {r.attempts > 0 && (
                  <p className="hint">
                    <a href={ev(r.id, last, 'trace.zip')}>Trace</a> (open at trace.playwright.dev or with <code>npx playwright show-trace</code>) · <a href={ev(r.id, last, 'video.webm')}>Video</a> · <a href={ev(r.id, 0, 'repos.json')}>repos.json</a>
                  </p>
                )}
              </div>
            )}
          </div>
        )
      })}
    </div>
  )
}
