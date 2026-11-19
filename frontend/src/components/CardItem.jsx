import { useJobs } from '../lib/jobs'

// Markdown -> one short plain-text line for the card preview.
const plain = (md) => md.replace(/```[\s\S]*?```/g, ' ').replace(/!\[[^\]]*\]\([^)]*\)/g, ' ').replace(/[#>*_`~\\[\]()!-]+/g, ' ').replace(/\s+/g, ' ').trim()

// A draggable kanban card; click opens the ticket modal.
export default function CardItem({ card, onOpen, onDropOn }) {
  const generating = useJobs().busyCards.has(card.id)
  const working = generating || card.running // Claude job, or an Agent/QA task being run
  return (
    <div
      className={`kcard${working ? ' running' : ''}`}
      draggable
      onDragStart={(e) => e.dataTransfer.setData('text/plain', String(card.id))}
      onDragOver={(e) => e.preventDefault()}
      onDrop={onDropOn}
      onClick={() => onOpen(card)}
    >
      <div className="kcard-title">{card.ident && <span className="ident">{card.ident}</span>}{card.title}{working && <span className="spinner" title="Claude is working" aria-label="Claude is working" />}</div>
      {card.approved && <span className="approved-chip">Approved ✓</span>}
      {plain(card.description) && <div className="kcard-desc">{plain(card.description)}</div>}
      {card.deadline && (
        <span className="deadline-chip" title="Deadline">
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <rect x="3" y="5" width="18" height="16" rx="3" /><path d="M3 10h18M8 3v4M16 3v4" />
          </svg>
          {card.deadline}
        </span>
      )}
    </div>
  )
}
