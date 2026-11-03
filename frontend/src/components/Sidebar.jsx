import { NavLink } from 'react-router-dom'
import Logo from './Logo'

const SWATCH = ['#EA9259', '#7FC9C9', '#D65A42']
export const totalCards = (b) => Object.values(b.counts || {}).reduce((a, n) => a + n, 0)

export default function Sidebar({ user, boards, onLogout, onNewBoard }) {
  return (
    <aside className="sidebar">
      <NavLink to="/" className="brand-row" aria-label="One Piece home"><Logo size={28} /></NavLink>
      <NavLink to="/" end className="nav-item">
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <rect x="3" y="3" width="7" height="7" rx="1.5" /><rect x="14" y="3" width="7" height="7" rx="1.5" />
          <rect x="3" y="14" width="7" height="7" rx="1.5" /><rect x="14" y="14" width="7" height="7" rx="1.5" />
        </svg>
        <span className="grow">Dashboard</span>
      </NavLink>
      <div className="side-head">
        <span className="side-label">Boards</span>
        <button className="side-plus" title="New board" aria-label="New board" onClick={onNewBoard}>+</button>
      </div>
      {boards.map((b, i) => (
        <NavLink key={b.id} to={`/boards/${b.id}`} className="nav-item board-link">
          <span className="swatch" style={{ background: SWATCH[i % SWATCH.length] }} />
          <span className="grow">{b.name}</span>
          <span className="count">{totalCards(b)}</span>
        </NavLink>
      ))}
      <div className="side-user">
        <span className="avatar">{user.charAt(0).toUpperCase()}</span>
        <span className="grow user-name">{user}</span>
        <button className="link" onClick={onLogout}>Log out</button>
      </div>
    </aside>
  )
}
