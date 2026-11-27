import { confirmDialog } from '../lib/confirm'
import { KIND, rowKey, useActivity } from '../lib/activity'
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { api, baseName } from '../api'
import { FolderIcon } from '../components/Logo'
import { totalCards } from '../components/Sidebar'
import Tip from '../components/Tip'
import SearchButton from '../components/SearchButton'

const icon = (d) => (
  <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d={d} /></svg>
)
const EDIT = icon('M12 20h9M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4Z')
const TRASH = icon('M3 6h18M8 6V4a1 1 0 0 1 1-1h6a1 1 0 0 1 1 1v2M19 6l-1 14a1 1 0 0 1-1 1H7a1 1 0 0 1-1-1L5 6M10 11v6M14 11v6')

const STATUSES = ['draft', 'business', 'agent', 'done']

function greeting() {
  const h = new Date().getHours()
  return h < 12 ? 'Good Morning' : h < 18 ? 'Good Afternoon' : 'Good Evening'
}
const plural = (n, word) => `${n} ${word}${n === 1 ? '' : 's'}`

export default function Dashboard({ user, boards, reload, onNew, onEdit }) {
  const [error, setError] = useState('')
  const { rows, finished, dismiss, clearFinished } = useActivity()

  async function remove(b) {
    if (!(await confirmDialog(`Delete board "${b.name}" and all its cards?`, { okLabel: 'Delete', danger: true }))) return
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
        <SearchButton />
      </div>

      {error && <p className="error" style={{ marginBottom: 16 }}>{error}</p>}

      <div className="board-grid">
        {(boards ?? []).map((b) => (
          <div className="panel board-card" key={b.id}>
            <Link to={`/boards/${b.id}`} className="board-hit">
              <div className="board-top">
                <span className="board-name">{b.name}</span>
                <span className="board-count">{plural(totalCards(b), 'card')}</span>
              </div>
              <p className="board-desc">{b.description}</p>
              <div className="chips board-chips">
                {b.folders.map((f) => (
                  <span className="chip" key={f}><FolderIcon />{baseName(f)}</span>
                ))}
              </div>
              <div className="statbar" aria-hidden="true">
                {STATUSES.map((s) => <span key={s} style={{ flex: b.counts[s] }} />)}
              </div>
            </Link>
            <div className="board-actions">
              <Tip label="Edit" side="top"><button aria-label={`Edit ${b.name}`} onClick={() => onEdit(b)}>{EDIT}</button></Tip>
              <Tip label="Delete" side="top"><button className="danger" aria-label={`Delete ${b.name}`} onClick={() => remove(b)}>{TRASH}</button></Tip>
            </div>
          </div>
        ))}
        <button className="add-tile" onClick={onNew}>+ Add a Product</button>
      </div>

      <div className="section-title"><span className="pulse" />Agents at Work</div>
      <div className="panel">
        {rows.length === 0 && finished.length === 0 ? <span className="muted">All quiet on deck. No agents are running.</span> : (
          <ul className="activity-list">
            {rows.map((r) => (
              <li key={'run' + rowKey(r)} className="act-row">
                <Link to={`/boards/${r.boardId}`}>
                  <span className="act-flag running"><span className="spinner" aria-hidden="true" />Running</span>
                  <strong className="act-kind">{KIND[r.kind]}</strong>
                  <span className="act-title">{r.ident && <span className="ident">{r.ident}</span>}{r.title}</span>
                  <span className="act-board muted">{r.board}</span>
                  <span className="act-time muted">now</span>
                </Link>
                <span className="act-x" />
              </li>
            ))}
            {finished.map((r) => (
              <li key={'done' + rowKey(r)} className="act-row done">
                <Link to={`/boards/${r.boardId}`}>
                  <span className="act-flag done">Done ✓</span>
                  <strong className="act-kind">{KIND[r.kind]}</strong>
                  <span className="act-title">{r.ident && <span className="ident">{r.ident}</span>}{r.title}</span>
                  <span className="act-board muted">{r.board}</span>
                  <span className="act-time muted">{new Date(r.finishedAt).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</span>
                </Link>
                <button type="button" className="act-x" aria-label="Remove from this list" title="Remove from this list (the work itself is untouched)" onClick={() => dismiss(rowKey(r))}>×</button>
              </li>
            ))}
            {finished.length > 1 && <li><button type="button" className="link" onClick={clearFinished}>Clear finished</button></li>}
          </ul>
        )}
      </div>
    </>
  )
}
