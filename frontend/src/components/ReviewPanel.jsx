import { useCallback, useEffect, useRef, useState } from 'react'
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

  const start = useCallback(async () => {
    await api(`/api/cards/${cardId}/review`, { method: 'POST', body: {} })
    refresh()
  }, [cardId, refresh])

  return { review, refresh, start }
}

const isOutdated = (review, card) => !!review && !!card && review.contentVersion !== card.contentVersion

// Right-hand panel: review status, live log and the Approve control.
export function ReviewSide({ card, review, unsaved, busy, onStart, onApprove }) {
  const logRef = useRef(null)
  useEffect(() => {
    if (logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight
  }, [review?.log?.length])

  if (!card) {
    return (
      <div className="field review-side">
        <span>Claude Review</span>
        <p className="hint">Create the ticket and Claude reviews it automatically.</p>
        <button type="button" disabled>Approve</button>
      </div>
    )
  }

  const outdated = isOutdated(review, card)
  const passed = review?.status === 'done' && review.verdict === 'pass' && !outdated
  const canApprove = passed && !unsaved && !card.approved

  let badge = null
  let hint = ''
  if (review === undefined) hint = 'Loading…'
  else if (review === null) { badge = ['idle', 'Not Reviewed']; hint = 'Start a review to unlock approval.' }
  else if (review.status === 'running') { badge = ['running', 'Reviewing…']; hint = 'Claude is reviewing this ticket.' }
  else if (review.status === 'failed') { badge = ['failed', 'Failed']; hint = review.error }
  else if (outdated) { badge = ['idle', 'Outdated']; hint = 'The ticket changed. A new review is on its way.' }
  else if (passed) { badge = ['pass', 'Passed']; hint = card.approved ? '' : 'Ready to approve.' }
  else { badge = ['work', 'Needs Work']; hint = 'Fix what Claude listed, then save to review again.' }
  if (unsaved && review?.status !== 'running') hint = 'Save your changes to get a new review.'

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
        {(review === null || review?.status === 'failed') && (
          <button type="button" className="link" disabled={busy} onClick={onStart}>
            {review === null ? 'Start Review' : 'Retry'}
          </button>
        )}
      </div>
      {review?.log?.length > 0 && (
        <ul className="review-log" ref={logRef} aria-label="Claude log">
          {review.log.map((l, i) => <li key={i}>{l}</li>)}
        </ul>
      )}
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

// Claude's verdict as a comment under the description.
export function ReviewComment({ card, review }) {
  if (!review || review.status !== 'done' || isOutdated(review, card)) return null
  const pass = review.verdict === 'pass'
  return (
    <div className={'review-comment ' + (pass ? 'pass' : 'work')}>
      <div className="review-comment-head">
        <span aria-hidden="true">✦</span> Claude’s Review
        <span className={'review-badge ' + (pass ? 'pass' : 'work')}>{pass ? 'Passed' : 'Needs Work'}</span>
      </div>
      {review.summary && <p>{review.summary}</p>}
      {review.missing.length > 0 && (
        <ul>{review.missing.map((m, i) => <li key={i}>{m}</li>)}</ul>
      )}
    </div>
  )
}
