import { useCallback, useEffect, useRef, useState } from 'react'
import { api } from '../api'
import PromptModal from './PromptModal'
import FlowsSection from './FlowsSection'
import ItemsSection from './ItemsSection'
import BranchingSection from './BranchingSection'
import DiscussModal from './DiscussModal'
import RichEditor from './RichEditor'
import { ReviewComment, ReviewSide, useReview } from './ReviewPanel'

const STATUS_LABEL = { draft: 'Draft', business: 'Business', agent: 'Agent', qa: 'QA', done: 'Done' }

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
  const [flowOpen, setFlowOpen] = useState(false)
  const [discussOpen, setDiscussOpen] = useState(false)
  const [branchTick, setBranchTick] = useState(0) // bumps when a Discuss turn ends, so Branching reloads
  const turnDone = useCallback(() => setBranchTick((n) => n + 1), [])
  const closeDiscuss = useCallback(() => { setDiscussOpen(false); setFlowOpen(false); setBranchTick((n) => n + 1) }, [])
  const titleRef = useRef(null)
  const { review, refresh, start } = useReview(current?.id)

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
    const onKey = (e) => e.key === 'Escape' && !promptOpen && !flowOpen && requestClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [requestClose, promptOpen, flowOpen])

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
      refresh() // the server starts a review when the content changed
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

  const contentChanged = title !== (current?.title ?? '') || description !== (current?.description ?? '')

  async function approve(value) {
    setBusy(true)
    setError('')
    try {
      const next = await api(`/api/cards/${current.id}/approve`, { method: 'POST', body: { approved: value } })
      setCurrent((c) => ({ ...c, ...next }))
      onChanged(next)
    } catch (err) {
      setError(err.message)
    } finally {
      setBusy(false)
    }
  }

  // A flow changed on the server: pick up the new content version, then follow the new review.
  async function flowsChanged() {
    try {
      const cards = await api(`/api/boards/${boardId}/cards`)
      const next = cards.find((c) => c.id === current.id)
      if (next) {
        setCurrent((c) => ({ ...c, contentVersion: next.contentVersion, approved: next.approved }))
        onChanged(next)
      }
    } catch {
      // the next save or review poll corrects this
    }
    refresh()
  }

  async function startReview() {
    setError('')
    try {
      await start()
    } catch (err) {
      setError(err.message)
    }
  }

  const isDraft = current?.status === 'draft' // drafts have no review, flows, or approval

  const edit = (setter) => (e) => {
    setter(e.target.value)
    setSaved(false)
  }

  return (
    <>
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && requestClose()}>
      <form className="modal ticket ticket-xl" role="dialog" aria-modal="true" aria-label={current ? 'Edit Ticket' : 'New Ticket'} onSubmit={save}>
        <div className="ticket-head">
          <h2>{current ? 'Edit Ticket' : 'New Ticket'}{current?.ident && <span className="ident">{current.ident}</span>}</h2>
          {current && (current.status === 'business' || current.status === 'done') && (
            <button type="button" className="ai-btn discuss-btn" title="Plan and run this ticket's branching with Claude"
              onClick={() => { setDiscussOpen(true); setFlowOpen(true) }}>
              <span aria-hidden="true">✦</span> Discuss
            </button>
          )}
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
            {!isDraft && <FlowsSection parent={current ? `/api/cards/${current.id}` : null} emptyHint="No flow yet. Claude asks for at least one before approval." onChanged={flowsChanged} onModal={setFlowOpen} />}
            {!isDraft && current && <BranchingSection cardId={current.id} reloadKey={branchTick} onModal={setFlowOpen} />}
            {!isDraft && <ItemsSection card={current} onModal={setFlowOpen} />}
            {!isDraft && <ReviewComment card={current} review={review} />}
          </div>

          <aside className="ticket-side">
            <div className="field">
              <span>Status</span>
              <span className="status-pill">{STATUS_LABEL[current?.status ?? 'business']}</span>
            </div>
            <div className="field">
              <span>Deadline</span>
              <div className="date-wrap">
                <input type="date" value={deadline} onChange={edit(setDeadline)} />
                {deadline && (
                  <button type="button" className="date-clear" aria-label="Clear Deadline" title="Clear Deadline"
                    onClick={() => { setDeadline(''); setSaved(false) }}>×</button>
                )}
              </div>
            </div>
            {!isDraft && <ReviewSide card={current} review={review} unsaved={contentChanged} busy={busy} onStart={startReview} onApprove={approve} />}
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
    {discussOpen && <DiscussModal card={current} boardId={boardId} onTurnDone={turnDone} onClose={closeDiscuss} />}
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
