import { useId } from 'react'

export function FolderIcon({ size = 12 }) {
  return (
    <svg width={size} height={size} viewBox="0 0 16 16" aria-hidden="true" style={{ flex: 'none' }}>
      <path d="M1.5 4.5a1 1 0 0 1 1-1h3.2l1.5 1.7h6.3a1 1 0 0 1 1 1v6.3a1 1 0 0 1-1 1h-11a1 1 0 0 1-1-1Z" fill="currentColor" />
    </svg>
  )
}


/**
 * Logo One Piece — "Keping Terakhir".
 *
 * variant:
 *  - "tile"  (default) tile gradient oranye→merah, kepingan putih + aqua
 *  - "glyph" tanpa tile, untuk empty state
 *
 * Ukuran <= 24px otomatis memakai versi favicon (tanpa knob, celah lebih tebal).
 */
function Mark({ size = 32, variant = 'tile', className, title = 'One Piece' }) {
  const uid = useId().replace(/:/g, '')
  const g = `op-g-${uid}`
  const m = `op-m-${uid}`
  const simple = size <= 24 && variant === 'tile'

  const gaps = simple
    ? 'M32 10V54M10 32H54'
    : 'M32 11V19a3.6 3.6 0 1 1 0 6V32M11 32H19a3.6 3.6 0 1 0 6 0H32M32 53V45a3.6 3.6 0 1 1 0-6V32H39a3.6 3.6 0 1 1 6 0H53'
  const gapWidth = simple ? 4.5 : 2.6
  const body = simple ? { x: 11, y: 11, s: 42, r: 8 } : { x: 12, y: 12, s: 40, r: 7 }
  const last = simple
    ? 'M32 32H53V45a8 8 0 0 1-8 8H32Z'
    : 'M32 32H39a3.6 3.6 0 1 1 6 0H52V45a7 7 0 0 1-7 7H32V45a3.6 3.6 0 1 1 0-6Z'

  if (variant === 'glyph') {
    return (
      <svg width={size} height={size} viewBox="10 10 44 44" className={className} role="img" aria-label={title}>
        <defs>
          <linearGradient id={g} x1="12" y1="12" x2="52" y2="52" gradientUnits="userSpaceOnUse">
            <stop offset="0" stopColor="#F2A56F" />
            <stop offset="1" stopColor="#D65A42" />
          </linearGradient>
          <mask id={m} maskUnits="userSpaceOnUse" x="10" y="10" width="44" height="44">
            <rect x="10" y="10" width="44" height="44" fill="#fff" />
            <path d={gaps} fill="none" stroke="#000" strokeWidth={gapWidth} />
          </mask>
        </defs>
        <g mask={`url(#${m})`}>
          <rect x="12" y="12" width="40" height="40" rx="7" fill={`url(#${g})`} />
          <path d={last} fill="#B0E3E3" />
        </g>
      </svg>
    )
  }

  return (
    <svg width={size} height={size} viewBox="0 0 64 64" className={className} role="img" aria-label={title}>
      <defs>
        <linearGradient id={g} x1="0" y1="0" x2="64" y2="64" gradientUnits="userSpaceOnUse">
          <stop offset="0" stopColor="#F2A56F" />
          <stop offset="1" stopColor="#D65A42" />
        </linearGradient>
        <mask id={m} maskUnits="userSpaceOnUse" x="0" y="0" width="64" height="64">
          <rect width="64" height="64" fill="#fff" />
          <path d={gaps} fill="none" stroke="#000" strokeWidth={gapWidth} />
        </mask>
      </defs>
      <rect width="64" height="64" rx="15" fill={`url(#${g})`} />
      <g mask={`url(#${m})`}>
        <rect x={body.x} y={body.y} width={body.s} height={body.s} rx={body.r} fill="#fff" />
        <path d={last} fill="#B0E3E3" />
      </g>
    </svg>
  )
}

// `wordmark` adds the "One Piece" text beside the mark (Plus Jakarta Sans, from the app CSS).
export default function Logo({ wordmark = false, ...props }) {
  if (!wordmark) return <Mark {...props} />
  const size = props.size ?? 32
  return (
    <span className="logo">
      <Mark {...props} />
      <span className="logo-word" style={{ fontSize: Math.round(size * 0.64) }}>
        <span style={{ color: '#3B2417' }}>One</span> <span style={{ color: '#B8452E' }}>Piece</span>
      </span>
    </span>
  )
}
