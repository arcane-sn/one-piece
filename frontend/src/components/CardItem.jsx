// A draggable kanban card; click opens the ticket modal.
export default function CardItem({ card, onOpen, onDropOn }) {
  return (
    <div
      className="kcard"
      draggable
      onDragStart={(e) => e.dataTransfer.setData('text/plain', String(card.id))}
      onDragOver={(e) => e.preventDefault()}
      onDrop={onDropOn}
      onClick={() => onOpen(card)}
    >
      <div className="kcard-title">{card.title}</div>
      {card.description && <div className="kcard-desc">{card.description}</div>}
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
