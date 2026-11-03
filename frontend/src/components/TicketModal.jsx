import { useCallback, useEffect, useRef, useState } from 'react'
import { api } from '../api'
import PromptModal from './PromptModal'
import RichEditor from './RichEditor'

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
  const [promptOpen, setPromptOpen] = useState(false)
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
    const onKey = (e) => e.key === 'Escape' && !promptOpen && requestClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [requestClose, promptOpen])

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
    <>
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && requestClose()}>
      <form className="modal ticket" role="dialog" aria-modal="true" aria-label={current ? 'Edit Ticket' : 'New Ticket'} onSubmit={save}>
        <div className="ticket-head">
          <h2>{current ? 'Edit Ticket' : 'New Ticket'}</h2>
          <button type="button" className="icon-btn" aria-label="Close" onClick={requestClose}>×</button>
        </div>

        <div className="ticket-body">
          <div className="ticket-main">
            <label className="field">
              <span>Title</span>
              <input ref={titleRef} placeholder="What needs to happen?" maxLength={200} value={title} onChange={edit(setTitle)} />
            </label>
            <div className="field">
              <div className="field-row">
                <span>Description</span>
                <button type="button" className="ai-btn" title="Generate Description With Claude" onClick={() => setPromptOpen(true)}>
                  <span aria-hidden="true">✦</span> Generate
                </button>
              </div>
              <RichEditor value={description} placeholder="Describe the change and what “done” looks like."
                onChange={(v) => { setDescription(v); setSaved(false) }} />
            </div>
          </div>

          <aside className="ticket-side">
            <div className="field">
              <span>Status</span>
              <span className="status-pill">{STATUS_LABEL[current?.status ?? 'business']}</span>
            </div>
            <div className="field">
              <span>Deadline</span>
              <input type="date" value={deadline} onChange={edit(setDeadline)} />
              {deadline && <button type="button" className="link" onClick={() => { setDeadline(''); setSaved(false) }}>Clear Deadline</button>}
            </div>
            <div className="field">
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
            {current ? 'Save Changes' : 'Create Ticket'}
          </button>
        </div>
      </form>
    </div>
    {promptOpen && (
      <PromptModal
        boardId={boardId}
        title={title}
        description={description}
        onUse={(md) => { setDescription(md); setSaved(false); setPromptOpen(false) }}
        onClose={() => setPromptOpen(false)}
      />
    )}
    </>
  )
}
