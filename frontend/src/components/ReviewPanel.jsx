import { useCallback, useEffect, useState } from 'react'
import { api } from '../api'

// Latest Claude review of a card; polls while a review is running.
// review: undefined = loading, null = never reviewed.
export function useReview(cardId) {
  const [review, setReview] = useState(undefined)
  const refresh = useCallback(async () => {
    if (!cardId) return
    try {
      setReview(await api(`/api/cards/${cardId}/review`))
    } catch {
      // keep what we have; the next poll or save refreshes it
    }
  }, [cardId])

  useEffect(() => {
    setReview(undefined)
    refresh()
  }, [refresh])

  useEffect(() => {
    if (review?.status !== 'running') return
    const t = setInterval(refresh, 1500)
    return () => clearInterval(t)
  }, [review?.status, refresh])

  return { review, refresh }
}

const isOutdated = (review, card) => !!review && !!card && review.contentVersion !== card.contentVersion

// Right-hand panel: review status, the button that opens the Claude Review chat, and the Approve control.
export function ReviewSide({ card, review, unsaved, busy, reviewing, onOpenReview, onApprove }) {
  if (!card) {
    return (
      <div className="field review-side">
        <span>Claude Review</span>
        <p className="hint">Create the ticket, then discuss it with Claude until it passes.</p>
        <button type="button" disabled>Approve</button>
      </div>
    )
  }

  const outdated = isOutdated(review, card)
  const passed = review?.status === 'done' && review.verdict === 'pass' && !outdated
  const canApprove = passed && !unsaved && !card.approved
  const inBusiness = card.status === 'business'

  let badge = null
  let hint = ''
  if (review === undefined) hint = 'Loading…'
  else if (reviewing) { badge = ['running', 'Reviewing…']; hint = 'Claude is working on this ticket.' }
  else if (review === null) { badge = ['idle', 'Not Reviewed']; hint = 'Open the review and Claude helps you get this ticket approved.' }
  else if (outdated) { badge = ['idle', 'Outdated']; hint = 'The ticket changed since the last review. Discuss it again.' }
  else if (passed) { badge = ['pass', 'Passed']; hint = card.approved ? '' : 'Ready to approve.' }
  else { badge = ['work', 'Needs Work']; hint = review?.summary || 'Discuss it with Claude to fix what is open.' }
  if (unsaved && !reviewing) hint = 'Save your changes before opening the review.'

  return (
    <div className="field review-side">
      <span>Claude Review</span>
      <div className="review-head">
        {badge && (
          <span className={'review-badge ' + badge[0]}>
            {badge[0] === 'running' && <span className="spinner" />}
            {badge[1]}
          </span>
        )}
      </div>
      <button type="button" className="ai-btn review-open-btn" disabled={unsaved || !inBusiness} onClick={onOpenReview}
        title={inBusiness ? 'Review and improve this ticket with Claude' : 'Claude Review is available while the ticket is in Business'}>
        {reviewing ? <><span className="spinner" aria-hidden="true" /> Open Review</> : <><span aria-hidden="true">✦</span> Review With Claude</>}
      </button>
      {card.approved ? (
        <>
          <button type="button" className="approved-btn" disabled>Approved ✓</button>
          <button type="button" className="link" disabled={busy} onClick={() => onApprove(false)}>Revoke Approval</button>
        </>
      ) : (
        <button type="button" disabled={!canApprove || busy} onClick={() => onApprove(true)}>Approve</button>
      )}
      {hint && <p className="hint">{hint}</p>}
    </div>
  )
}
