import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import BoardForm from '../components/BoardForm'

export default function Dashboard() {
  const [boards, setBoards] = useState(null)
  const [editing, setEditing] = useState(null) // null | 'new' | Board
  const [error, setError] = useState('')

  const load = () => api('/api/boards').then(setBoards).catch((e) => setError(e.message))
  useEffect(() => { load() }, [])

  async function remove(b) {
    if (!window.confirm(`Delete board "${b.name}" and all its cards?`)) return
    try {
      await api(`/api/boards/${b.id}`, { method: 'DELETE' })
      load()
    } catch (e) {
      setError(e.message)
    }
  }

  return (
    <>
      <h2>Boards</h2>
      {error && <p className="error">{error}</p>}
      {boards === null ? (
        <p className="muted">Loading…</p>
      ) : (
        <div className="board-list">
          {boards.map((b) => (
            <div className="panel board-item" key={b.id}>
              <Link to={`/boards/${b.id}`}><strong>{b.name}</strong></Link>
              {b.folders.map((f) => <small key={f}>{f}</small>)}
              <div className="row" style={{ marginTop: '.5rem' }}>
                <button onClick={() => setEditing(b)}>Edit</button>
                <button className="danger" onClick={() => remove(b)}>Delete</button>
              </div>
            </div>
          ))}
          {boards.length === 0 && <p className="muted" style={{ gridColumn: '1 / -1' }}>No boards yet. Create one for your first product.</p>}
        </div>
      )}
      {editing ? (
        <BoardForm
          key={editing === 'new' ? 'new' : editing.id}
          initial={editing === 'new' ? undefined : editing}
          onSaved={() => { setEditing(null); load() }}
          onCancel={() => setEditing(null)}
        />
      ) : (
        <button className="primary" onClick={() => setEditing('new')}>+ New board</button>
      )}

      <h2 style={{ marginTop: '2rem' }}>Agents at work</h2>
      <p className="muted">All quiet on deck. No agents are running.</p>
    </>
  )
}
