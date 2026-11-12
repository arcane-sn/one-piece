import { useCallback, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { api } from '../api'
import MentionTextarea from './MentionTextarea'
import { toast } from '../lib/toast'
import { refreshJobs } from '../lib/jobs'

const NOTE = { stopped: 'Stopped', failed: 'Failed' }
const VERDICT = { pass: ['pass', 'Passed'], needs_work: ['work', 'Needs Work'] }

// Claude Review as a chat: Claude reviews the ticket, asks what only the owner can decide, and applies
// fixes itself (description, title, Business Flows). It records a verdict every turn; the owner approves.
// `onTurnDone` fires when a turn ends and `onCardChanged(card)` after an Undo, so the ticket modal reloads.
export default function ReviewChatModal({ card, boardId, onTurnDone, onCardChanged, onClose }) {
  const base = `/api/cards/${card.id}/review-chat`
  const [view, setView] = useState(null) // { messages, running, available }
  const [review, setReview] = useState(undefined)
  const [text, setText] = useState('')
  const wasRunning = useRef(false)
  const listRef = useRef(null)

  const load = useCallback(async () => {
    try {
      const [v, r] = await Promise.all([api(base), api(`/api/cards/${card.id}/review`)])
      setView(v)
      setReview(r)
    } catch (e) {
      toast(e.message)
    }
  }, [base, card.id])
  useEffect(() => { load() }, [load])

  useEffect(() => {
    if (!view?.running) return
    const t = setInterval(load, 1000)
    return () => clearInterval(t)
  }, [view?.running, load])

  useEffect(() => {
    if (wasRunning.current && view && !view.running) onTurnDone?.()
    wasRunning.current = !!view?.running
  }, [view, onTurnDone])

  useEffect(() => {
    const el = listRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [view?.messages])

  useEffect(() => {
    const onKey = (e) => { if (e.key === 'Escape') { e.stopPropagation(); onClose() } }
    window.addEventListener('keydown', onKey, true)
    return () => window.removeEventListener('keydown', onKey, true)
  }, [onClose])

  async function act(path, body) {
    try {
      await api(`${base}${path}`, { method: 'POST', body: body ?? {} })
      await load()
      refreshJobs()
      return true
    } catch (e) {
      toast(e.message)
      return false
    }
  }

  async function send() {
    const t = text.trim()
    if (!t || view?.running) return
    if (await act('/messages', { text: t })) setText('')
  }

  async function undo(ref) {
    if (!window.confirm('Go back to the ticket text from before this change?')) return
    try {
      const next = await api(`/api/cards/${card.id}/revisions/${ref}/restore`, { method: 'POST', body: {} })
      onCardChanged?.(next)
      await load()
    } catch (e) {
      toast(e.message)
    }
  }

  const newSession = () =>
    window.confirm('Start a new session? The chat stays; Claude just starts fresh.') && act('/new')

  const running = !!view?.running
  const available = view?.available
  const current = review && review.status === 'done' && review.contentVersion === card.contentVersion ? review : null
  const verdict = current && VERDICT[current.verdict]

  return createPortal(
    <div className="modal-backdrop item-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="modal brainstorm" role="dialog" aria-modal="true" aria-label="Claude Review">
        <div className="ticket-head">
          <h2>Claude Review{card.ident && <span className="ident">{card.ident}</span>}</h2>
          <button type="button" className="icon-btn" aria-label="Close" onClick={onClose}>×</button>
        </div>
        <div className="discuss-stage review-strip">
          {!available && view ? 'Claude Review is available while the ticket is in Business.' : (
            verdict
              ? <><span className={'review-badge ' + verdict[0]}>{verdict[1]}</span> {current.summary}</>
              : <>Not reviewed yet. Say hello and Claude reviews the ticket and fixes what it can.</>
          )}
          {current?.verdict === 'needs_work' && current.missing.length > 0 && (
            <ul className="review-open">{current.missing.map((m, i) => <li key={i}>{m}</li>)}</ul>
          )}
        </div>
        <div className="bs-list" ref={listRef}>
          {view === null && <p className="muted">Loading…</p>}
          {view?.messages.length === 0 && (
            <p className="muted">Tell Claude what to focus on, or just send “review”. It edits the description and Business Flows directly; use Undo on a change line to go back.</p>
          )}
          {view?.messages.map((m) => m.role === 'marker' ? (
            <div key={m.id} className="discuss-marker"><span>{m.text}</span></div>
          ) : m.role === 'event' ? (
            <div key={m.id} className="review-event">
              <span>✎ {m.text}</span>
              {m.ref > 0 && <button type="button" className="link" disabled={running} onClick={() => undo(m.ref)}>Undo</button>}
            </div>
          ) : (
            <div key={m.id} className={'bs-msg' + (m.role === 'user' ? ' user' : '')}>
              {m.text || (m.status === 'running' ? 'Thinking…' : '')}
              {m.role === 'assistant' && NOTE[m.status] && <span className="bs-note">{NOTE[m.status]}</span>}
            </div>
          ))}
        </div>
        <div className="bs-input discuss-input">
          <MentionTextarea
            rows={2}
            maxLength={20000}
            placeholder="Message Claude…  (paste a screenshot to attach it; Enter sends, Shift+Enter adds a line)"
            value={text}
            disabled={running || !available}
            onChange={setText}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
                e.preventDefault()
                send()
              }
            }}
          />
          {running ? (
            <button type="button" className="danger" onClick={() => act('/stop')}><span className="spinner" aria-hidden="true" /> Thinking… Stop</button>
          ) : (
            <button type="button" className="primary" disabled={!text.trim() || !available} onClick={send}>Send</button>
          )}
        </div>
        <div className="bs-actions">
          <button type="button" className="link" disabled={running} onClick={newSession}>New session</button>
        </div>
      </div>
    </div>,
    document.body,
  )
}
