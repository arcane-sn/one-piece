import { useLayoutEffect, useRef } from 'react'
import { useJobs } from '../lib/jobs'
import { KIND, useActivity } from '../lib/activity'

// Markdown -> one short plain-text line for the card preview.
const plain = (md) => md.replace(/```[\s\S]*?```/g, ' ').replace(/!\[[^\]]*\]\([^)]*\)/g, ' ').replace(/[#>*_`~\\[\]()!-]+/g, ' ').replace(/\s+/g, ' ').trim()

// Where each card last was on screen, captured as its element unmounts. A card that moves to another column is a
// new element there, so the new one reads this to fly in from where the old one was (FLIP).
const lastSeen = new Map()

function landFrom(el, id) {
  if (!el || el.closest('.columns')?.classList.contains('intro') || matchMedia('(prefers-reduced-motion: reduce)').matches) return
  const prev = lastSeen.get(id)
  lastSeen.delete(id)
  const to = el.getBoundingClientRect()
  const moved = prev && performance.now() - prev.at < 600
  const dx = moved ? prev.left - to.left : 0
  const dy = moved ? prev.top - to.top : -12 // a brand-new card just settles in
  const dist = Math.hypot(dx, dy)
  el.style.position = 'relative'
  el.style.zIndex = '30'
  const anim = el.animate(
    [
      { transform: `translate(${dx}px, ${dy}px) scale(1.04) rotate(${Math.max(-2, Math.min(2, dx / 120))}deg)`, boxShadow: '0 24px 36px -12px rgba(120, 60, 20, 0.5)' },
      { transform: 'translate(0, 0) scale(0.99)', boxShadow: '0 8px 14px -8px rgba(120, 60, 20, 0.3)', offset: 0.8 },
      { transform: 'none' },
    ],
    { duration: Math.min(520, 240 + dist * 0.5), easing: 'cubic-bezier(0.25, 0.8, 0.3, 1)' },
  )
  anim.onfinish = anim.oncancel = () => { el.style.position = ''; el.style.zIndex = '' }
}

// A draggable kanban card; click opens the ticket modal.
export default function CardItem({ card, onOpen, onDropOn }) {
  const generating = useJobs().busyCards.has(card.id)
  const work = useActivity().forCard(card.id)[0]
  const ref = useRef(null)
  useLayoutEffect(() => {
    const el = ref.current
    landFrom(el, card.id)
    return () => { const r = el?.getBoundingClientRect(); if (r && r.width > 0) lastSeen.set(card.id, { left: r.left, top: r.top, at: performance.now() }) }
  }, [card.id])
  return (
    <div
      ref={ref}
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
