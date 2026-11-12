import { useEffect, useRef, useState } from 'react'
import { jobError, jobResult, useJob } from '../lib/jobs'
import MentionTextarea from './MentionTextarea'

// Small modal that asks Claude (`claude -p`, via the server) to draft a ticket
// description from a short idea. `onUse(markdown)` hands the result back.
export default function PromptModal({ boardId, cardId, title, description, onUse, onClose }) {
  // The job lives on the server: closing this modal does not stop it, and reopening shows its state.
  const { job, start, consume } = useJob(cardId ? 'card' : 'board', cardId || Number(boardId), 'description')
  const [prompt, setPrompt] = useState('')
  const [startError, setStartError] = useState('')
  const busy = job?.status === 'running'
  const result = jobResult(job)?.description ?? ''
  const error = startError || jobError(job)
  const filled = useRef(false)
  useEffect(() => { // restore the idea that was sent
    if (filled.current || !job?.request) return
    filled.current = true
    try { setPrompt((p) => p || JSON.parse(job.request).prompt || '') } catch { /* keep empty */ }
  }, [job])

  useEffect(() => {
    const onKey = (e) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  async function generate(e) {
    e.preventDefault()
    setStartError('')
    try {
      await start('/api/generate-description', { prompt, title, description, boardId: Number(boardId), cardId: cardId || undefined })
    } catch (err) {
      setStartError(err.message)
    }
  }

  async function use() {
    await consume()
    onUse(result)
  }

  return (
    <div className="modal-backdrop prompt-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <form className="modal prompt-modal" role="dialog" aria-modal="true" aria-label="Generate Description" onSubmit={generate}>
        <div className="modal-head">
          <h2>Generate Description</h2>
          <p>Describe the idea in a sentence or two. Type @ to pick a file or folder for Claude to read. Claude drafts the ticket for you to review.</p>
        </div>
        <label className="field">
          <span>Your Idea</span>
          <MentionTextarea boardId={boardId} autoFocus autoGrow rows={4} maxLength={20000} placeholder="e.g. Let ops teams save filter sets on the orders page and reopen them in one click. Type @ to point Claude at a file or folder."
            value={prompt} onChange={setPrompt} />
        </label>

        {busy && <p className="muted prompt-status"><span className="spinner" /> Claude is writing… You can close this and come back.</p>}
        {error && <p className="error">{error}</p>}
        {result && !busy && (
          <div className="field">
            <span>Draft</span>
            <pre className="prompt-result">{result}</pre>
            {description.trim() && <p className="hint">Using this draft replaces the current description.</p>}
          </div>
        )}

        <div className="modal-actions">
          <button type="button" onClick={onClose}>Close</button>
          <button type={result ? 'button' : 'submit'} className={result ? '' : 'primary'} disabled={busy || !prompt.trim()}
            onClick={result ? generate : undefined}>
            {busy ? <><span className="spinner" aria-hidden="true" /> Thinking…</> : result ? 'Regenerate' : 'Generate'}
          </button>
          {result && !busy && (
            <button type="button" className="primary" onClick={use}>Use This Draft</button>
          )}
        </div>
      </form>
    </div>
  )
}
