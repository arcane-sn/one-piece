export function FolderIcon({ size = 12 }) {
  return (
    <svg width={size} height={size} viewBox="0 0 16 16" aria-hidden="true" style={{ flex: 'none' }}>
      <path d="M1.5 4.5a1 1 0 0 1 1-1h3.2l1.5 1.7h6.3a1 1 0 0 1 1 1v6.3a1 1 0 0 1-1 1h-11a1 1 0 0 1-1-1Z" fill="currentColor" />
    </svg>
  )
}

// One Piece mark (Claude Design, direction A): a jigsaw piece on a warm tile.
// `glyph` is the tile-less version for empty states.
export default function Logo({ size = 40, wordmark = true, glyph = false }) {
  if (glyph) {
    return (
      <svg width={size} height={size} viewBox="0 0 64 64" role="img" aria-label="One Piece">
        <rect x="8" y="14" width="40" height="40" rx="6" fill="#D65A42" />
        <circle cx="28" cy="12" r="8" fill="#D65A42" />
        <circle cx="52" cy="34" r="8" fill="#EA9259" />
        <circle cx="8" cy="34" r="7" fill="#FFEAD3" />
      </svg>
    )
  }
  return (
    <span className="logo">
      <svg width={size} height={size} viewBox="0 0 64 64" role="img" aria-label="One Piece">
        <defs>
          <linearGradient id="op-logo-g" x1="0" y1="0" x2="1" y2="1">
            <stop offset="0" stopColor="#F2A56F" />
            <stop offset="1" stopColor="#D65A42" />
          </linearGradient>
        </defs>
        <rect width="64" height="64" rx="15" fill="url(#op-logo-g)" />
        <rect x="16" y="22" width="32" height="30" rx="4" fill="#fff" />
        <circle cx="32" cy="18" r="6.5" fill="#fff" />
        <circle cx="50" cy="37" r="6.5" fill="#fff" />
        <circle cx="16" cy="37" r="6" fill="#E5805A" />
      </svg>
      {wordmark && <span className="logo-word" style={{ fontSize: Math.round(size * 0.64) }}>One Piece</span>}
    </span>
  )
}
