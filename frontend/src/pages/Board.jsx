import { useCallback, useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api, baseName } from '../api'
import Column from '../components/Column'
import BoardModal from '../components/BoardModal'
import TicketModal from '../components/TicketModal'
import { FolderIcon } from '../components/Logo'

const COLUMNS = [
  ['business', 'Business', '#EA9259'],
  ['agent', 'Agent', '#D65A42'],
  ['qa', 'QA', '#3B2417'],
  ['done', 'Done', '#7FC9C9'],
]

export default function Board({ reloadBoards }) {
  const { id } = useParams()
  const [board, setBoard] = useState(null) // null = loading, false = not found
  const [cards, setCards] = useState([])
  const [error, setError] = useState('')
  const [ticket, setTicket] = useState(null) // null | 'new' | Card
  const [editing, setEditing] = useState(false)

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
      <div className="page-head">
        <h1 className="title">{board.name}</h1>
        <div className="chips right">
          {board.folders.length === 0 && <span className="muted">No folders yet</span>}
          {board.folders.map((f) => (
            <span className="chip soft" key={f} title={f}><FolderIcon />{baseName(f)}</span>
          ))}
          <button onClick={() => setEditing(true)}>Edit Board</button>
        </div>
      </div>
      {error && <p className="error" style={{ marginBottom: 16 }}>{error}</p>}
      <div className="columns">
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
          card={ticket === 'new' ? undefined : ticket}
          onChanged={upsert}
          onDeleted={removed}
          onClose={() => setTicket(null)}
        />
      )}
    </>
  )
}
