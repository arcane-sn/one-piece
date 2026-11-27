import { useCallback, useEffect, useState } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { api, baseName } from '../api'
import Column from '../components/Column'
import BoardModal from '../components/BoardModal'
import BrainstormModal from '../components/BrainstormModal'
import TicketModal from '../components/TicketModal'
import { FolderIcon } from '../components/Logo'
import SearchButton from '../components/SearchButton'
import Tip from '../components/Tip'

const svg = (children) => (
  <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{children}</svg>
)
const BULB = svg(<path d="M9 18h6M10 21h4M12 3a6 6 0 0 0-4 10.5c.7.7 1 1.5 1 2.5h6c0-1 .3-1.8 1-2.5A6 6 0 0 0 12 3Z" />)
const SLIDERS = svg(<><path d="M3 6h9M16 6h5M3 12h3M10 12h11M3 18h11M18 18h3" /><circle cx="14" cy="6" r="2" /><circle cx="8" cy="12" r="2" /><circle cx="16" cy="18" r="2" /></>)

const COLUMNS = [
  ['draft', 'Draft', '#9A8F86'],
  ['business', 'Business', '#EA9259'],
  ['agent', 'Agent', '#D65A42'],
  ['done', 'Done', '#7FC9C9'],
]

export default function Board({ reloadBoards }) {
  const { id } = useParams()
  const [search, setSearch] = useSearchParams()
  const [board, setBoard] = useState(null) // null = loading, false = not found
  const [cards, setCards] = useState([])
  const [error, setError] = useState('')
  const [ticket, setTicket] = useState(null) // null | 'new' | Card
  const [editing, setEditing] = useState(false)
  const [brainstorm, setBrainstorm] = useState(false)
  const [intro, setIntro] = useState(true) // the page-load fade is only for the first moments

  const load = useCallback(async () => {
    try {
      const [b, cs] = await Promise.all([api(`/api/boards/${id}`), api(`/api/boards/${id}/cards`)])
      setBoard(b)
      setCards(cs)
    } catch (e) {
      if (e.status === 404) setBoard(false)
      else setError(e.message)
    }
  }, [id])
  useEffect(() => { load() }, [load])
  // Cmd/Ctrl+N: new ticket on this board (not while a modal is already open)
  useEffect(() => {
    const onKey = (e) => {
      if (!(e.metaKey || e.ctrlKey) || e.altKey || e.shiftKey || e.key.toLowerCase() !== 'n') return
      if (!board || ticket || editing || brainstorm || document.querySelector('.modal-backdrop, .cmdk-overlay')) return
      e.preventDefault()
      setTicket('new')
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [board, ticket, editing, brainstorm])
  useEffect(() => {
    setIntro(true)
    const t = setTimeout(() => setIntro(false), 1400)
    return () => clearTimeout(t)
  }, [id])

  // ?card=<id> (from the Cmd+K palette) opens that ticket once the board has loaded.
  const wanted = search.get('card')
  useEffect(() => {
    if (!wanted || !board || String(board.id) !== String(id)) return
    const c = cards.find((x) => String(x.id) === wanted)
    if (c) setTicket(c)
    setSearch({}, { replace: true })
  }, [wanted, board, cards, id, setSearch])

  const upsert = (card) => {
    setCards((cs) => (cs.some((x) => x.id === card.id) ? cs.map((x) => (x.id === card.id ? card : x)) : [...cs, card]))
    reloadBoards()
  }
  const removed = (card) => {
    setCards((cs) => cs.filter((x) => x.id !== card.id))
    setTicket(null)
    reloadBoards()
  }

  // index = insertion index in the target column as currently displayed.
  async function moveCard(cardId, status, index) {
    const card = cards.find((c) => c.id === cardId)
    if (!card) return
    const col = (s) => cards.filter((c) => c.status === s).sort((a, b) => a.position - b.position)
    let pos = index
    if (card.status === status && col(status).findIndex((c) => c.id === cardId) < index) pos = index - 1
    const target = col(status).filter((c) => c.id !== cardId)
    pos = Math.max(0, Math.min(pos, target.length))
    target.splice(pos, 0, card)
    // optimistic update, renumbering every column
    setCards(
      COLUMNS.flatMap(([s]) =>
        (s === status ? target : col(s).filter((c) => c.id !== cardId)).map((c, i) => ({ ...c, status: s, position: i })),
      ),
    )
    setError('')
    try {
      await api(`/api/cards/${cardId}/move`, { method: 'POST', body: { status, position: pos } })
      reloadBoards()
    } catch (e) {
      setError(e.message)
      load()
    }
  }

  if (board === false) {
    return (
      <>
        <h1 className="title">Board Not Found</h1>
        <p className="sub-line"><Link to="/">← Back to dashboard</Link></p>
      </>
    )
  }
  if (board === null) return error ? <p className="error">{error}</p> : <p className="muted">Loading…</p>

  return (
    <>
      <div className="crumb"><Link to="/">Boards</Link> / {board.name}</div>
      <div className="page-head row">
        <h1 className="title">{board.name}</h1>
        <SearchButton />
      </div>
      <div className="board-tools">
        {board.folders.length === 0 && <span className="muted">No folders yet</span>}
        {board.folders.map((f) => (
          <Tip key={f} label={f} side="top"><span className="chip soft"><FolderIcon />{baseName(f)}</span></Tip>
        ))}
        <button className="tool-btn" onClick={() => setBrainstorm(true)}>{BULB}Brainstorming</button>
        <Tip label="Manage board" side="top">
          <button className="tool-btn icon" aria-label="Manage board" onClick={() => setEditing(true)}>{SLIDERS}</button>
        </Tip>
      </div>
      {error && <p className="error" style={{ marginBottom: 16 }}>{error}</p>}
      <div className={'columns' + (intro ? ' intro' : '')}>
        {COLUMNS.map(([status, label, dot]) => (
          <Column
            key={status}
            status={status}
            label={label}
            dot={dot}
            cards={cards.filter((c) => c.status === status).sort((a, b) => a.position - b.position)}
            onMove={moveCard}
            onOpen={setTicket}
            footer={status === 'business' ? <button className="add-card" onClick={() => setTicket('new')}>+ Add Card</button> : null}
          />
        ))}
      </div>
      {brainstorm && <BrainstormModal boardId={id} onCardsChanged={load} onClose={() => setBrainstorm(false)} />}
      {editing && (
        <BoardModal
          initial={board}
          onSaved={(b) => { setBoard(b); setEditing(false); reloadBoards() }}
          onClose={() => setEditing(false)}
        />
      )}
      {ticket && (
        <TicketModal
          key={ticket === 'new' ? 'new' : ticket.id}
          boardId={id}
          board={board}
          card={ticket === 'new' ? undefined : ticket}
          onChanged={upsert}
          onDeleted={removed}
          onClose={() => setTicket(null)}
        />
      )}
    </>
  )
}
