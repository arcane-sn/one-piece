import { useEffect, useState } from 'react'

const PHRASES = ['Thinking', 'Reading the context', 'Working it out', 'Putting it together', 'Almost there']

// Animated "Claude is working" label: a shimmering word with bouncing dots, a phrase that changes every few
// seconds and (in chats) the elapsed time, so a long turn never looks stuck. `inline` = compact, for buttons.
export default function Thinking({ inline = false }) {
  const [s, setS] = useState(0)
  useEffect(() => {
    const t = setInterval(() => setS((n) => n + 1), 1000)
    return () => clearInterval(t)
  }, [])
  const word = inline ? 'Thinking' : PHRASES[Math.min(Math.floor(s / 6), PHRASES.length - 1)]
  return (
    <span className="thinking" role="status">
      <span className="thinking-word">{word}</span>
      <span className="thinking-dots" aria-hidden="true"><i /><i /><i /></span>
      {!inline && s >= 3 && <span className="thinking-time">{s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`}</span>}
    </span>
  )
}
