import { useCallback, useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import { api, baseName } from '../api'
import { toast } from '../lib/toast'

const STATUS = [['planned', 'Planned'], ['branched', 'Branched'], ['merged_dev', 'Merged To Dev'], ['pr_open', 'PR Open'], ['blocked', 'Blocked']]
const label = (s) => STATUS.find((x) => x[0] === s)?.[1] ?? s

// Per-repo branching of one ticket: base branch, feature branch, -dev branch, status and PR link.
export default function BranchingSection({ cardId, reloadKey = 0, onModal }) {
  const [rows, setRows] = useState([])
  const [editing, setEditing] = useState(null) // null | 'new' | row

  const load = useCallback(async () => {
    try { setRows(await api(`/api/cards/${cardId}/branches`)) } catch (err) { toast(err.message) }
  }, [cardId])
  useEffect(() => { load() }, [load, reloadKey])

  const open = (v) => { setEditing(v); onModal?.(!!v) }
  const done = () => { open(null); load() }

  return (
    <div className="field">
      <div className="field-row">
        <span>Branching</span>
        <span className="items-actions">
          <button type="button" className="ai-btn" onClick={() => open('new')}>+ Add Repo</button>
        </span>
      </div>
      {rows.length === 0 ? (
        <p className="hint">No branching yet. Add a repo, or ask Claude in a Discuss session.</p>
      ) : (
        <ul className="branch-list">
          {rows.map((r) => (
            <li key={r.id}>
              <button type="button" className="branch-row" onClick={() => open(r)}>
                <span className="branch-repo" title={r.repo}>{baseName(r.repo)}</span>
                <span className="branch-names">{r.baseBranch} → {r.featureBranch}</span>
                <span className={'branch-chip ' + r.status}>{label(r.status)}</span>
              </button>
              {r.prUrl && <a href={r.prUrl} target="_blank" rel="noreferrer noopener" className="link-btn">PR</a>}
            </li>
          ))}
        </ul>
      )}
      {editing && <BranchModal cardId={cardId} row={editing === 'new' ? null : editing} onDone={done} onClose={() => open(null)} />}
    </div>
  )
}

function BranchModal({ cardId, row, onDone, onClose }) {
  const [repo, setRepo] = useState(row?.repo ?? '')
  const [base, setBase] = useState(row?.baseBranch ?? '')
  const [feature, setFeature] = useState(row?.featureBranch ?? '')
  const [dev, setDev] = useState(row?.devBranch ?? '')
  const [status, setStatus] = useState(row?.status ?? 'planned')
  const [prUrl, setPrUrl] = useState(row?.prUrl ?? '')
  const [note, setNote] = useState(row?.note ?? '')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const url = `/api/cards/${cardId}/branches`

  useEffect(() => {
    const onKey = (e) => { if (e.key === 'Escape') { e.stopPropagation(); onClose() } }
    window.addEventListener('keydown', onKey, true)
    return () => window.removeEventListener('keydown', onKey, true)
  }, [onClose])

  async function submit(e) {
    e.preventDefault()
    e.stopPropagation() // the portal bubbles into the ticket form
    setBusy(true)
    setError('')
    try {
      if (!row) await api(url, { method: 'POST', body: { repo, baseBranch: base } })
      else await api(url, { method: 'PUT', body: { repo: row.repo, baseBranch: base, featureBranch: feature, devBranch: dev, status, prUrl, note } })
      onDone()
    } catch (err) {
      setError(err.message)
      setBusy(false)
    }
  }

  async function remove() {
    if (!window.confirm('Remove this repo from the ticket’s branching?')) return
    setBusy(true)
    try {
      await api(`${url}?repo=${encodeURIComponent(row.repo)}`, { method: 'DELETE' })
      onDone()
    } catch (err) {
      setError(err.message)
      setBusy(false)
    }
  }

  return createPortal(
    <div className="modal-backdrop item-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <form className="modal" role="dialog" aria-modal="true" aria-label="Branching" onSubmit={submit}>
        <div className="ticket-head">
          <h2>{row ? 'Edit Branching' : 'Add Repo'}</h2>
          <button type="button" className="icon-btn" aria-label="Close" onClick={onClose}>×</button>
        </div>
        <label className="field">
          <span>Repo Folder <em>inside the board folder</em></span>
          <input autoFocus={!row} disabled={!!row} placeholder="/path/to/master/repo" value={repo} onChange={(e) => setRepo(e.target.value)} />
        </label>
        <label className="field">
          <span>Base Branch <em>the feature branch is cut from it</em></span>
          <input value={base} placeholder="main" onChange={(e) => setBase(e.target.value)} />
        </label>
        {row && (
          <>
            <label className="field"><span>Feature Branch</span><input value={feature} onChange={(e) => setFeature(e.target.value)} /></label>
            <label className="field"><span>Dev Branch <em>merged into the shared dev branch via PR</em></span><input value={dev} onChange={(e) => setDev(e.target.value)} /></label>
            <label className="field">
              <span>Status</span>
              <select value={status} onChange={(e) => setStatus(e.target.value)}>
                {STATUS.map(([v, l]) => <option key={v} value={v}>{l}</option>)}
              </select>
            </label>
            <label className="field"><span>PR Link</span><input value={prUrl} onChange={(e) => setPrUrl(e.target.value)} /></label>
            <label className="field"><span>Note</span><textarea rows={2} value={note} onChange={(e) => setNote(e.target.value)} /></label>
          </>
        )}
        {error && <p className="error">{error}</p>}
        <div className="modal-actions">
          {row && <button type="button" className="danger" onClick={remove} disabled={busy}>Remove</button>}
          <span className="grow" />
          <button type="button" onClick={onClose}>Cancel</button>
          <button className="primary" type="submit" disabled={busy || !base.trim() || (!row && !repo.trim())}>{row ? 'Save' : 'Add'}</button>
        </div>
      </form>
    </div>,
    document.body,
  )
}
