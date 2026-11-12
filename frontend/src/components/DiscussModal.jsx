import { useCallback, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { api } from '../api'
import MentionTextarea from './MentionTextarea'
import { toast } from '../lib/toast'

const NOTE = { stopped: 'Stopped', failed: 'Failed' }
const STAGE = { setup: 'Setting up branches', closeout: 'Closing out: dev branch and pull requests' }

// Per-ticket chat where Claude plans and runs the branching with the owner. Business: create the
// feature branches. Done: make the -dev branches and open the PRs. `onTurnDone` fires when a turn ends.
export default function DiscussModal({ card, boardId, onTurnDone, onClose }) {
  const base = `/api/cards/${card.id}/discuss`
  const [view, setView] = useState(null) // { messages, running, stage }
  const [text, setText] = useState('')
  const wasRunning = useRef(false)
  const listRef = useRef(null)

  const load = useCallback(async () => {
    try {
      setView(await api(base))
    } catch (e) {
      toast(e.message)
    }
  }, [base])
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

  const newSession = () =>
    window.confirm('Start a new session? The chat and the Branching list stay; Claude just starts fresh.') && act('/new')

  const running = !!view?.running
  const stage = view?.stage

  return createPortal(
    <div className="modal-backdrop item-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="modal brainstorm" role="dialog" aria-modal="true" aria-label="Discuss">
        <div className="ticket-head">
          <h2>Discuss{card.ident && <span className="ident">{card.ident}</span>}</h2>
          <button type="button" className="icon-btn" aria-label="Close" onClick={onClose}>×</button>
        </div>
        <p className="discuss-stage">{stage ? STAGE[stage] : 'Discuss is available while the ticket is in Business or Done.'}</p>
        <div className="bs-list" ref={listRef}>
          {view === null && <p className="muted">Loading…</p>}
          {view?.messages.length === 0 && (
            <p className="muted">
              {stage === 'closeout'
                ? 'Tell Claude to wrap up. It merges each feature branch into a -dev branch, pushes, and opens the pull requests.'
                : 'Tell Claude which repos this ticket touches (type @ to pick them) and which branch each one starts from.'}
            </p>
          )}
          {view?.messages.map((m) => m.role === 'marker' ? (
            <div key={m.id} className="discuss-marker"><span>{m.text}</span></div>
          ) : (
            <div key={m.id} className={'bs-msg' + (m.role === 'user' ? ' user' : '')}>
              {m.text || (m.status === 'running' ? 'Thinking…' : '')}
              {m.role === 'assistant' && NOTE[m.status] && <span className="bs-note">{NOTE[m.status]}</span>}
            </div>
          ))}
        </div>
        <div className="bs-input discuss-input">
          <MentionTextarea
            boardId={boardId}
            rows={2}
            maxLength={20000}
            placeholder="Message Claude…  (@ picks a repo; paste a screenshot to attach it; Enter sends, Shift+Enter adds a line)"
            value={text}
            disabled={running || !stage}
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
            <button type="button" className="primary" disabled={!text.trim() || !stage} onClick={send}>Send</button>
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
