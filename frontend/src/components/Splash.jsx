// Loading screen: the logo's jigsaw pieces slide together, then the name appears.
// `minMs` keeps it on screen long enough for the animation to finish.
import { useEffect, useState } from 'react'

const PIECES = [ // quadrant clip, white/aqua fill, start offset (the pieces come in from the corners)
  { clip: 'M0 0H32V32H0Z', fill: '#fff', from: '-16px,-16px' },
  { clip: 'M32 0H64V32H32Z', fill: '#fff', from: '16px,-16px' },
  { clip: 'M0 32H32V64H0Z', fill: '#fff', from: '-16px,16px' },
  { clip: 'M32 32H64V64H32Z', fill: '#B0E3E3', from: '16px,16px' },
]
const GAPS = 'M32 11V19a3.6 3.6 0 1 1 0 6V32M11 32H19a3.6 3.6 0 1 0 6 0H32M32 53V45a3.6 3.6 0 1 1 0-6V32H39a3.6 3.6 0 1 1 6 0H53'

export function Splash() {
  return (
    <div className="splash" role="status" aria-label="Loading One Piece">
      <svg width="96" height="96" viewBox="0 0 64 64" aria-hidden="true">
        <defs>
          <linearGradient id="sp-g" x1="0" y1="0" x2="64" y2="64" gradientUnits="userSpaceOnUse">
            <stop offset="0" stopColor="#F2A56F" /><stop offset="1" stopColor="#D65A42" />
          </linearGradient>
          <mask id="sp-m" maskUnits="userSpaceOnUse" x="0" y="0" width="64" height="64">
            <rect width="64" height="64" fill="#fff" />
            <path d={GAPS} fill="none" stroke="#000" strokeWidth="2.6" />
          </mask>
          {PIECES.map((p, i) => <clipPath key={i} id={`sp-c${i}`}><path d={p.clip} /></clipPath>)}
        </defs>
        <rect className="splash-tile" width="64" height="64" rx="15" fill="url(#sp-g)" />
        <g mask="url(#sp-m)">
          {PIECES.map((p, i) => (
            <g key={i} clipPath={`url(#sp-c${i})`}>
              <g className="splash-piece" style={{ '--from': p.from, animationDelay: `${i * 0.12}s` }}>
                <rect x="12" y="12" width="40" height="40" rx="7" fill={p.fill} />
              </g>
            </g>
          ))}
        </g>
      </svg>
      <div className="splash-word"><span style={{ color: '#3B2417' }}>One</span> <span style={{ color: '#B8452E' }}>Piece</span></div>
    </div>
  )
}

// Shows the splash for at least `minMs` after `active` turns true (used after login).
export function useSplash(active, minMs = 1700) {
  const [done, setDone] = useState(false)
  useEffect(() => {
    if (!active) { setDone(false); return }
    const t = setTimeout(() => setDone(true), minMs)
    return () => clearTimeout(t)
  }, [active, minMs])
  return active && !done
}
