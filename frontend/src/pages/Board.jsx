import { useCallback, useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api } from '../api'
import Column from '../components/Column'

const COLUMNS = [
  ['business', 'Business'],
  ['agent', 'Agent'],
  ['qa', 'QA'],
  ['done', 'Done'],
]

function AddCard({ onAdd }) {
  const [title, setTitle] = useState('')
  const [desc, setDesc] = useState('')

  async function submit(e) {
    e.preventDefault()
    if (await onAdd(title, desc)) {
      setTitle('')
      setDesc('')
    }
  }
  return (
    <form className="form" onSubmit={submit}>
      <input placeholder="Card title" value={title} onChange={(e) => setTitle(e.target.value)} />
      <textarea rows={2} placeholder="Description (optional)" value={desc} onChange={(e) => setDesc(e.target.value)} />
      <button type="submit" disabled={!title.trim()}>+ Add card</button>
    </form>
  )
}

export default function Board() {
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
    try {
      await api(`/api/cards/${cardId}/move`, { method: 'POST', body: { status, position: pos } })
    } catch (e) {
      setError(e.message)
      load()
    }
  }

  if (board === false) {
    return (
      <>
        <p>Board not found.</p>
        <Link to="/">← Back to dashboard</Link>
      </>
    )
  }
  if (board === null) return error ? <p className="error">{error}</p> : <p className="muted">Loading…</p>

  return (
    <>
      <Link to="/">← Dashboard</Link>
      <h2>{board.name}</h2>
      <p className="muted">{board.folders.length ? board.folders.join(' · ') : 'No folders yet'}</p>
      {error && <p className="error">{error}</p>}
      <div className="columns">
        {COLUMNS.map(([status, label]) => (
          <Column
            key={status}
            status={status}
            label={label}
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
