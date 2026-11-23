import { useState } from 'react'
import { createPortal } from 'react-dom'

// Custom tooltip (replaces the browser `title`). Rendered in a portal because the sidebar's
// backdrop-filter would otherwise trap `position: fixed` inside it.
export default function Tip({ label, off = false, side = 'right', children }) {
  const [pos, setPos] = useState(null)
  const show = (e) => {
    const r = e.currentTarget.getBoundingClientRect()
    setPos(side === 'top' ? { x: r.left + r.width / 2, y: r.top - 10 } : { x: r.right + 12, y: r.top + r.height / 2 })
  }
  const hide = () => setPos(null)
  return (
    <span className="tip-wrap" onMouseEnter={show} onMouseLeave={hide} onMouseDown={hide} onFocus={show} onBlur={hide}>
      {children}
      {!off && pos && createPortal(
        <span className={'tip' + (side === 'top' ? ' tip-top' : '')} role="tooltip" style={{ left: pos.x, top: pos.y }}>{label}</span>,
        document.body,
      )}
    </span>
  )
}
