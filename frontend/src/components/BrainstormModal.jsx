import Markdown from './Markdown'
import ChatComposer, { ContinueChip, NewChatButton } from './ChatComposer'
import Tip from './Tip'
import { toast } from '../lib/toast'
import { useCallback, useEffect, useRef, useState } from 'react'
import { api } from '../api'
import MentionTextarea from './MentionTextarea'
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
  const [query, setQuery] = useState(null) // null = search closed
  const [drafted, setDrafted] = useState(() => new Set())
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

  async function send(override) {
    const t = (typeof override === 'string' ? override : text).trim()
    if (!t || view?.running) return
    if (await act('/brainstorm/messages', { text: t }) && typeof override !== 'string') setText('')
  }

  // One click: a Claude reply becomes a Draft ticket (title = its first line, description = the reply).
  async function makeDraft(m) {
    const first = m.text.split('\n').map((l) => l.replace(/^[#>*\-•\s]+/, '').trim()).find(Boolean) || 'Draft from chat'
    const title = Array.from(first).slice(0, 120).join('')
    try {
      await api(`${base}/drafts`, { method: 'POST', body: { title, description: m.text } })
      setDrafted((d) => new Set(d).add(m.id))
      toast('Added to Draft', 'ok')
      onCardsChanged()
    } catch (e) {
      toast(e.message)
    }
  }

  // Export = latest plan + the whole conversation, as one Markdown file.
  function exportAll() {
    const day = new Date().toISOString().slice(0, 10)
    const parts = []
    if (view?.plan) parts.push(`# Plan (version ${view.plan.version})\n\n${view.plan.content}`)
    const chat = (view?.messages ?? []).filter((m) => (m.role === 'user' || m.role === 'assistant') && m.text)
    if (chat.length) parts.push('# Conversation\n\n' + chat.map((m) => `**${m.role === 'user' ? 'You' : 'Claude'}:**\n\n${m.text}`).join('\n\n---\n\n'))
    if (parts.length === 0) return toast('Nothing to export yet')
    const url = URL.createObjectURL(new Blob([parts.join('\n\n---\n\n') + '\n'], { type: 'text/markdown' }))
    const a = Object.assign(document.createElement('a'), { href: url, download: `brainstorm-board-${boardId}-${day}.md` })
    a.click()
    URL.revokeObjectURL(url)
  }

  const newConversation = () =>
    window.confirm('Start a new conversation? The chat history and the plan stay; Claude just starts fresh.') &&
    act('/brainstorm/new')
  const restore = (v) =>
    window.confirm(`Restore plan version ${v}? It becomes the newest version; nothing is deleted.`) && act(`/plan/${v}/restore`)

  const running = !!view?.running
  const q = (query ?? '').trim().toLowerCase()
  const shown = (view?.messages ?? []).filter((m) => !q || m.text?.toLowerCase().includes(q))
  const latest = view?.plan?.version

  return (
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="modal brainstorm" role="dialog" aria-modal="true" aria-label="Brainstorming">
        <div className="ticket-head">
          <h2>Brainstorming</h2>
          <div className="head-actions">
            {tab === 'chat' && (
              <Tip label="Search chat" side="top">
                <button type="button" className="icon-btn" aria-label="Search chat" onClick={() => setQuery((q) => (q === null ? '' : null))}>
                  <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><circle cx="11" cy="11" r="7" /><path d="m20 20-3.5-3.5" /></svg>
                </button>
              </Tip>
            )}
            <Tip label="Export plan + chat (.md)" side="top">
              <button type="button" className="icon-btn" aria-label="Export" onClick={exportAll}>
                <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M12 3v12M7 10l5 5 5-5M5 21h14" /></svg>
              </button>
            </Tip>
            {tab === 'chat' && <NewChatButton label="New conversation" disabled={running} onClick={newConversation} />}
            <button type="button" className="icon-btn" aria-label="Close" onClick={onClose}>×</button>
          </div>
        </div>
        <div className="bs-tabs" role="tablist">
          <button role="tab" aria-selected={tab === 'chat'} className={tab === 'chat' ? 'on' : ''} onClick={() => setTab('chat')}>Chat</button>
          <button role="tab" aria-selected={tab === 'plan'} className={tab === 'plan' ? 'on' : ''} onClick={() => setTab('plan')}>Plan</button>
        </div>

        {tab === 'chat' ? (
          <>
            {query !== null && (
              <div className="bs-search">
                <input
                  autoFocus
                  placeholder="Search this chat…"
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  onKeyDown={(e) => { if (e.key === 'Escape') { e.stopPropagation(); setQuery(null) } }}
                />
                {query.trim() && <span className="bs-count">{shown.length} of {view?.messages.length ?? 0}</span>}
              </div>
            )}
            <div className="bs-list" ref={listRef}>
              {view === null && <p className="muted">Loading…</p>}
              {view?.messages.length === 0 && (
                <p className="muted">Tell Claude what this product should do. It plans with you and fills the Draft column.</p>
              )}
              {query?.trim() && shown.length === 0 && <p className="muted">No messages match.</p>}
              {shown.map((m) => (
                <div key={m.id} className={'bs-msg' + (m.role === 'user' ? ' user' : '')}>
                  {m.role === 'assistant' && m.text ? <Markdown text={m.text} /> : (m.text || (m.status === 'running' ? 'Thinking…' : ''))}
                  {m.role === 'assistant' && NOTE[m.status] && <span className="bs-note">{NOTE[m.status]}</span>}
                  {m.role === 'assistant' && m.text && m.status !== 'running' && (
                    <button type="button" className="bs-draft" disabled={drafted.has(m.id)} onClick={() => makeDraft(m)}>
                      {drafted.has(m.id) ? 'Added to Draft ✓' : '+ Draft'}
                    </button>
                  )}
                </div>
              ))}
              <ContinueChip messages={view?.messages} running={view?.running} onContinue={() => send('continue')} />
            </div>
            <ChatComposer
              boardId={boardId}
              value={text}
              onChange={setText}
              onSend={send}
              onStop={() => act('/brainstorm/stop')}
              running={running}
            />
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
