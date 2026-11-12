import { useEffect, useRef, useState } from 'react'
import { api } from '../api'
import MentionTextarea from './MentionTextarea'

// Small modal that asks Claude (`claude -p`, via the server) to draft a ticket
// description from a short idea. `onUse(markdown)` hands the result back.
export default function PromptModal({ boardId, title, description, onUse, onClose }) {
  const [prompt, setPrompt] = useState('')
  const [result, setResult] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const alive = useRef(true)

  useEffect(() => {
    alive.current = true
    const onKey = (e) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => {
      alive.current = false
      window.removeEventListener('keydown', onKey)
    }
  }, [onClose])

  async function generate(e) {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      const r = await api('/api/generate-description', { method: 'POST', body: { prompt, title, description, boardId: Number(boardId) } })
      if (alive.current) setResult(r.description)
    } catch (err) {
      if (alive.current) setError(err.message)
    } finally {
      if (alive.current) setBusy(false)
    }
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
          <MentionTextarea boardId={boardId} autoFocus rows={4} maxLength={2000} placeholder="e.g. Let ops teams save filter sets on the orders page and reopen them in one click. Type @ to point Claude at a file or folder."
            value={prompt} onChange={setPrompt} />
        </label>

        {busy && <p className="muted prompt-status"><span className="spinner" /> Claude is writing…</p>}
        {error && <p className="error">{error}</p>}
        {result && !busy && (
          <div className="field">
            <span>Draft</span>
            <pre className="prompt-result">{result}</pre>
            {description.trim() && <p className="hint">Using this draft replaces the current description.</p>}
          </div>
        )}

        <div className="modal-actions">
          <button type="button" onClick={onClose}>Cancel</button>
          <button type={result ? 'button' : 'submit'} className={result ? '' : 'primary'} disabled={busy || !prompt.trim()}
            onClick={result ? generate : undefined}>
            {result ? 'Regenerate' : 'Generate'}
          </button>
          {result && !busy && (
            <button type="button" className="primary" onClick={() => onUse(result)}>Use This Draft</button>
          )}
        </div>
      </form>
    </div>
  )
}
