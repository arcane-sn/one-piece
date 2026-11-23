import { useJobs } from '../lib/jobs'
import { KIND, useActivity } from '../lib/activity'

// Markdown -> one short plain-text line for the card preview.
const plain = (md) => md.replace(/```[\s\S]*?```/g, ' ').replace(/!\[[^\]]*\]\([^)]*\)/g, ' ').replace(/[#>*_`~\\[\]()!-]+/g, ' ').replace(/\s+/g, ' ').trim()

// A draggable kanban card; click opens the ticket modal.
export default function CardItem({ card, onOpen, onDropOn }) {
  const generating = useJobs().busyCards.has(card.id)
  const work = useActivity().forCard(card.id)[0]
  return (
    <div
      className={'kcard' + (work ? ' working' : '')}
      draggable
      onDragStart={(e) => e.dataTransfer.setData('text/plain', String(card.id))}
      onDragOver={(e) => e.preventDefault()}
      onDrop={onDropOn}
      onClick={() => onOpen(card)}
    >
      <div className="kcard-title">{card.ident && <span className="ident">{card.ident}</span>}{card.title}{generating && <span className="spinner" title="Claude is generating" aria-label="Claude is generating" style={{ marginLeft: 6 }} />}</div>
      {work && <span className="working-chip"><span className="spinner" aria-hidden="true" /> {KIND[work.kind]} · Claude is working</span>}
      {card.branch && <span className="branch-tag" title={'Worked on branch ' + card.branch}>⑂ {card.branch}</span>}
      {card.qaTotal > 0 && <span className={'branch-tag' + (card.qaRan < card.qaTotal ? ' ev-warn' : '')} title="QA test cases executed in the browser / passed">QA {card.qaRan}/{card.qaTotal} run · {card.qaPassed} passed</span>}
      {plain(card.description) && <div className="kcard-desc">{plain(card.description)}</div>}
      {(card.deadline || card.approved) && (
        <div className="kcard-foot">
          {card.deadline ? (
            <span className="deadline-chip" title="Deadline">
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                <rect x="3" y="5" width="18" height="16" rx="3" /><path d="M3 10h18M8 3v4M16 3v4" />
              </svg>
              {card.deadline}
            </span>
          ) : <span />}
          {card.approved && <span className="approved-chip">Approved ✓</span>}
        </div>
      )}
    </div>
  )
}
