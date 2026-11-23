import { NavLink } from 'react-router-dom'
import Logo from './Logo'
import Tip from './Tip'
import UsageCard from './UsageCard'

const SWATCH = ['#EA9259', '#7FC9C9', '#D65A42']
export const totalCards = (b) => Object.values(b.counts || {}).reduce((a, n) => a + n, 0)

const Chevrons = ({ flip }) => (
  <svg width="14" height="14" viewBox="0 0 14 14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" style={{ transform: flip ? 'scaleX(-1)' : 'none', transition: 'transform 0.28s var(--ease)' }}>
    <path d="M7.5 3 3.5 7l4 4M12 3 8 7l4 4" />
  </svg>
)
const GridIcon = () => (
  <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <rect x="3" y="3" width="7" height="7" rx="1.5" /><rect x="14" y="3" width="7" height="7" rx="1.5" />
    <rect x="3" y="14" width="7" height="7" rx="1.5" /><rect x="14" y="14" width="7" height="7" rx="1.5" />
  </svg>
)
const LogoutIcon = () => (
  <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4M16 17l5-5-5-5M21 12H9" />
  </svg>
)

export default function Sidebar({ user, boards, onLogout, onNewBoard, collapsed, onToggle }) {
  const r = collapsed // icon-only rail (same DOM, CSS animates between the two)
  return (
    <>
      <aside className={'sidebar' + (r ? ' rail' : '')}>
        <Tip label="One Piece" off={!r}>
          <NavLink to="/" className="brand-row" aria-label="One Piece home"><Logo size={30} wordmark /></NavLink>
        </Tip>
        <Tip label="Dashboard" off={!r}>
          <NavLink to="/" end className="nav-item" aria-label="Dashboard">
            <GridIcon />
            <span className="grow">Dashboard</span>
          </NavLink>
        </Tip>
        <div className="side-head">
          <span className="side-label">Boards</span>
          <Tip label="New Board" off={!r}>
            <button className="side-plus" aria-label="New Board" onClick={onNewBoard} />
          </Tip>
        </div>
        {boards.map((b, i) => (
          <Tip key={b.id} label={`${b.name} · ${totalCards(b)}`} off={!r}>
            <NavLink to={`/boards/${b.id}`} className="nav-item board-link" aria-label={b.name}>
              <span className="board-tile" style={{ background: SWATCH[i % SWATCH.length] }}>{b.name.charAt(0).toUpperCase()}</span>
              <span className="grow">{b.name}</span>
              <span className="count">{totalCards(b)}</span>
            </NavLink>
          </Tip>
        ))}
        <div className="side-spacer" />
        <UsageCard rail={r} />
        <div className="side-user">
          <Tip label={user} off={!r}>
            <span className="avatar">{user.charAt(0).toUpperCase()}</span>
          </Tip>
          <span className="grow user-name">{user}</span>
          <Tip label="Log Out" off={!r}>
            <button className="link logout" aria-label="Log Out" onClick={onLogout}>
              <span className="only-rail"><LogoutIcon /></span>
              <span className="only-full">Log Out</span>
            </button>
          </Tip>
        </div>
      </aside>
      <Tip label={(r ? 'Show sidebar' : 'Hide sidebar') + (/Mac/.test(navigator.platform) ? '  ⌘B' : '  Ctrl+B')}>
        <button type="button" className={'side-collapse' + (r ? ' rail' : '')} aria-label={r ? 'Show sidebar' : 'Hide sidebar'} onClick={onToggle}><Chevrons flip={r} /></button>
      </Tip>
    </>
  )
}
