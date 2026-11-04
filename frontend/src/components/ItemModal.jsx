import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { api } from '../api'
import FlowsSection from './FlowsSection'
import RichEditor from './RichEditor'

const STATUS = [['todo', 'To Do'], ['doing', 'Doing'], ['done', 'Done']]
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
  const logRef = useRef(null)

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
          <h2>{current ? 'Edit ' : 'New '}{NAME[k]}</h2>
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
                  <button key={v} type="button" className={status === v ? 'on' : ''} aria-pressed={status === v} onClick={() => setStatus(v)}>{label}</button>
                ))}
              </div>
            </div>
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
    </div>,
    document.body,
  )
}
