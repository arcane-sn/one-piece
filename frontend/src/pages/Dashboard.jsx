import { useEffect, useState } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { api, baseName } from '../api'
import BoardForm from '../components/BoardForm'
import { FolderIcon } from '../components/Logo'
import { totalCards } from '../components/Sidebar'

const STATUSES = ['business', 'agent', 'qa', 'done']

function greeting() {
  const h = new Date().getHours()
  return h < 12 ? 'Good morning' : h < 18 ? 'Good afternoon' : 'Good evening'
}
const plural = (n, word) => `${n} ${word}${n === 1 ? '' : 's'}`

export default function Dashboard({ user, boards, reload }) {
  const location = useLocation()
  const [editing, setEditing] = useState(location.state?.newBoard ? 'new' : null) // null | 'new' | Board
  const [error, setError] = useState('')

  // the sidebar's "+ New board" navigates here with state
  useEffect(() => {
    if (location.state?.newBoard) setEditing('new')
  }, [location.key, location.state])

  async function remove(b) {
    if (!window.confirm(`Delete board "${b.name}" and all its cards?`)) return
    try {
      await api(`/api/boards/${b.id}`, { method: 'DELETE' })
      reload()
    } catch (e) {
      setError(e.message)
    }
  }

  const cards = (boards ?? []).reduce((n, b) => n + totalCards(b), 0)

  return (
    <>
      <div className="page-head">
        <div>
          <h1 className="title">{greeting()}, {user}</h1>
          <div className="sub-line">
            {boards === null ? 'Loading…' : `${plural(boards.length, 'board')} · ${plural(cards, 'card')}`}
          </div>
        </div>
        <button className="primary" onClick={() => setEditing('new')}>+ New board</button>
      </div>

      {error && <p className="error" style={{ marginBottom: 16 }}>{error}</p>}
      {editing && (
        <BoardForm
          key={editing === 'new' ? 'new' : editing.id}
          initial={editing === 'new' ? undefined : editing}
          onSaved={() => { setEditing(null); reload() }}
          onCancel={() => setEditing(null)}
        />
      )}

      <div className="board-grid">
        {(boards ?? []).map((b) => (
          <div className="panel board-card" key={b.id}>
            <Link to={`/boards/${b.id}`} className="board-hit">
              <div className="board-top">
                <span className="board-name">{b.name}</span>
                <span className="board-count">{plural(totalCards(b), 'card')}</span>
              </div>
              {b.folders.length > 0 && (
                <div className="chips">
                  {b.folders.map((f) => (
                    <span className="chip" key={f} title={f}><FolderIcon />{baseName(f)}</span>
                  ))}
                </div>
              )}
              <div className="statbar" aria-hidden="true">
                {STATUSES.map((s) => <span key={s} style={{ flex: b.counts[s] }} />)}
              </div>
            </Link>
            <div className="board-actions">
              <button onClick={() => setEditing(b)}>Edit</button>
              <button className="danger" onClick={() => remove(b)}>Delete</button>
            </div>
          </div>
        ))}
        <button className="add-tile" onClick={() => setEditing('new')}>+ Add a product</button>
      </div>

      <div className="section-title"><span className="pulse" />Agents at work</div>
      <div className="panel"><span className="muted">All quiet on deck. No agents are running.</span></div>
    </>
  )
}
