const LABEL = {
  description: 'Clear description, no contradictions',
  cases: 'Every case (A, B, C…) has defined behaviour',
  dod: 'Definition of Done is concrete and testable',
  edge: 'Error and edge cases covered',
  scope: 'One focused change, small enough',
  flows: 'Business Flow matches the description',
}
const MARK = { ok: ['✓', 'ok'], gap: ['!', 'gap'], na: ['–', 'na'] }

// The evidence behind a verdict: one line per criterion with Claude's short reason.
export default function ReviewChecklist({ checks }) {
  if (!checks?.length) return null
  return (
    <ul className="review-checks">
      {checks.map((c) => (
        <li key={c.key} className={'rc-' + c.status} title={c.note || undefined}>
          <span className="rc-mark" aria-hidden="true">{MARK[c.status]?.[0]}</span>
          <span className="rc-label">{LABEL[c.key] ?? c.key}</span>
          {c.note && <span className="rc-note">{c.note}</span>}
        </li>
      ))}
    </ul>
  )
}
