import { useCallback, useEffect, useRef, useState } from 'react'
import { api } from '../api'

const STATUS_LABEL = { business: 'Business', agent: 'Agent', qa: 'QA', done: 'Done' }

// Jira-style ticket modal: create (no `card`) or edit. Title/description on the
// left, a details panel (status, deadline, approval) on the right.
export default function TicketModal({ boardId, card, onChanged, onDeleted, onClose }) {
  const [current, setCurrent] = useState(card) // last saved card; set after the first create
  const [title, setTitle] = useState(card?.title ?? '')
  const [description, setDescription] = useState(card?.description ?? '')
  const [deadline, setDeadline] = useState(card?.deadline ?? '')
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState('')
  const titleRef = useRef(null)

  const dirty =
    title !== (current?.title ?? '') ||
    description !== (current?.description ?? '') ||
    deadline !== (current?.deadline ?? '')

  const requestClose = useCallback(() => {
    if (dirty && !window.confirm('Discard unsaved changes?')) return
    onClose()
  }, [dirty, onClose])

  useEffect(() => {
    if (!card) titleRef.current?.focus()
  }, [card])

  useEffect(() => {
    const onKey = (e) => e.key === 'Escape' && requestClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [requestClose])

  async function save(e) {
    e.preventDefault()
    setBusy(true)
    setError('')
    const body = { title, description, deadline }
    try {
      const next = current
        ? await api(`/api/cards/${current.id}`, { method: 'PUT', body })
        : await api(`/api/boards/${boardId}/cards`, { method: 'POST', body })
      setCurrent(next)
      setSaved(true)
      onChanged(next)
    } catch (err) {
      setError(err.message)
    } finally {
      setBusy(false)
    }
  }

  async function remove() {
    if (!window.confirm(`Delete "${current.title}"?`)) return
    setBusy(true)
    setError('')
    try {
      await api(`/api/cards/${current.id}`, { method: 'DELETE' })
      onDeleted(current)
    } catch (err) {
      setError(err.message)
      setBusy(false)
    }
  }

  const edit = (setter) => (e) => {
    setter(e.target.value)
    setSaved(false)
  }

  return (
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && requestClose()}>
      <form className="modal ticket" role="dialog" aria-modal="true" aria-label={current ? 'Edit ticket' : 'New ticket'} onSubmit={save}>
        <div className="ticket-head">
          <h2>{current ? 'Edit ticket' : 'New ticket'}</h2>
          <button type="button" className="icon-btn" aria-label="Close" onClick={requestClose}>×</button>
        </div>

        <div className="ticket-body">
          <div className="ticket-main">
            <label className="field">
              <span>Title</span>
              <input ref={titleRef} placeholder="What needs to happen?" maxLength={200} value={title} onChange={edit(setTitle)} />
            </label>
            <label className="field">
              <span>Description</span>
              <textarea rows={9} maxLength={10000} placeholder="Describe the change and what “done” looks like."
                value={description} onChange={edit(setDescription)} />
            </label>
          </div>

          <aside className="ticket-side">
            <div className="field">
              <span>Status</span>
              <span className="status-pill">{STATUS_LABEL[current?.status ?? 'business']}</span>
            </div>
            <div className="field">
              <span>Deadline</span>
              <input type="date" value={deadline} onChange={edit(setDeadline)} />
              {deadline && <button type="button" className="link" onClick={() => { setDeadline(''); setSaved(false) }}>Clear deadline</button>}
            </div>
            <div className="field">
              <span>Approval</span>
              <button type="button" disabled title="Available after Claude review">Approve</button>
              <p className="hint">Unlocks once Claude has reviewed this ticket.</p>
            </div>
          </aside>
        </div>

        {error && <p className="error">{error}</p>}
        <div className="modal-actions ticket-actions">
          {current && <button type="button" className="danger" onClick={remove} disabled={busy}>Delete</button>}
          <span className="grow" />
          {saved && !dirty && <span className="saved">Saved ✓</span>}
          <button type="button" onClick={requestClose}>Close</button>
          <button className="primary" type="submit" disabled={busy || !title.trim() || (current && !dirty)}>
            {current ? 'Save changes' : 'Create ticket'}
          </button>
        </div>
      </form>
    </div>
  )
}
