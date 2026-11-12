import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { api } from '../api'
import FlowsSection from './FlowsSection'
import RichEditor from './RichEditor'
import { toast } from '../lib/toast'

const STATUS = [['todo', 'To Do'], ['doing', 'In Progress'], ['done', 'Done']]
const NAME = { agent: 'Agent Task', qa: 'QA Test Case' }

// Agent Task / QA Test Case: title, description, Technical Flows (agent tasks only)
// on the left; status and the Claude log on the right.
export default function ItemModal({ cardId, item, kind, onSaved, onDeleted, onClose }) {
  const k = item?.kind ?? kind
  const [current, setCurrent] = useState(item)
  const [title, setTitle] = useState(item?.title ?? '')
  const [body, setBody] = useState(item?.body ?? '')
  const [status, setStatus] = useState(item?.status ?? 'todo')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [flowOpen, setFlowOpen] = useState(false)
  const [readMore, setReadMore] = useState(false)
  const logRef = useRef(null)
  const running = current?.status === 'doing'

  const dirty = title !== (current?.title ?? '') || body !== (current?.body ?? '') || status !== (current?.status ?? 'todo')
  const close = () => {
    if (dirty && !window.confirm('Discard unsaved changes?')) return
    onClose()
  }

  useEffect(() => {
    const onKey = (e) => e.key === 'Escape' && !flowOpen && close()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })
  useEffect(() => {
    if (logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight
  }, [current?.log?.length])

  // While a run is going, follow the item so the log and status stay live.
  useEffect(() => {
    if (!running) return
    const t = setInterval(async () => {
      try {
        const all = await api(`/api/cards/${cardId}/items`)
        const next = all.find((x) => x.id === current.id)
        if (!next) return
        setCurrent(next)
        if (next.status !== 'doing') setStatus(next.status)
      } catch { /* keep polling */ }
    }, 1500)
    return () => clearInterval(t)
  }, [running, cardId, current?.id])

  async function runOrStop(action) {
    setError('')
    try {
      const next = await api(`/api/items/${current.id}/${action}`, { method: 'POST', body: {} })
      setCurrent(next)
      setStatus(next.status)
    } catch (err) {
      toast(err.message)
    }
  }

  async function restore() {
    try {
      const next = await api(`/api/items/${current.id}/restore`, { method: 'POST', body: {} })
      setCurrent(next)
      toast('Restored', 'ok')
    } catch (err) {
      toast(err.message)
    }
  }

  async function save(e) {
    e.preventDefault()
    e.stopPropagation() // React bubbles through the portal into the ticket form
    setBusy(true)
    setError('')
    const payload = { title, body, status }
    try {
      const next = current
        ? await api(`/api/items/${current.id}`, { method: 'PUT', body: payload })
        : await api(`/api/cards/${cardId}/items`, { method: 'POST', body: { ...payload, kind: k } })
      if (!current) { // keep the modal open so flows can be added to the new item
        setCurrent(next)
        setStatus(next.status)
        setBusy(false)
        return
      }
      onSaved(next)
    } catch (err) {
      setError(err.message)
      setBusy(false)
    }
  }

  async function remove() {
    if (!window.confirm(`Delete "${current.title}"?`)) return
    setBusy(true)
    try {
      await api(`/api/items/${current.id}`, { method: 'DELETE' })
      onDeleted(current)
    } catch (err) {
      setError(err.message)
      setBusy(false)
    }
  }

  return createPortal(
    <div className="modal-backdrop item-backdrop" onMouseDown={(e) => e.target === e.currentTarget && close()}>
      <form className="modal ticket" role="dialog" aria-modal="true" aria-label={(current ? 'Edit ' : 'New ') + NAME[k]} onSubmit={save}>
        <div className="ticket-head">
          <h2>{current ? 'Edit ' : 'New '}{NAME[k]}{current?.ident && <span className="ident">{current.ident}</span>}{current?.superseded && <span className="item-status stale">Superseded</span>}</h2>
          <button type="button" className="icon-btn" aria-label="Close" onClick={close}>×</button>
        </div>
        <div className="ticket-body">
          <div className="ticket-main">
            <label className="field">
              <span>Title</span>
              <input autoFocus maxLength={200} placeholder={k === 'agent' ? 'What should the agent do?' : 'What should be tested?'} value={title} onChange={(e) => setTitle(e.target.value)} />
            </label>
            <div className="field">
              <span>Description</span>
              <RichEditor value={body} placeholder={k === 'agent' ? 'Technical approach and what “done” looks like.' : 'Steps and the expected result.'} onChange={setBody} />
            </div>
            {k === 'agent' && (
              <FlowsSection parent={current ? `/api/items/${current.id}` : null} label="Technical Flow"
                emptyHint="No technical flow yet." onChanged={() => {}} onModal={setFlowOpen} />
            )}
          </div>
          <aside className="ticket-side">
            <div className="field">
              <span>Status</span>
              <div className="status-seg" role="group" aria-label="Status">
                {STATUS.map(([v, label]) => (
                  <button key={v} type="button" className={status === v ? 'on' : ''} aria-pressed={status === v} onClick={() => setStatus(v)}>{v === 'doing' && current?.running && <span className="spinner" aria-hidden="true" />}{label}</button>
                ))}
              </div>
            </div>
            {current?.superseded && (
              <div className="review-comment work">
                <div className="review-comment-head">Superseded</div>
                <p>A newer Generate replaced this {NAME[k]}. It is kept for history and is skipped by Run and Run All.</p>
                <button type="button" onClick={restore}>Restore</button>
              </div>
            )}
            {current && !current.superseded && (
              <div className="field">
                <span>Run</span>
                {running
                  ? <button type="button" onClick={() => runOrStop('stop')}>Stop</button>
                  : <button type="button" className="primary" disabled={dirty || current.status === 'done'} onClick={() => runOrStop('run')}>Run with Claude</button>}
                <p className="hint">{dirty ? 'Save your changes first.' : k === 'agent' ? 'Claude edits files in the board’s first folder. Needs an approved ticket.' : 'Claude checks this against the code. Run after all Agent Tasks are Done.'}</p>
              </div>
            )}
            {current?.failure && (
              <div className="review-comment work">
                <div className="review-comment-head">Why It Failed</div>
                <p><strong>Issue:</strong> {current.failure}</p>
                {current.failureDesc && (
                  <p className="failure-desc"><strong>Desc:</strong> {current.failureDesc}</p>
                )}
                {current.failureDesc && (
                  <button type="button" className="link-btn" onClick={() => { setReadMore(true); setFlowOpen(true) }}>Read more</button>
                )}
              </div>
            )}
            <div className="field review-side">
              <span>Claude Log</span>
              {current?.log?.length > 0
                ? <ul className="review-log" ref={logRef} aria-label="Claude log">{current.log.map((l, i) => <li key={i}>{l}</li>)}</ul>
                : <p className="hint">Nothing yet. Claude’s notes appear here.</p>}
            </div>
          </aside>
        </div>
        {error && <p className="error">{error}</p>}
        <div className="modal-actions ticket-actions">
          {current && <button type="button" className="danger" onClick={remove} disabled={busy}>Delete</button>}
          <span className="grow" />
          <button type="button" onClick={close}>Close</button>
          <button className="primary" type="submit" disabled={busy || !title.trim() || (current && !dirty)}>
            {current ? 'Save Changes' : 'Create'}
          </button>
        </div>
      </form>
      {readMore && createPortal(
        <div className="modal-backdrop item-backdrop" onMouseDown={(e) => e.target === e.currentTarget && (setReadMore(false), setFlowOpen(false))}>
          <div className="modal" role="dialog" aria-modal="true" aria-label="Why it failed">
            <div className="ticket-head">
              <h2>Why It Failed</h2>
              <button type="button" className="icon-btn" aria-label="Close" onClick={() => { setReadMore(false); setFlowOpen(false) }}>×</button>
            </div>
            <div className="failure-full">
              <p className="failure-label">Issue</p>
              <p>{current.failure}</p>
              <p className="failure-label">Desc</p>
              <p>{current.failureDesc}</p>
            </div>
            <div className="modal-actions"><span className="grow" /><button type="button" onClick={() => { setReadMore(false); setFlowOpen(false) }}>Close</button></div>
          </div>
        </div>,
        document.body,
      )}
    </div>,
    document.body,
  )
}
