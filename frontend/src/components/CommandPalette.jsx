import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api } from '../api'

const STATUS = {
  draft: ['Draft', '#9A8F86'],
  business: ['Business', '#EA9259'],
  agent: ['Agent', '#D65A42'],
  done: ['Done', '#7FC9C9'],
}
const MAX = 40

// Lower = better. -1 = no match. Every whitespace-separated term must match somewhere.
function score(text, ident, q) {
  const t = text.toLowerCase()
  const id = ident.toLowerCase()
  let best = 0
  for (const term of q.toLowerCase().split(/\s+/).filter(Boolean)) {
    let s
    if (id === term) s = 0
    else if (id.startsWith(term)) s = 1
    else if (t.startsWith(term)) s = 2
    else if (id.includes(term)) s = 3
    else if (t.includes(term)) s = 4
    else return -1
    best = Math.max(best, s)
  }
  return best
}

function ago(iso) {
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000)
  if (s < 60) return 'just now'
  if (s < 3600) return `${Math.floor(s / 60)}m ago`
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`
  return `${Math.floor(s / 86400)}d ago`
}

// Cmd/Ctrl+K: jump to a ticket (by identifier or title), a board, or the dashboard.
export default function CommandPalette({ boards }) {
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)
  const [q, setQ] = useState('')
  const [cards, setCards] = useState([]) // tickets of every board
  const [active, setActive] = useState(0)
  const [loading, setLoading] = useState(false)
  const listRef = useRef(null)

  useEffect(() => {
    const onKey = (e) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setOpen((o) => !o)
      }
    }
    const onOpen = () => setOpen(true)
    window.addEventListener('keydown', onKey)
    window.addEventListener('op:cmdk', onOpen)
    return () => { window.removeEventListener('keydown', onKey); window.removeEventListener('op:cmdk', onOpen) }
  }, [])

  // Fresh data on every open, so new tickets show up.
  useEffect(() => {
    if (!open) return
    let live = true
    setQ('')
    setActive(0)
    setLoading(true)
    Promise.all(boards.map((b) => api(`/api/boards/${b.id}/cards`).then((cs) => cs.map((c) => ({ ...c, boardName: b.name })), () => [])))
      .then((all) => { if (live) setCards(all.flat()) })
      .finally(() => { if (live) setLoading(false) })
    return () => { live = false }
  }, [open, boards])

  const items = useMemo(() => {
    const all = [
      { key: 'dash', kind: 'page', title: 'Dashboard', sub: 'All boards', ident: '', to: '/' },
      ...boards.map((b) => ({ key: `b${b.id}`, kind: 'board', title: b.name, sub: 'Board', ident: '', to: `/boards/${b.id}` })),
      ...cards.map((c) => ({ key: `c${c.id}`, kind: 'ticket', title: c.title, sub: c.boardName, ident: c.ident, status: c.status, at: c.updatedAt || '', id: c.id, to: `/boards/${c.boardId}?card=${c.id}` })),
    ]
    const query = q.trim()
    // no query: pages and boards, then the tickets changed most recently first
    if (!query) {
      const recent = all.filter((i) => i.kind === 'ticket').sort((a, b) => (b.at > a.at ? 1 : b.at < a.at ? -1 : b.id - a.id))
      return all.filter((i) => i.kind !== 'ticket').concat(recent).slice(0, MAX)
    }
    return all
      .map((i) => ({ i, s: score(i.title + ' ' + i.sub, i.ident, query) }))
      .filter((x) => x.s >= 0)
      .sort((a, b) => a.s - b.s)
      .map((x) => x.i)
      .slice(0, MAX)
  }, [q, cards, boards])

  const close = useCallback(() => setOpen(false), [])
  const go = useCallback((item) => { setOpen(false); if (item) navigate(item.to) }, [navigate])

  useEffect(() => {
    listRef.current?.querySelector('[data-active="true"]')?.scrollIntoView({ block: 'nearest' })
  }, [active, items])

  if (!open) return null

  const onKeyDown = (e) => {
    if (e.key === 'Escape') { e.preventDefault(); close() }
    else if (e.key === 'ArrowDown') { e.preventDefault(); setActive((a) => Math.min(a + 1, items.length - 1)) }
    else if (e.key === 'ArrowUp') { e.preventDefault(); setActive((a) => Math.max(a - 1, 0)) }
    else if (e.key === 'Enter') { e.preventDefault(); go(items[active]) }
  }

  return (
    <div className="cmdk-overlay" onMouseDown={close}>
      <div className="cmdk" role="dialog" aria-label="Jump to" onMouseDown={(e) => e.stopPropagation()}>
        <input
          className="cmdk-input"
          autoFocus
          placeholder="Jump to a ticket or board…  (try MID-12)"
          value={q}
          onChange={(e) => { setQ(e.target.value); setActive(0) }}
          onKeyDown={onKeyDown}
        />
        <div className="cmdk-list" ref={listRef}>
          {items.map((it, i) => (
            <div
              key={it.key}
              className="cmdk-item"
              data-active={i === active}
              onMouseMove={() => setActive(i)}
              onClick={() => go(it)}
            >
              {it.kind === 'ticket' && <span className="cmdk-ident">{it.ident}</span>}
              {it.kind !== 'ticket' && <span className="cmdk-kind">{it.kind === 'board' ? 'Board' : 'Page'}</span>}
              <span className="cmdk-title">{it.title}</span>
              {it.kind === 'ticket' && <span className="cmdk-sub">{it.sub}</span>}
              {it.kind === 'ticket' && !q.trim() && it.at && <span className="cmdk-ago">{ago(it.at)}</span>}
              {it.status && <span className="cmdk-status" style={{ '--c': STATUS[it.status]?.[1] }}>{STATUS[it.status]?.[0] ?? it.status}</span>}
            </div>
          ))}
          {items.length === 0 && <div className="cmdk-empty">{loading ? 'Loading…' : 'Nothing found'}</div>}
        </div>
        <div className="cmdk-foot"><span><kbd>↑</kbd><kbd>↓</kbd> navigate</span><span><kbd>↵</kbd> open</span><span><kbd>esc</kbd> close</span></div>
      </div>
    </div>
  )
}
