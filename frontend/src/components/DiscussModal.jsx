import { confirmDialog } from '../lib/confirm'
import { useStickyScroll } from '../lib/stickyScroll'
import Thinking from './Thinking'
import Markdown from './Markdown'
import ChatComposer, { ContinueChip, NewChatButton } from './ChatComposer'
import { useCallback, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { api } from '../api'
import MentionTextarea from './MentionTextarea'
import { toast } from '../lib/toast'

const NOTE = { stopped: 'Stopped', failed: 'Failed' }
const STAGE = { shape: 'Shaping the description and Business Flow', setup: 'Business Flow and branch setup', closeout: 'Closing out: integrate every repo, then push and open pull requests' }

// Per-ticket chat where Claude plans and runs the branching with the owner. Business: create the
// feature branches. Done: make the -dev branches and open the PRs. `onTurnDone` fires when a turn ends.
export default function DiscussModal({ card, boardId, onTurnDone, onCardChanged, onClose }) {
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

  useStickyScroll(listRef, view?.messages, null)

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

  async function send(override) {
    const t = (typeof override === 'string' ? override : text).trim()
    if (!t || view?.running) return
    if (await act('/messages', { text: t }) && typeof override !== 'string') setText('')
  }

  async function undo(ref) {
    if (!(await confirmDialog('Go back to the ticket text from before this change?', { okLabel: 'Undo', danger: true }))) return
    try {
      onCardChanged?.(await api(`/api/cards/${card.id}/revisions/${ref}/restore`, { method: 'POST', body: {} }))
      await load()
    } catch (e) {
      toast(e.message)
    }
  }

  const newSession = () =>
    confirmDialog('Start a new session? The chat and the Branching list stay; Claude just starts fresh.').then((ok) => ok && act('/new'))

  const running = !!view?.running
  const stage = view?.stage

  return createPortal(
    <div className="modal-backdrop item-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="modal brainstorm" role="dialog" aria-modal="true" aria-label="Discuss">
        <div className="ticket-head">
          <h2>Discuss{card.ident && <span className="ident">{card.ident}</span>}</h2>
          <div className="head-actions">
            <NewChatButton label="New session" disabled={running} onClick={newSession} />
            <button type="button" className="icon-btn" aria-label="Close" onClick={onClose}>×</button>
          </div>
        </div>
        {card.title && <p className="chat-ticket-title" title={card.title}>{card.title}</p>}
        <p className="discuss-stage">{stage ? STAGE[stage] : 'Loading…'}</p>
        {view?.branches?.length > 0 && (
          <div className="branch-strip" aria-label="Branches of this ticket">
            {view.branches.map((b) => (
              <span className="branch-chip" key={b.id}>
                <b>{b.repo}</b>
                <code>{b.featureBranch}</code>
                <span className={'branch-st ' + b.status}>{b.status.replace('_', ' ')}</span>
                {b.prUrl && <a href={b.prUrl} target="_blank" rel="noreferrer noopener">PR ↗</a>}
              </span>
            ))}
          </div>
        )}
        <div className="bs-list" ref={listRef}>
          {view === null && <p className="muted">Loading…</p>}
          {view?.messages.length === 0 && (
            <p className="muted">
              {stage === 'closeout'
                ? 'Tell Claude to wrap up. It integrates every repo into its -dev branch first, and pushes only when all of them are clean.'
                : stage === 'shape'
                  ? 'Talk through what this ticket should do. Claude asks about the cases and writes the description and Business Flow as things get settled (type @ to point at code).'
                  : 'Shape the flow with Claude, then tell it which repos this ticket touches (type @ to pick them), the release branch each starts from, and whether it goes via -dev or direct.'}
            </p>
          )}
          {view?.messages.map((m) => m.role === 'marker' ? (
            <div key={m.id} className="discuss-marker"><span>{m.text}</span></div>
          ) : m.role === 'event' ? (
            <div key={m.id} className="review-event">
              <span>✎ {m.text}</span>
              {m.ref > 0 && <button type="button" className="link" disabled={!!view?.running} onClick={() => undo(m.ref)}>Undo</button>}
            </div>
          ) : (
            <div key={m.id} className={'bs-msg' + (m.role === 'user' ? ' user' : '')}>
              {m.role === 'assistant' && m.text ? <Markdown text={m.text} /> : (m.text || (m.status === 'running' ? <Thinking /> : ''))}
              {m.role === 'assistant' && NOTE[m.status] && <span className="bs-note">{NOTE[m.status]}</span>}
            </div>
          ))}
          <ContinueChip messages={view?.messages} running={running} onContinue={() => send('continue')} />
        </div>
        <ChatComposer
          boardId={boardId}
          maxLength={20000}
          value={text}
          onChange={setText}
          onSend={send}
          onStop={() => act('/stop')}
          running={running}
          disabled={!stage}
        />
      </div>
    </div>,
    document.body,
  )
}
