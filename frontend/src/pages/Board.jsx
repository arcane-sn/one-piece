import { useCallback, useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api, baseName } from '../api'
import Column from '../components/Column'
import { FolderIcon } from '../components/Logo'

const COLUMNS = [
  ['business', 'Business', '#EA9259'],
  ['agent', 'Agent', '#D65A42'],
  ['qa', 'QA', '#3B2417'],
  ['done', 'Done', '#7FC9C9'],
]

function AddCard({ onAdd }) {
  const [open, setOpen] = useState(false)
  const [title, setTitle] = useState('')
  const [desc, setDesc] = useState('')

  async function submit(e) {
    e.preventDefault()
    if (await onAdd(title, desc)) {
      setTitle('')
      setDesc('')
      setOpen(false)
    }
  }
  if (!open) return <button className="add-card" onClick={() => setOpen(true)}>+ Add card</button>
  return (
    <form className="form" onSubmit={submit}>
      <input autoFocus placeholder="Card title" value={title} onChange={(e) => setTitle(e.target.value)} />
      <textarea rows={2} placeholder="Description (optional)" value={desc} onChange={(e) => setDesc(e.target.value)} />
      <div className="row">
        <button className="primary" type="submit" disabled={!title.trim()} style={{ height: 36 }}>Add card</button>
        <button type="button" onClick={() => setOpen(false)}>Cancel</button>
      </div>
    </form>
  )
}

export default function Board({ reloadBoards }) {
  const { id } = useParams()
  const [board, setBoard] = useState(null) // null = loading, false = not found
  const [cards, setCards] = useState([])
  const [error, setError] = useState('')

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

  async function guarded(fn) {
    setError('')
    try {
      await fn()
      reloadBoards() // keep sidebar counts fresh
      return true
    } catch (e) {
      setError(e.message)
      return false
    }
  }

  const addCard = (title, description) =>
    guarded(async () => {
      const c = await api(`/api/boards/${id}/cards`, { method: 'POST', body: { title, description } })
      setCards((cs) => [...cs, c])
    })

  const saveCard = (cardId, title, description) =>
    guarded(async () => {
      const c = await api(`/api/cards/${cardId}`, { method: 'PUT', body: { title, description } })
      setCards((cs) => cs.map((x) => (x.id === cardId ? c : x)))
    })

  const deleteCard = (card) =>
    guarded(async () => {
      await api(`/api/cards/${card.id}`, { method: 'DELETE' })
      setCards((cs) => cs.filter((x) => x.id !== card.id))
    })

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
        <h1 className="title">Board not found</h1>
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
            onSave={saveCard}
            onDelete={deleteCard}
            footer={status === 'business' ? <AddCard onAdd={addCard} /> : null}
          />
        ))}
      </div>
    </>
  )
}
