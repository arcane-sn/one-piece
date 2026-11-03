// One Piece mark: a single jigsaw piece on a warm gradient tile, plus optional wordmark.
export default function Logo({ size = 40, wordmark = true }) {
  return (
    <span className="logo">
      <svg width={size} height={size} viewBox="0 0 40 40" role="img" aria-label="One Piece">
        <defs>
          <linearGradient id="op-logo-g" x1="0" y1="0" x2="1" y2="1">
            <stop offset="0" stopColor="#f4a66b" />
            <stop offset="1" stopColor="#d65a42" />
          </linearGradient>
        </defs>
        <rect width="40" height="40" rx="11" fill="url(#op-logo-g)" />
        <path transform="translate(20 20) scale(1.3) translate(-21.5 -21)" d="M11 14h7.5a3 3 0 1 1 6 0H29v7.5a3 3 0 1 0 0 6V31H11z" fill="#fff" fillOpacity=".95" />
      </svg>
      {wordmark && (
        <span className="logo-word">
          One<b>Piece</b>
        </span>
      )}
    </span>
  )
}
