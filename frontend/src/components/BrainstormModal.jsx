import { useCallback, useEffect, useRef, useState } from 'react'
import { api } from '../api'
import RichEditor from './RichEditor'

const NOTE = { stopped: 'Stopped', failed: 'Failed' }

// Per-board planning chat with Claude. The reply and the plan arrive by polling
// while a turn runs; `onCardsChanged` fires when a turn ends (Claude may have added Drafts).
export default function BrainstormModal({ boardId, onClose, onCardsChanged }) {
  const base = `/api/boards/${boardId}`
  const [view, setView] = useState(null) // { messages, running, plan, versions }
  const [tab, setTab] = useState('chat')
  const [text, setText] = useState('')
  const [error, setError] = useState('')
  const wasRunning = useRef(false)
  const listRef = useRef(null)

  const load = useCallback(async () => {
    try {
      setView(await api(`${base}/brainstorm`))
    } catch (e) {
      setError(e.message)
    }
  }, [base])
  useEffect(() => { load() }, [load])

  useEffect(() => {
    if (!view?.running) return
    const t = setInterval(load, 1000)
    return () => clearInterval(t)
  }, [view?.running, load])

  useEffect(() => {
    if (wasRunning.current && view && !view.running) onCardsChanged()
    wasRunning.current = !!view?.running
  }, [view, onCardsChanged])

  useEffect(() => {
    const el = listRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [view?.messages, tab])

  useEffect(() => {
    const onKey = (e) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  async function act(path, body) {
    setError('')
    try {
      await api(`${base}${path}`, { method: 'POST', body: body ?? {} })
      await load()
      return true
    } catch (e) {
      setError(e.message)
      return false
    }
  }

  async function send() {
    const t = text.trim()
    if (!t || view?.running) return
    if (await act('/brainstorm/messages', { text: t })) setText('')
  }

  const newConversation = () =>
    window.confirm('Start a new conversation? The chat history and the plan stay; Claude just starts fresh.') &&
    act('/brainstorm/new')
  const restore = (v) =>
    window.confirm(`Restore plan version ${v}? It becomes the newest version; nothing is deleted.`) && act(`/plan/${v}/restore`)

  const running = !!view?.running
  const latest = view?.plan?.version

  return (
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="modal brainstorm" role="dialog" aria-modal="true" aria-label="Brainstorming">
        <div className="ticket-head">
          <h2>Brainstorming</h2>
          <button type="button" className="icon-btn" aria-label="Close" onClick={onClose}>×</button>
        </div>
        <div className="bs-tabs" role="tablist">
          <button role="tab" aria-selected={tab === 'chat'} className={tab === 'chat' ? 'on' : ''} onClick={() => setTab('chat')}>Chat</button>
          <button role="tab" aria-selected={tab === 'plan'} className={tab === 'plan' ? 'on' : ''} onClick={() => setTab('plan')}>Plan</button>
        </div>

        {tab === 'chat' ? (
          <>
            <div className="bs-list" ref={listRef}>
              {view === null && <p className="muted">Loading…</p>}
              {view?.messages.length === 0 && (
                <p className="muted">Tell Claude what this product should do. It plans with you and fills the Draft column.</p>
              )}
              {view?.messages.map((m) => (
                <div key={m.id} className={'bs-msg' + (m.role === 'user' ? ' user' : '')}>
                  {m.text || (m.status === 'running' ? 'Thinking…' : '')}
                  {m.role === 'assistant' && NOTE[m.status] && <span className="bs-note">{NOTE[m.status]}</span>}
                </div>
              ))}
            </div>
            <div className="bs-input">
              <textarea
                rows={2}
                placeholder="Message Claude…  (Enter sends, Shift+Enter adds a line)"
                value={text}
                disabled={running}
                onChange={(e) => setText(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
                    e.preventDefault()
                    send()
                  }
                }}
              />
              {running ? (
                <button type="button" className="danger" onClick={() => act('/brainstorm/stop')}>Stop</button>
              ) : (
                <button type="button" className="primary" disabled={!text.trim()} onClick={send}>Send</button>
              )}
            </div>
            <div className="bs-actions">
              <button type="button" className="link" disabled={running} onClick={newConversation}>New conversation</button>
            </div>
          </>
        ) : (
          <>
            {view?.plan ? (
              <RichEditor readOnly value={view.plan.content} onChange={() => {}} placeholder="" />
            ) : (
              <p className="muted">No plan yet. Brainstorm with Claude and it writes one here.</p>
            )}
            {view?.versions.length > 0 && (
              <ul className="bs-versions" aria-label="Plan versions">
                {view.versions.map((v) => (
                  <li key={v.version}>
                    <span>Version {v.version} · {v.createdAt.replace('T', ' ').replace('Z', '')}{v.version === latest ? ' (current)' : ''}</span>
                    <button type="button" disabled={running || v.version === latest} onClick={() => restore(v.version)}>Restore</button>
                  </li>
                ))}
              </ul>
            )}
          </>
        )}
        {error && <p className="error">{error}</p>}
      </div>
    </div>
  )
}
