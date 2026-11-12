import { useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { api } from '../api'
import { build, parse } from '../lib/swimlanes'
import { jobError, jobResult, useJob } from '../lib/jobs'

const HINT = {
  business: 'e.g. A customer pays, support sees it, and a wrong payment is refunded.',
  technical: 'e.g. The form calls the API, the API checks the password and sets the session cookie.',
}

const isRetry = (job) => { try { return !!JSON.parse(job.request).retry } catch { return false } }

// How many lines of the text the swimlane drawing could not read (null = it cannot be drawn at all).
function problems(source) {
  try {
    const P = parse(source)
    build(P)
    return P.bad ?? []
  } catch {
    return null
  }
}

// Prompt -> Claude drafts a swimlane flow -> preview with the real drawing -> save.
// If the drawing cannot read some lines, Claude gets one automatic chance to fix them.
export default function GenerateFlowModal({ kind, id, createPath, onSaved, onClose }) {
  // The job lives on the server: closing this modal does not stop it, and reopening shows its state.
  const { job, start, consume } = useJob(kind === 'technical' ? 'item' : 'card', id, 'flow')
  const [prompt, setPrompt] = useState('')
  const [title, setTitle] = useState('')
  const [source, setSource] = useState('')
  const [saving, setSaving] = useState(false)
  const [startError, setStartError] = useState('')
  const [fontsTick, setFontsTick] = useState(0)
  const busy = job?.status === 'running'
  const retried = useRef(new Set())
  const filled = useRef(0)

  useEffect(() => { document.fonts?.ready.then(() => setFontsTick(1)) }, [])
  useEffect(() => {
    const onKey = (e) => e.key === 'Escape' && !saving && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })
  useEffect(() => { // restore the request that was sent
    if (!job?.request || prompt) return
    try { setPrompt(JSON.parse(job.request).prompt || '') } catch { /* keep empty */ }
  }, [job]) // eslint-disable-line react-hooks/exhaustive-deps

  const result = jobResult(job)
  useEffect(() => { // a finished job fills the preview
    if (!result || filled.current === job.id) return
    filled.current = job.id
    setTitle((t) => result.title || t)
    setSource(result.source)
    const p = problems(result.source)
    if ((p === null || p.length > 0) && !isRetry(job) && !retried.current.has(job.id)) {
      retried.current.add(job.id) // one automatic retry, telling Claude which lines failed
      start('/api/flows/generate', { kind, id, prompt: JSON.parse(job.request || '{}').prompt || prompt, previous: result.source, problems: p ?? [] }).catch(() => {})
    }
  }, [result, job]) // eslint-disable-line react-hooks/exhaustive-deps

  const error = startError || jobError(job)
  const bad = useMemo(() => (source ? problems(source) : []), [source])
  const svg = useMemo(() => {
    if (!source || bad === null) return ''
    try { return build(parse(source)).svg } catch { return '' }
  }, [source, bad, fontsTick])

  async function generate(e) {
    e?.preventDefault()
    e?.stopPropagation()
    setStartError('')
    try {
      await start('/api/flows/generate', { kind, id, prompt })
    } catch (err) {
      setStartError(err.message)
    }
  }

  async function save() {
    setSaving(true)
    setStartError('')
    try {
      const saved = await api(createPath, { method: 'POST', body: { title: title.trim(), source } })
      await consume()
      onSaved(saved)
    } catch (err) {
      setStartError(err.message)
      setSaving(false)
    }
  }

  const ready = !!source && !busy
  return createPortal(
    <div className="modal-backdrop flow-backdrop" onMouseDown={(e) => e.target === e.currentTarget && !saving && onClose()}>
      <form className="modal gen-flow-modal" role="dialog" aria-modal="true" aria-label="Generate Flow" onSubmit={generate}>
        <div className="ticket-head">
          <h2>Generate {kind === 'technical' ? 'Technical' : 'Business'} Flow</h2>
          <button type="button" className="icon-btn" aria-label="Close" onClick={onClose} disabled={saving}>×</button>
        </div>
        <label className="field">
          <span>What Should The Flow Show?</span>
          <textarea autoFocus rows={6} maxLength={20000} placeholder={HINT[kind]} value={prompt} onChange={(e) => setPrompt(e.target.value)} />
        </label>
        {busy && <p className="muted prompt-status"><span className="spinner" /> Claude is drawing the flow… You can close this and come back.</p>}
        {error && <p className="error">{error}</p>}
        {ready && (
          <>
            <label className="field">
              <span>Flow Name</span>
              <input maxLength={200} value={title} onChange={(e) => setTitle(e.target.value)} />
            </label>
            <div className="field">
              <span>Preview{bad?.length > 0 && <em className="flow-warn"> · {bad.length} line{bad.length > 1 ? 's' : ''} not understood</em>}</span>
              <div className="sl-out gen-preview">
                <div className="sl-scroll">
                  {svg
                    ? <div className="sl-svg" style={{ zoom: 0.85 }} dangerouslySetInnerHTML={{ __html: svg }} />
                    : <p className="sl-empty">This text cannot be drawn. Regenerate, or save and fix it in the editor.</p>}
                </div>
              </div>
              <p className="hint">You can edit the text after saving: open the flow from the list.</p>
            </div>
          </>
        )}
        <div className="modal-actions">
          <button type="button" onClick={onClose} disabled={saving}>Close</button>
          <button type="submit" className={ready ? '' : 'primary'} disabled={busy || saving || !prompt.trim()}>{busy ? <><span className="spinner" aria-hidden="true" /> Thinking…</> : ready ? 'Regenerate' : 'Generate'}</button>
          {ready && (
            <button type="button" className="primary" disabled={saving || !title.trim() || !svg} onClick={save}>Save Flow</button>
          )}
        </div>
      </form>
    </div>,
    document.body,
  )
}
